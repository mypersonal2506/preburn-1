package customers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customers/queries"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/plans"
)

const (
	displayNameMaximumLength = 200
	metadataMaximumKeys      = 50
	metadataMaximumBytes     = 4096
	listingName              = "customers"
	likeEscapedUnderscore    = `\_`
	likeAnySuffix            = "%"

	externalIDRule = "expected 1 to 128 letters, digits or the characters . _ : @ -"

	externalIDLocation     = "path.external_id"
	customerIDLocation     = "body.customer_id"
	customerUserIDLocation = "body.customer_user_id"
	displayNameLocation    = "body.display_name"
	metadataLocation       = "body.metadata"
	searchLocation         = "query.search"
)

// Status is the record status of a customer: active, disabled or archived.
// Upsert and Ensure create active customers and keep the status of existing
// ones.
type Status string

// Customer is a customer of the operator's product in one environment.
type Customer struct {
	// ID is the customer's UUID, exposed with the prefix cust.
	ID uuid.UUID
	// Environment is the environment the customer belongs to.
	Environment httpapi.Environment
	// ExternalID is the id the operator's system gives the customer, unique
	// within the environment.
	ExternalID string
	// DisplayName is the name the dashboard shows, or nil for none.
	DisplayName *string
	// PlanID is the plan of the customer, or nil when the default plan of
	// the environment applies.
	PlanID *uuid.UUID
	// Metadata is a JSON object the operator attaches to the customer. It is
	// empty, never nil, when the operator attached nothing.
	Metadata map[string]json.RawMessage
	// Status is the record status of the customer.
	Status Status
	// CreatedAt is when the customer was created, in UTC.
	CreatedAt time.Time
	// UpdatedAt is when an upsert last replaced the customer, in UTC.
	UpdatedAt time.Time
}

// CustomerUser is a user of a customer, known by the id the operator's
// system gives it.
type CustomerUser struct {
	// ID is the customer user's UUID, exposed with the prefix cuser.
	ID uuid.UUID
	// Environment is the environment the customer user belongs to.
	Environment httpapi.Environment
	// CustomerID is the customer the user belongs to.
	CustomerID uuid.UUID
	// ExternalID is the id the operator's system gives the user, unique per
	// customer.
	ExternalID string
	// CreatedAt is when the customer user was created, in UTC.
	CreatedAt time.Time
}

// UpsertInput is the state Service.Upsert gives a customer. It replaces the
// whole state, so a nil field clears what the customer had.
type UpsertInput struct {
	// DisplayName is 1 to 200 characters with no control characters and not
	// only spaces, or nil for none.
	DisplayName *string
	// PlanID is an active plan of the customer's environment, or nil for the
	// default plan of the environment.
	PlanID *uuid.UUID
	// Metadata is a JSON object of at most 50 keys and at most 4096 bytes
	// encoded as compact JSON. Nil stores an empty object.
	Metadata map[string]json.RawMessage
}

// Service creates, replaces and reads customers and customer users, and holds
// the Cache that resolves customers by external id. Create one with
// NewService. It is safe for concurrent use.
type Service struct {
	queries       *queries.Queries
	cache         *cache.Client
	clock         clock.Clock
	customerCache *Cache
}

type listPosition struct {
	CreatedAt  time.Time `json:"created_at"`
	CustomerID uuid.UUID `json:"customer_id"`
}

var (
	externalIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:@-]{1,128}$`)
	listCursor        = httpapi.NewCursor[listPosition](listingName)
	displayNameRule   = fmt.Sprintf("expected 1 to %d characters without control characters", displayNameMaximumLength)
	metadataKeysRule  = fmt.Sprintf("expected at most %d keys", metadataMaximumKeys)
	metadataBytesRule = fmt.Sprintf("expected at most %d bytes of JSON", metadataMaximumBytes)
)

// NewService returns a Service that stores customers in pool, publishes the
// customer invalidation through cacheClient and reads time from timeSource.
// It only stores its arguments, so zero values serve route registration for
// the OpenAPI document.
func NewService(pool *pgxpool.Pool, cacheClient *cache.Client, timeSource clock.Clock) *Service {
	service := &Service{queries: queries.New(pool), cache: cacheClient, clock: timeSource}
	service.customerCache = newCache(service, timeSource)
	return service
}

// Cache returns the Cache of the service's customers. Upsert clears it.
func (service *Service) Cache() *Cache {
	return service.customerCache
}

// Upsert creates the active customer with externalID in environment, or
// replaces the display name, plan and metadata of the existing one with
// input and keeps its id, status and creation time. It clears the service's
// Cache and publishes the customer invalidation, which clears the caches of
// the other processes. An external id that does not match
// ^[A-Za-z0-9._:@-]{1,128}$ or an input outside the rules of UpsertInput
// returns a 422 validation_failed problem naming every invalid field, at
// path.external_id, body.display_name and body.metadata. A plan that is not
// an active plan of environment returns plans.ErrPlanNotFound.
func (service *Service) Upsert(ctx context.Context, environment httpapi.Environment, externalID string, input UpsertInput) (Customer, error) {
	encodedMetadata, err := encodeMetadata(input.Metadata)
	if err != nil {
		return Customer{}, err
	}
	if problems := upsertProblems(externalID, input, encodedMetadata); len(problems) > 0 {
		return Customer{}, httpapi.NewValidationProblem(problems...)
	}
	if input.PlanID != nil {
		if err := service.requireActivePlan(ctx, environment, *input.PlanID); err != nil {
			return Customer{}, err
		}
	}
	row, err := service.queries.UpsertCustomer(ctx, queries.UpsertCustomerParams{
		CustomerID:  identifiers.New(),
		Environment: queries.Environment(environment),
		ExternalID:  externalID,
		DisplayName: input.DisplayName,
		PlanID:      input.PlanID,
		Metadata:    encodedMetadata,
		WrittenAt:   service.clock.Now(),
	})
	if err != nil {
		return Customer{}, fmt.Errorf("upsert customer: %w", err)
	}
	customer, err := customerFromRow(row)
	if err != nil {
		return Customer{}, err
	}
	service.customerCache.Clear()
	err = service.cache.PublishInvalidation(ctx, cache.Invalidation{
		Kind:        cache.InvalidationKindCustomer,
		Environment: string(environment),
		ID:          identifiers.Encode(identifiers.PrefixCustomer, customer.ID),
	})
	if err != nil {
		return Customer{}, err
	}
	return customer, nil
}

// Ensure returns the customer with externalID in environment, and creates it
// active, without a display name, plan or metadata, when it is missing.
// Concurrent calls for one external id create one customer and all return
// it. An external id that does not match ^[A-Za-z0-9._:@-]{1,128}$ returns
// a 422 validation_failed problem at body.customer_id, the field that names
// the customer in runtime requests.
func (service *Service) Ensure(ctx context.Context, environment httpapi.Environment, externalID string) (Customer, error) {
	if !externalIDPattern.MatchString(externalID) {
		return Customer{}, httpapi.NewValidationProblem(httpapi.ProblemError{Location: customerIDLocation, Message: externalIDRule})
	}
	row, err := service.queries.InsertCustomerIfMissing(ctx, queries.InsertCustomerIfMissingParams{
		CustomerID:  identifiers.New(),
		Environment: queries.Environment(environment),
		ExternalID:  externalID,
		CreatedAt:   service.clock.Now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = service.queries.SelectCustomerByExternalID(ctx, queries.SelectCustomerByExternalIDParams{
			Environment: queries.Environment(environment),
			ExternalID:  externalID,
		})
	}
	if err != nil {
		return Customer{}, fmt.Errorf("ensure customer: %w", err)
	}
	return customerFromRow(row)
}

// EnsureUser returns the user with externalUserID of the customer with
// customerID in environment, and creates it when it is missing. The customer
// must belong to environment. Concurrent calls for one user create one user
// and all return it. An external user id that does not match
// ^[A-Za-z0-9._:@-]{1,128}$ returns a 422 validation_failed problem at
// body.customer_user_id, the field that names the user in runtime requests.
func (service *Service) EnsureUser(ctx context.Context, environment httpapi.Environment, customerID uuid.UUID, externalUserID string) (CustomerUser, error) {
	if !externalIDPattern.MatchString(externalUserID) {
		return CustomerUser{}, httpapi.NewValidationProblem(httpapi.ProblemError{Location: customerUserIDLocation, Message: externalIDRule})
	}
	row, err := service.queries.InsertCustomerUserIfMissing(ctx, queries.InsertCustomerUserIfMissingParams{
		CustomerUserID: identifiers.New(),
		Environment:    queries.Environment(environment),
		CustomerID:     customerID,
		ExternalID:     externalUserID,
		CreatedAt:      service.clock.Now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = service.queries.SelectCustomerUser(ctx, queries.SelectCustomerUserParams{
			Environment: queries.Environment(environment),
			CustomerID:  customerID,
			ExternalID:  externalUserID,
		})
	}
	if err != nil {
		return CustomerUser{}, fmt.Errorf("ensure user of customer %s: %w", customerID, err)
	}
	return customerUserFromRow(row), nil
}

// Customer returns the customer with customerID in environment, or
// httpapi.ErrNotFound when there is none.
func (service *Service) Customer(ctx context.Context, environment httpapi.Environment, customerID uuid.UUID) (Customer, error) {
	row, err := service.queries.SelectCustomerByID(ctx, queries.SelectCustomerByIDParams{
		Environment: queries.Environment(environment),
		CustomerID:  customerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Customer{}, httpapi.ErrNotFound
	}
	if err != nil {
		return Customer{}, fmt.Errorf("select customer %s: %w", customerID, err)
	}
	return customerFromRow(row)
}

// CustomerByExternalID returns the customer with externalID in environment,
// or httpapi.ErrNotFound when there is none. It never creates a customer.
func (service *Service) CustomerByExternalID(ctx context.Context, environment httpapi.Environment, externalID string) (Customer, error) {
	row, err := service.queries.SelectCustomerByExternalID(ctx, queries.SelectCustomerByExternalIDParams{
		Environment: queries.Environment(environment),
		ExternalID:  externalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Customer{}, httpapi.ErrNotFound
	}
	if err != nil {
		return Customer{}, fmt.Errorf("select customer by external id: %w", err)
	}
	return customerFromRow(row)
}

// List returns one page of the customers of environment, newest first, and
// the cursor of the next page, which is empty on the last page. A non-empty
// search keeps the customers whose external id starts with search in any
// letter case, so a complete external id also finds its customer. A search
// that does not match ^[A-Za-z0-9._:@-]{1,128}$ returns a 422
// validation_failed problem at query.search. An empty cursor starts at the
// newest customer. A limit of 0 selects httpapi.ListLimitDefault, and a limit
// outside 1 to httpapi.ListLimitMaximum returns a 422 validation_failed
// problem at query.limit. A cursor that List did not return for environment
// fails with httpapi.ErrInvalidCursor.
func (service *Service) List(ctx context.Context, environment httpapi.Environment, search string, cursor string, limit int) ([]Customer, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	parameters := queries.ListCustomersParams{Environment: queries.Environment(environment), RowLimit: int64(pageSize) + 1}
	if search != "" {
		if !externalIDPattern.MatchString(search) {
			return nil, "", httpapi.NewValidationProblem(httpapi.ProblemError{Location: searchLocation, Message: externalIDRule})
		}
		pattern := strings.ReplaceAll(strings.ToLower(search), "_", likeEscapedUnderscore) + likeAnySuffix
		parameters.ExternalIDPattern = &pattern
	}
	if cursor != "" {
		position, err := listCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		parameters.AfterCreatedAt = &position.CreatedAt
		parameters.AfterCustomerID = &position.CustomerID
	}
	rows, err := service.queries.ListCustomers(ctx, parameters)
	if err != nil {
		return nil, "", fmt.Errorf("list customers of environment %s: %w", environment, err)
	}
	listed := make([]Customer, 0, len(rows))
	for _, row := range rows {
		customer, err := customerFromRow(row)
		if err != nil {
			return nil, "", err
		}
		listed = append(listed, customer)
	}
	if len(listed) <= pageSize {
		return listed, "", nil
	}
	listed = listed[:pageSize]
	last := listed[pageSize-1]
	nextCursor, err := listCursor.Encode(environment, listPosition{CreatedAt: last.CreatedAt, CustomerID: last.ID})
	if err != nil {
		return nil, "", err
	}
	return listed, nextCursor, nil
}

func (service *Service) requireActivePlan(ctx context.Context, environment httpapi.Environment, planID uuid.UUID) error {
	active, err := service.queries.SelectActivePlanExists(ctx, queries.SelectActivePlanExistsParams{
		Environment: queries.Environment(environment),
		PlanID:      planID,
	})
	if err != nil {
		return fmt.Errorf("select plan %s: %w", planID, err)
	}
	if !active {
		return plans.ErrPlanNotFound
	}
	return nil
}

func upsertProblems(externalID string, input UpsertInput, encodedMetadata []byte) []httpapi.ProblemError {
	var problems []httpapi.ProblemError
	if !externalIDPattern.MatchString(externalID) {
		problems = append(problems, httpapi.ProblemError{Location: externalIDLocation, Message: externalIDRule})
	}
	if input.DisplayName != nil && !validDisplayName(*input.DisplayName) {
		problems = append(problems, httpapi.ProblemError{Location: displayNameLocation, Message: displayNameRule})
	}
	if len(input.Metadata) > metadataMaximumKeys {
		problems = append(problems, httpapi.ProblemError{Location: metadataLocation, Message: metadataKeysRule})
	}
	if len(encodedMetadata) > metadataMaximumBytes {
		problems = append(problems, httpapi.ProblemError{Location: metadataLocation, Message: metadataBytesRule})
	}
	return problems
}

func validDisplayName(displayName string) bool {
	return strings.TrimSpace(displayName) != "" &&
		utf8.RuneCountInString(displayName) <= displayNameMaximumLength &&
		utf8.ValidString(displayName) &&
		!strings.ContainsFunc(displayName, unicode.IsControl)
}

func encodeMetadata(metadata map[string]json.RawMessage) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]json.RawMessage{}
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(metadata); err != nil {
		return nil, fmt.Errorf("encode customer metadata: %w", err)
	}
	return bytes.TrimSuffix(encoded.Bytes(), []byte("\n")), nil
}

func customerFromRow(row queries.Customer) (Customer, error) {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
		return Customer{}, fmt.Errorf("decode metadata of customer %s: %w", row.CustomerID, err)
	}
	return Customer{
		ID:          row.CustomerID,
		Environment: httpapi.Environment(row.Environment),
		ExternalID:  row.ExternalID,
		DisplayName: row.DisplayName,
		PlanID:      row.PlanID,
		Metadata:    metadata,
		Status:      Status(row.Status),
		CreatedAt:   row.CreatedAt.UTC(),
		UpdatedAt:   row.UpdatedAt.UTC(),
	}, nil
}

func customerUserFromRow(row queries.CustomerUser) CustomerUser {
	return CustomerUser{
		ID:          row.CustomerUserID,
		Environment: httpapi.Environment(row.Environment),
		CustomerID:  row.CustomerID,
		ExternalID:  row.ExternalID,
		CreatedAt:   row.CreatedAt.UTC(),
	}
}
