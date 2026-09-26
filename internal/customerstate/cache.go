package customerstate

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/signals"
)

const (
	cacheTimeToLive     = 30 * time.Second
	cacheMaximumEntries = 10000
)

// Cache keeps the current customer states it loaded in process memory for
// 30 seconds, at most 10000 per environment, and evicts the least recently
// used one beyond that. It holds only states loaded at the current time,
// because with overlapping subscriptions a period loaded at an earlier time
// can still contain the current time without being the current period. A
// state is never served once its period has ended. A load that was still
// reading the database when an invalidation arrived returns its result
// without caching it. Create one with NewCache. It is safe for concurrent
// use.
type Cache struct {
	loader     *Loader
	states     map[httpapi.Environment]*cache.LocalCache[uuid.UUID, signals.CustomerState]
	mutex      sync.Mutex
	generation uint64
}

// NewCache returns an empty Cache that loads through loader and reads time
// from timeSource for expiry.
func NewCache(loader *Loader, timeSource clock.Clock) *Cache {
	return &Cache{
		loader: loader,
		states: map[httpapi.Environment]*cache.LocalCache[uuid.UUID, signals.CustomerState]{
			httpapi.EnvironmentTest: cache.NewLocalCache[uuid.UUID, signals.CustomerState](timeSource, cacheTimeToLive, cacheMaximumEntries),
			httpapi.EnvironmentLive: cache.NewLocalCache[uuid.UUID, signals.CustomerState](timeSource, cacheTimeToLive, cacheMaximumEntries),
		},
	}
}

// Load returns the current state of the customer with customerID in
// environment, where now is the caller's current time: from the cache while
// its period contains now, or else through Loader.Load, and caches it. A
// state at another time comes from Loader, never from Load. It returns
// httpapi.ErrNotFound when the customer does not exist. The returned state
// shares its PlanID and HoldTimes with the cache and other callers, so
// callers never modify them.
func (stateCache *Cache) Load(ctx context.Context, environment httpapi.Environment, customerID uuid.UUID, now time.Time) (signals.CustomerState, error) {
	environmentStates := stateCache.states[environment]
	if state, found := environmentStates.Get(customerID); found && state.Period.Contains(now) {
		return state, nil
	}
	generation := stateCache.currentGeneration()
	state, err := stateCache.loader.Load(ctx, environment, customerID, now)
	if err != nil {
		return signals.CustomerState{}, err
	}
	stateCache.store(environmentStates, customerID, state, generation)
	return state, nil
}

// Loader returns the Loader the cache loads through, for states at a time
// other than the current one, which the cache never holds.
func (stateCache *Cache) Loader() *Loader {
	return stateCache.loader
}

// Invalidate removes the states invalidation marks stale. The customer kind
// removes the customer its ID names, or empties the environment when the ID
// names no customer. The plan and settings kinds empty the environment, and
// every other kind is ignored. An environment other than test and live
// empties every environment. It is the handler the process passes to
// cache.Client.SubscribeInvalidations.
func (stateCache *Cache) Invalidate(invalidation cache.Invalidation) {
	if invalidation.Kind != cache.InvalidationKindCustomer && invalidation.Kind != cache.InvalidationKindPlan && invalidation.Kind != cache.InvalidationKindSettings {
		return
	}
	environment, err := httpapi.ParseEnvironment(invalidation.Environment)
	if err != nil {
		stateCache.Clear()
		return
	}
	stateCache.mutex.Lock()
	defer stateCache.mutex.Unlock()
	stateCache.generation++
	customerID, err := identifiers.Decode(identifiers.PrefixCustomer, invalidation.ID)
	if invalidation.Kind == cache.InvalidationKindCustomer && err == nil {
		stateCache.states[environment].Delete(customerID)
		return
	}
	stateCache.states[environment].Clear()
}

// Clear empties every environment and keeps loads that are reading the
// database at that moment from caching their result. The process calls it
// when the invalidation subscription reconnects, because invalidations
// published while it was disconnected are lost.
func (stateCache *Cache) Clear() {
	stateCache.mutex.Lock()
	defer stateCache.mutex.Unlock()
	stateCache.generation++
	for _, environmentStates := range stateCache.states {
		environmentStates.Clear()
	}
}

func (stateCache *Cache) currentGeneration() uint64 {
	stateCache.mutex.Lock()
	defer stateCache.mutex.Unlock()
	return stateCache.generation
}

func (stateCache *Cache) store(environmentStates *cache.LocalCache[uuid.UUID, signals.CustomerState], customerID uuid.UUID, state signals.CustomerState, generation uint64) {
	stateCache.mutex.Lock()
	defer stateCache.mutex.Unlock()
	if stateCache.generation == generation {
		environmentStates.Set(customerID, state)
	}
}
