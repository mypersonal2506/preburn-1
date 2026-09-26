package customers

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/httpapi"
)

const (
	cacheTimeToLive     = 30 * time.Second
	cacheMaximumEntries = 10000
)

// Cache keeps the customers it resolved by external id, and the customer
// users it ensured, in process memory for 30 seconds, at most 10000 of each,
// and evicts the least recently used one beyond that. A lookup that was
// still reading the database when the cache was cleared returns its result
// without caching it, so a customer upserted during the lookup is never
// served stale from the cache. Get the one of a Service with Service.Cache.
// It is safe for concurrent use.
type Cache struct {
	service    *Service
	customers  *cache.LocalCache[cacheKey, Customer]
	users      *cache.LocalCache[userCacheKey, CustomerUser]
	mutex      sync.Mutex
	generation uint64
}

type cacheKey struct {
	environment httpapi.Environment
	externalID  string
}

type userCacheKey struct {
	environment    httpapi.Environment
	customerID     uuid.UUID
	externalUserID string
}

func newCache(service *Service, timeSource clock.Clock) *Cache {
	return &Cache{
		service:   service,
		customers: cache.NewLocalCache[cacheKey, Customer](timeSource, cacheTimeToLive, cacheMaximumEntries),
		users:     cache.NewLocalCache[userCacheKey, CustomerUser](timeSource, cacheTimeToLive, cacheMaximumEntries),
	}
}

// Ensure returns the customer with externalID in environment from the cache,
// or on a miss through Service.Ensure, which creates it when it is missing,
// and caches it. The returned customer shares its DisplayName, PlanID and
// Metadata with the cache and other callers, so callers never modify them.
func (customerCache *Cache) Ensure(ctx context.Context, environment httpapi.Environment, externalID string) (Customer, error) {
	key := cacheKey{environment: environment, externalID: externalID}
	if customer, found := customerCache.customers.Get(key); found {
		return customer, nil
	}
	generation := customerCache.currentGeneration()
	customer, err := customerCache.service.Ensure(ctx, environment, externalID)
	if err != nil {
		return Customer{}, err
	}
	storeIfCurrent(customerCache, customerCache.customers, key, customer, generation)
	return customer, nil
}

// EnsureUser returns the user with externalUserID of the customer with
// customerID in environment from the cache, or on a miss through
// Service.EnsureUser, which creates it when it is missing, and caches it. A
// user found in the cache costs no database write.
func (customerCache *Cache) EnsureUser(ctx context.Context, environment httpapi.Environment, customerID uuid.UUID, externalUserID string) (CustomerUser, error) {
	key := userCacheKey{environment: environment, customerID: customerID, externalUserID: externalUserID}
	if user, found := customerCache.users.Get(key); found {
		return user, nil
	}
	generation := customerCache.currentGeneration()
	user, err := customerCache.service.EnsureUser(ctx, environment, customerID, externalUserID)
	if err != nil {
		return CustomerUser{}, err
	}
	storeIfCurrent(customerCache, customerCache.users, key, user, generation)
	return user, nil
}

// Invalidate empties the cache when invalidation has the kind customer, and
// ignores every other kind. It is the handler the process passes to
// cache.Client.SubscribeInvalidations.
func (customerCache *Cache) Invalidate(invalidation cache.Invalidation) {
	if invalidation.Kind == cache.InvalidationKindCustomer {
		customerCache.Clear()
	}
}

// Clear empties the cache, and keeps lookups that are reading the database at
// that moment from caching their result. Upsert calls it, and the process
// calls it when the invalidation subscription reconnects, because
// invalidations published while it was disconnected are lost.
func (customerCache *Cache) Clear() {
	customerCache.mutex.Lock()
	defer customerCache.mutex.Unlock()
	customerCache.generation++
	customerCache.customers.Clear()
	customerCache.users.Clear()
}

func (customerCache *Cache) currentGeneration() uint64 {
	customerCache.mutex.Lock()
	defer customerCache.mutex.Unlock()
	return customerCache.generation
}

func storeIfCurrent[Key comparable, Value any](customerCache *Cache, entries *cache.LocalCache[Key, Value], key Key, value Value, generation uint64) {
	customerCache.mutex.Lock()
	defer customerCache.mutex.Unlock()
	if customerCache.generation == generation {
		entries.Set(key, value)
	}
}
