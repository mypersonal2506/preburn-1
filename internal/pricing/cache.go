package pricing

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
	ruleSetTimeToLive     = 5 * time.Minute
	ruleSetMaximumEntries = 2
)

// RuleSetCache keeps the RuleSet of each environment in process memory for 5
// minutes. A RuleSet holds the model aliases and the catalog rules and
// overrides of the environment that are open or closed within the last 90
// days, so a report up to 90 days late is rated at the prices in effect when
// it occurred. Concurrent misses
// for one environment share a single load from Postgres. A load that was
// still reading when the cache was cleared returns its RuleSet without
// caching it, so an override changed during the load is never served stale
// from the cache. Get the one of a Service with Service.RuleSets. It is safe
// for concurrent use.
type RuleSetCache struct {
	service    *Service
	ruleSets   *cache.LocalCache[httpapi.Environment, *RuleSet]
	loads      singleflight.Group
	mutex      sync.Mutex
	generation uint64
}

func newRuleSetCache(service *Service, timeSource clock.Clock) *RuleSetCache {
	return &RuleSetCache{
		service:  service,
		ruleSets: cache.NewLocalCache[httpapi.Environment, *RuleSet](timeSource, ruleSetTimeToLive, ruleSetMaximumEntries),
	}
}

// Get returns the RuleSet of environment from the cache, or loads it from
// Postgres and caches it. The load runs to completion even when ctx ends,
// because other callers may be waiting for it.
func (ruleSetCache *RuleSetCache) Get(ctx context.Context, environment httpapi.Environment) (*RuleSet, error) {
	if ruleSet, found := ruleSetCache.ruleSets.Get(environment); found {
		return ruleSet, nil
	}
	generation := ruleSetCache.currentGeneration()
	loaded, err, _ := ruleSetCache.loads.Do(fmt.Sprintf("%s/%d", environment, generation), func() (any, error) {
		ruleSet, err := ruleSetCache.service.loadRuleSet(context.WithoutCancel(ctx), environment)
		if err != nil {
			return nil, err
		}
		ruleSetCache.store(environment, ruleSet, generation)
		return ruleSet, nil
	})
	if err != nil {
		return nil, err
	}
	return loaded.(*RuleSet), nil
}

// Invalidate empties the cache when invalidation has the kind pricing, and
// ignores every other kind. It is the handler the process passes to
// cache.Client.SubscribeInvalidations.
func (ruleSetCache *RuleSetCache) Invalidate(invalidation cache.Invalidation) {
	if invalidation.Kind == cache.InvalidationKindPricing {
		ruleSetCache.Clear()
	}
}

// Clear empties the cache, and keeps loads that are reading Postgres at that
// moment from caching their result. Every override change calls it, and the
// process calls it when the invalidation subscription reconnects, because
// invalidations published while it was disconnected are lost.
func (ruleSetCache *RuleSetCache) Clear() {
	ruleSetCache.mutex.Lock()
	defer ruleSetCache.mutex.Unlock()
	ruleSetCache.generation++
	ruleSetCache.ruleSets.Clear()
}

func (ruleSetCache *RuleSetCache) currentGeneration() uint64 {
	ruleSetCache.mutex.Lock()
	defer ruleSetCache.mutex.Unlock()
	return ruleSetCache.generation
}

func (ruleSetCache *RuleSetCache) store(environment httpapi.Environment, ruleSet *RuleSet, generation uint64) {
	ruleSetCache.mutex.Lock()
	defer ruleSetCache.mutex.Unlock()
	if ruleSetCache.generation == generation {
		ruleSetCache.ruleSets.Set(environment, ruleSet)
	}
}
