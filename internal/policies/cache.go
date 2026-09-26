package policies

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/httpapi"
)

const (
	activePoliciesTimeToLive     = 60 * time.Second
	activePoliciesMaximumEntries = 2
)

// Cache keeps the active policies of each environment in process memory for
// 60 seconds. Concurrent misses for one environment share a single load from
// Postgres. A load that was still reading when the cache was cleared returns
// its policies without caching them, so a policy changed during the load is
// never served stale from the cache. Get the one of a Service with
// Service.Cache. It is safe for concurrent use.
type Cache struct {
	service    *Service
	policies   *cache.LocalCache[httpapi.Environment, []Policy]
	loads      singleflight.Group
	mutex      sync.Mutex
	generation uint64
}

func newCache(service *Service, timeSource clock.Clock) *Cache {
	return &Cache{
		service:  service,
		policies: cache.NewLocalCache[httpapi.Environment, []Policy](timeSource, activePoliciesTimeToLive, activePoliciesMaximumEntries),
	}
}

// Active returns the active policies of environment, ordered by id, from the
// cache, or loads them from Postgres and caches them. The load runs to
// completion even when ctx ends, because other callers may be waiting for
// it. The returned policies are shared with the cache and other callers, so
// callers never modify them.
func (policyCache *Cache) Active(ctx context.Context, environment httpapi.Environment) ([]Policy, error) {
	if active, found := policyCache.policies.Get(environment); found {
		return active, nil
	}
	generation := policyCache.currentGeneration()
	loaded, err, _ := policyCache.loads.Do(fmt.Sprintf("%s/%d", environment, generation), func() (any, error) {
		active, err := policyCache.service.loadActivePolicies(context.WithoutCancel(ctx), environment)
		if err != nil {
			return nil, err
		}
		policyCache.store(environment, active, generation)
		return active, nil
	})
	if err != nil {
		return nil, err
	}
	return loaded.([]Policy), nil
}

// Invalidate empties the cache when invalidation has the kind policies, and
// ignores every other kind. It is the handler the process passes to
// cache.Client.SubscribeInvalidations.
func (policyCache *Cache) Invalidate(invalidation cache.Invalidation) {
	if invalidation.Kind == cache.InvalidationKindPolicies {
		policyCache.Clear()
	}
}

// Clear empties the cache, and keeps loads that are reading Postgres at that
// moment from caching their result. Every policy change calls it, and the
// process calls it when the invalidation subscription reconnects, because
// invalidations published while it was disconnected are lost.
func (policyCache *Cache) Clear() {
	policyCache.mutex.Lock()
	defer policyCache.mutex.Unlock()
	policyCache.generation++
	policyCache.policies.Clear()
}

func (policyCache *Cache) currentGeneration() uint64 {
	policyCache.mutex.Lock()
	defer policyCache.mutex.Unlock()
	return policyCache.generation
}

func (policyCache *Cache) store(environment httpapi.Environment, active []Policy, generation uint64) {
	policyCache.mutex.Lock()
	defer policyCache.mutex.Unlock()
	if policyCache.generation == generation {
		policyCache.policies.Set(environment, active)
	}
}
