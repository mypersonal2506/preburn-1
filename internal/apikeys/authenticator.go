package apikeys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/apikeys/queries"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	authorizationHeader    = "Authorization"
	bearerScheme           = "Bearer"
	keyCacheTimeToLive     = 30 * time.Second
	keyCacheMaximumEntries = 1000
	lastUsedWriteInterval  = time.Minute
)

// Principal is the httpapi.Principal of a request authenticated by an API
// key. It implements httpapi.EnvironmentPrincipal, so handlers read its
// environment with httpapi.EnvironmentFromContext.
type Principal struct {
	// APIKeyID is the id of the key.
	APIKeyID uuid.UUID
	// Environment is the environment of the key.
	Environment httpapi.Environment
	// Scope is the scope of the key.
	Scope Scope
}

// Authenticator is the httpapi.Authenticator of API keys sent in the
// Authorization header as bearer tokens. It keeps the keys it resolved in
// process memory for 30 seconds. A lookup that was still reading the
// database when the cache was cleared returns its result without caching
// it, so a key revoked during the lookup is not cached. Get the one of a
// Service with Service.Authenticator. It is safe for concurrent use.
type Authenticator struct {
	queries         *queries.Queries
	clock           clock.Clock
	logger          *logging.Logger
	principals      *cache.LocalCache[string, Principal]
	cacheMutex      sync.Mutex
	cacheGeneration uint64
	lastUsedMutex   sync.Mutex
	lastUsedWrites  map[uuid.UUID]time.Time
}

var secretPattern = regexp.MustCompile(fmt.Sprintf(`^%s_(test|live)_(runtime|admin)_[0-9A-Za-z]{%d}$`, secretPrefix, secretRandomLength))

func newAuthenticator(keyQueries *queries.Queries, timeSource clock.Clock, logger *logging.Logger) *Authenticator {
	return &Authenticator{
		queries:        keyQueries,
		clock:          timeSource,
		logger:         logger,
		principals:     cache.NewLocalCache[string, Principal](timeSource, keyCacheTimeToLive, keyCacheMaximumEntries),
		lastUsedWrites: map[uuid.UUID]time.Time{},
	}
}

// PrincipalEnvironment returns the environment of the key.
func (principal Principal) PrincipalEnvironment() httpapi.Environment {
	return principal.Environment
}

// Authenticate returns the Principal of the key that request sends in its
// Authorization header. It checks, in order:
//
//   - the request has one Authorization header, with the scheme Bearer in
//     any letter case, a single space and a token shaped like a Preburn
//     secret, or httpapi.ErrAuthenticationRequired without a database read
//   - the SHA-256 of the token names an active key, looked up in the cache
//     and then in the database, or httpapi.ErrAuthenticationRequired
//   - group admits the key's scope, or httpapi.ErrScopeForbidden: runtime
//     routes admit both scopes, admin routes only admin keys, and dashboard
//     routes no key
//
// An admitted request records the key's last use when the process last
// wrote it a minute or more ago. A failed write logs
// apikeys.last_used_update_failed and the request goes on.
func (authenticator *Authenticator) Authenticate(ctx context.Context, request *http.Request, group httpapi.RouteGroup) (httpapi.Principal, error) {
	secret, found := bearerSecret(request.Header)
	if !found {
		return nil, httpapi.ErrAuthenticationRequired
	}
	principal, err := authenticator.principal(ctx, secret)
	if err != nil {
		return nil, err
	}
	if !principal.Scope.admits(group) {
		return nil, httpapi.ErrScopeForbidden
	}
	authenticator.recordUse(ctx, principal.APIKeyID)
	return principal, nil
}

// Invalidate empties the cache of resolved keys when invalidation has the
// kind api_key, and ignores every other kind. It is the handler the process
// passes to cache.Client.SubscribeInvalidations.
func (authenticator *Authenticator) Invalidate(invalidation cache.Invalidation) {
	if invalidation.Kind == cache.InvalidationKindAPIKey {
		authenticator.ClearCache()
	}
}

// ClearCache empties the cache of resolved keys, and keeps lookups that are
// reading the database at that moment from caching their result. Revoke
// calls it, and the process calls it when the invalidation subscription
// reconnects, because invalidations published while it was disconnected are
// lost.
func (authenticator *Authenticator) ClearCache() {
	authenticator.cacheMutex.Lock()
	defer authenticator.cacheMutex.Unlock()
	authenticator.cacheGeneration++
	authenticator.principals.Clear()
}

// HasBearerAuthorization reports whether request has an Authorization header
// with the scheme Bearer in any letter case, whatever follows the scheme.
// Such a request is for the Authenticator, which admits it only with exactly
// one Authorization header holding the secret of an active key. A header with
// another scheme, such as the Basic credentials of a reverse proxy, does not
// count.
func HasBearerAuthorization(request *http.Request) bool {
	for _, value := range request.Header.Values(authorizationHeader) {
		if _, isBearer := bearerToken(value); isBearer {
			return true
		}
	}
	return false
}

func (authenticator *Authenticator) principal(ctx context.Context, secret string) (Principal, error) {
	secretHash := secrets.HashToken(secret)
	if principal, found := authenticator.principals.Get(string(secretHash)); found {
		return principal, nil
	}
	generation := authenticator.currentCacheGeneration()
	row, err := authenticator.queries.SelectActiveAPIKeyBySecretHash(ctx, secretHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, httpapi.ErrAuthenticationRequired
	}
	if err != nil {
		return Principal{}, fmt.Errorf("select active API key by secret hash: %w", err)
	}
	principal := Principal{APIKeyID: row.APIKeyID, Environment: httpapi.Environment(row.Environment), Scope: Scope(row.Scope)}
	authenticator.cachePrincipal(string(secretHash), principal, generation)
	return principal, nil
}

func (authenticator *Authenticator) currentCacheGeneration() uint64 {
	authenticator.cacheMutex.Lock()
	defer authenticator.cacheMutex.Unlock()
	return authenticator.cacheGeneration
}

func (authenticator *Authenticator) cachePrincipal(secretHash string, principal Principal, generation uint64) {
	authenticator.cacheMutex.Lock()
	defer authenticator.cacheMutex.Unlock()
	if authenticator.cacheGeneration == generation {
		authenticator.principals.Set(secretHash, principal)
	}
}

func (authenticator *Authenticator) recordUse(ctx context.Context, apiKeyID uuid.UUID) {
	now := authenticator.clock.Now()
	if !authenticator.claimLastUsedWrite(apiKeyID, now) {
		return
	}
	err := authenticator.queries.UpdateAPIKeyLastUsed(ctx, queries.UpdateAPIKeyLastUsedParams{APIKeyID: apiKeyID, LastUsedAt: now})
	if err != nil {
		authenticator.logger.Warn(ctx, logging.APIKeysLastUsedUpdateFailed,
			slog.String("key_id", identifiers.Encode(identifiers.PrefixAPIKey, apiKeyID)),
			slog.String("error", err.Error()))
	}
}

func (authenticator *Authenticator) claimLastUsedWrite(apiKeyID uuid.UUID, now time.Time) bool {
	authenticator.lastUsedMutex.Lock()
	defer authenticator.lastUsedMutex.Unlock()
	if written, found := authenticator.lastUsedWrites[apiKeyID]; found && now.Sub(written) < lastUsedWriteInterval {
		return false
	}
	authenticator.lastUsedWrites[apiKeyID] = now
	return true
}

func (scope Scope) admits(group httpapi.RouteGroup) bool {
	return group == httpapi.RouteGroupRuntime || group == httpapi.RouteGroupAdmin && scope == ScopeAdmin
}

func bearerSecret(header http.Header) (string, bool) {
	values := header.Values(authorizationHeader)
	if len(values) != 1 {
		return "", false
	}
	secret, isBearer := bearerToken(values[0])
	if !isBearer || !secretPattern.MatchString(secret) {
		return "", false
	}
	return secret, true
}

func bearerToken(authorization string) (string, bool) {
	scheme, token, _ := strings.Cut(authorization, " ")
	return token, strings.EqualFold(scheme, bearerScheme)
}
