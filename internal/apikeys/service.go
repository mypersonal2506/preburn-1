package apikeys

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/apikeys/queries"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	secretPrefix         = "pb"
	secretRandomLength   = 32
	secretLastFourLength = 4
	nameMaximumLength    = 80
	listingName          = "api_keys"

	nameLocation  = "body.name"
	scopeLocation = "body.scope"
)

// Scope is the set of route groups an API key calls.
type Scope string

const (
	// ScopeRuntime keys call the runtime routes, such as check and report.
	ScopeRuntime Scope = "runtime"
	// ScopeAdmin keys call the runtime and the admin routes.
	ScopeAdmin Scope = "admin"
)

// Status is the state of an API key. Only active keys authenticate.
type Status string

const (
	// StatusActive keys authenticate requests.
	StatusActive Status = "active"
	// StatusDisabled keys are revoked and authenticate nothing.
	StatusDisabled Status = "disabled"
)

// APIKey is a credential that a service sends as a bearer token to call
// Preburn in one environment. It never holds the secret.
type APIKey struct {
	// ID is the key's UUID, exposed with the prefix key. It is not the secret.
	ID uuid.UUID
	// Environment is the environment every request with the key acts in.
	Environment httpapi.Environment
	// Name tells keys apart in the dashboard.
	Name string
	// Scope is the set of route groups the key calls.
	Scope Scope
	// SecretLastFour is the last four characters of the secret, for display.
	SecretLastFour string
	// Status tells whether the key authenticates.
	Status Status
	// LastUsedAt is when the key last authenticated a request, written at
	// most once a minute per process, or nil before its first use.
	LastUsedAt *time.Time
	// CreatedByMemberID is the member who created the key, or nil for a key
	// created from the command line.
	CreatedByMemberID *uuid.UUID
	// CreatedAt is when the key was created, in UTC.
	CreatedAt time.Time
}

// Service creates, lists and revokes API keys, and holds the Authenticator
// that resolves them. Create one with NewService. It is safe for concurrent
// use.
type Service struct {
	queries       *queries.Queries
	cache         *cache.Client
	clock         clock.Clock
	authenticator *Authenticator
}

type listPosition struct {
	CreatedAt time.Time `json:"created_at"`
	APIKeyID  uuid.UUID `json:"api_key_id"`
}

var (
	listCursor = httpapi.NewCursor[listPosition](listingName)
	nameRule   = fmt.Sprintf("expected 1 to %d characters without control characters", nameMaximumLength)
	scopeRule  = fmt.Sprintf("expected %s or %s", ScopeRuntime, ScopeAdmin)
)

// NewService returns a Service that stores keys in pool, publishes the
// api_key invalidation through cacheClient, reads time from timeSource and
// logs the events of its Authenticator to logger. It only stores its
// arguments, so zero values serve route registration for the OpenAPI
// document.
func NewService(pool *pgxpool.Pool, cacheClient *cache.Client, timeSource clock.Clock, logger *logging.Logger) *Service {
	keyQueries := queries.New(pool)
	return &Service{
		queries:       keyQueries,
		cache:         cacheClient,
		clock:         timeSource,
		authenticator: newAuthenticator(keyQueries, timeSource, logger),
	}
}

// Authenticator returns the Authenticator of the service's keys. Revoke
// clears its cache.
func (service *Service) Authenticator() *Authenticator {
	return service.authenticator
}

// Create adds an active key named name with scope to environment and returns
// it with its secret, pb_<environment>_<scope>_<32 random base62
// characters>. Only the SHA-256 hash and the last four characters of the
// secret are stored, so no later call returns it. createdBy is the member
// creating the key, or nil. A name that is not 1 to 80 characters without
// control characters and not only spaces, or an unknown scope, returns a 422
// validation_failed problem at body.name and body.scope.
func (service *Service) Create(ctx context.Context, environment httpapi.Environment, name string, scope Scope, createdBy *uuid.UUID) (APIKey, string, error) {
	if problems := createProblems(name, scope); len(problems) > 0 {
		return APIKey{}, "", httpapi.NewValidationProblem(problems...)
	}
	secret := strings.Join([]string{secretPrefix, string(environment), string(scope), secrets.RandomBase62(secretRandomLength)}, "_")
	row, err := service.queries.InsertAPIKey(ctx, queries.InsertAPIKeyParams{
		APIKeyID:          identifiers.New(),
		Environment:       queries.Environment(environment),
		Name:              name,
		Scope:             string(scope),
		SecretHash:        secrets.HashToken(secret),
		SecretLastFour:    secret[len(secret)-secretLastFourLength:],
		CreatedByMemberID: createdBy,
		CreatedAt:         service.clock.Now(),
	})
	if err != nil {
		return APIKey{}, "", fmt.Errorf("insert API key: %w", err)
	}
	return apiKeyFromRow(row), secret, nil
}

// List returns one page of the active and disabled keys of environment,
// newest first, and the cursor of the next page, which is empty on the last
// page. An empty cursor starts at the newest key. A limit of 0 selects
// httpapi.ListLimitDefault, and a limit outside 1 to httpapi.ListLimitMaximum
// returns a 422 validation_failed problem at query.limit. A cursor that List
// did not return for environment fails with httpapi.ErrInvalidCursor.
func (service *Service) List(ctx context.Context, environment httpapi.Environment, cursor string, limit int) ([]APIKey, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	parameters := queries.ListAPIKeysParams{Environment: queries.Environment(environment), RowLimit: int64(pageSize) + 1}
	if cursor != "" {
		position, err := listCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		parameters.AfterCreatedAt = &position.CreatedAt
		parameters.AfterAPIKeyID = &position.APIKeyID
	}
	rows, err := service.queries.ListAPIKeys(ctx, parameters)
	if err != nil {
		return nil, "", fmt.Errorf("list API keys of environment %s: %w", environment, err)
	}
	keys := make([]APIKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, apiKeyFromRow(row))
	}
	if len(keys) <= pageSize {
		return keys, "", nil
	}
	keys = keys[:pageSize]
	last := keys[pageSize-1]
	nextCursor, err := listCursor.Encode(environment, listPosition{CreatedAt: last.CreatedAt, APIKeyID: last.ID})
	if err != nil {
		return nil, "", err
	}
	return keys, nextCursor, nil
}

// Revoke disables the key with apiKeyID in environment. It clears the cache
// of the service's Authenticator, so the next request with the key fails,
// and publishes the api_key invalidation, which clears the caches of the
// other processes. A disabled key can be revoked again. A key that does not
// exist in environment returns httpapi.ErrNotFound.
func (service *Service) Revoke(ctx context.Context, environment httpapi.Environment, apiKeyID uuid.UUID) error {
	disabled, err := service.queries.DisableAPIKey(ctx, queries.DisableAPIKeyParams{
		APIKeyID:    apiKeyID,
		Environment: queries.Environment(environment),
		UpdatedAt:   service.clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("disable API key %s: %w", apiKeyID, err)
	}
	if disabled == 0 {
		return httpapi.ErrNotFound
	}
	service.authenticator.ClearCache()
	return service.cache.PublishInvalidation(ctx, cache.Invalidation{
		Kind:        cache.InvalidationKindAPIKey,
		Environment: string(environment),
		ID:          identifiers.Encode(identifiers.PrefixAPIKey, apiKeyID),
	})
}

func createProblems(name string, scope Scope) []httpapi.ProblemError {
	var problems []httpapi.ProblemError
	if !validName(name) {
		problems = append(problems, httpapi.ProblemError{Location: nameLocation, Message: nameRule})
	}
	if scope != ScopeRuntime && scope != ScopeAdmin {
		problems = append(problems, httpapi.ProblemError{Location: scopeLocation, Message: scopeRule})
	}
	return problems
}

func validName(name string) bool {
	return strings.TrimSpace(name) != "" &&
		utf8.RuneCountInString(name) <= nameMaximumLength &&
		utf8.ValidString(name) &&
		!strings.ContainsFunc(name, unicode.IsControl)
}

func apiKeyFromRow(row queries.APIKey) APIKey {
	key := APIKey{
		ID:                row.APIKeyID,
		Environment:       httpapi.Environment(row.Environment),
		Name:              row.Name,
		Scope:             Scope(row.Scope),
		SecretLastFour:    row.SecretLastFour,
		Status:            Status(row.Status),
		CreatedByMemberID: row.CreatedByMemberID,
		CreatedAt:         row.CreatedAt.UTC(),
	}
	if row.LastUsedAt != nil {
		lastUsedAt := row.LastUsedAt.UTC()
		key.LastUsedAt = &lastUsedAt
	}
	return key
}
