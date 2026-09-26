package customerstate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
)

type cachedCustomers struct {
	firstTestCustomerID  uuid.UUID
	secondTestCustomerID uuid.UUID
	liveCustomerID       uuid.UUID
}

func TestCacheServesTheCachedStateUntilItExpires(t *testing.T) {
	harness := newHarness(t)
	customerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, september, dollars(50))
	stateCache := customerstate.NewCache(harness.loader, harness.clock)

	assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentTest, customerID, dollars(50))
	harness.setEveryRollupRevenue(t.Context(), t, dollars(80))
	harness.clock.Advance(29 * time.Second)
	assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentTest, customerID, dollars(50))
	harness.clock.Advance(time.Second)
	assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentTest, customerID, dollars(80))
}

func TestCacheReloadsWhenThePeriodEnds(t *testing.T) {
	harness := newHarness(t)
	harness.clock.Set(september.End.Add(-10 * time.Second))
	customerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, september, dollars(50))
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, october, dollars(70))
	stateCache := customerstate.NewCache(harness.loader, harness.clock)

	assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentTest, customerID, dollars(50))
	harness.clock.Advance(20 * time.Second)
	state, err := stateCache.Load(t.Context(), httpapi.EnvironmentTest, customerID, harness.clock.Now())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if state.Period != october || state.NetRevenue != dollars(70) {
		t.Errorf("Load() after the period ended = %+v and %d, want %+v and %d", state.Period, state.NetRevenue, october, dollars(70))
	}
}

func TestCacheInvalidation(t *testing.T) {
	tests := []struct {
		name               string
		invalidate         func(stateCache *customerstate.Cache, customers cachedCustomers)
		wantFirstReloaded  bool
		wantSecondReloaded bool
		wantLiveReloaded   bool
	}{
		{
			name: "customer invalidation clears that customer",
			invalidate: func(stateCache *customerstate.Cache, customers cachedCustomers) {
				stateCache.Invalidate(customerInvalidation(httpapi.EnvironmentTest, customers.firstTestCustomerID))
			},
			wantFirstReloaded: true,
		},
		{
			name: "customer invalidation in the other environment clears nothing here",
			invalidate: func(stateCache *customerstate.Cache, customers cachedCustomers) {
				stateCache.Invalidate(customerInvalidation(httpapi.EnvironmentLive, customers.firstTestCustomerID))
			},
		},
		{
			name: "customer invalidation without an id clears the environment",
			invalidate: func(stateCache *customerstate.Cache, _ cachedCustomers) {
				stateCache.Invalidate(cache.Invalidation{Kind: cache.InvalidationKindCustomer, Environment: string(httpapi.EnvironmentTest)})
			},
			wantFirstReloaded:  true,
			wantSecondReloaded: true,
		},
		{
			name: "plan invalidation clears the environment",
			invalidate: func(stateCache *customerstate.Cache, _ cachedCustomers) {
				stateCache.Invalidate(cache.Invalidation{
					Kind:        cache.InvalidationKindPlan,
					Environment: string(httpapi.EnvironmentTest),
					ID:          identifiers.Encode(identifiers.PrefixPlan, identifiers.New()),
				})
			},
			wantFirstReloaded:  true,
			wantSecondReloaded: true,
		},
		{
			name: "settings invalidation clears the environment",
			invalidate: func(stateCache *customerstate.Cache, _ cachedCustomers) {
				stateCache.Invalidate(cache.Invalidation{Kind: cache.InvalidationKindSettings, Environment: string(httpapi.EnvironmentLive)})
			},
			wantLiveReloaded: true,
		},
		{
			name: "other kinds are ignored",
			invalidate: func(stateCache *customerstate.Cache, _ cachedCustomers) {
				for _, kind := range []cache.InvalidationKind{cache.InvalidationKindPolicies, cache.InvalidationKindPricing, cache.InvalidationKindAPIKey, cache.InvalidationKindWebhookEndpoints} {
					stateCache.Invalidate(cache.Invalidation{Kind: kind, Environment: string(httpapi.EnvironmentTest)})
				}
			},
		},
		{
			name: "unknown environment clears every environment",
			invalidate: func(stateCache *customerstate.Cache, _ cachedCustomers) {
				stateCache.Invalidate(cache.Invalidation{Kind: cache.InvalidationKindPlan, Environment: "staging"})
			},
			wantFirstReloaded:  true,
			wantSecondReloaded: true,
			wantLiveReloaded:   true,
		},
		{
			name: "clear empties every environment",
			invalidate: func(stateCache *customerstate.Cache, _ cachedCustomers) {
				stateCache.Clear()
			},
			wantFirstReloaded:  true,
			wantSecondReloaded: true,
			wantLiveReloaded:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newHarness(t)
			customers := cachedCustomers{
				firstTestCustomerID:  harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active"),
				secondTestCustomerID: harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active"),
				liveCustomerID:       harness.createCustomer(t, httpapi.EnvironmentLive, withoutPlan, "active"),
			}
			harness.insertRollup(t, httpapi.EnvironmentTest, customers.firstTestCustomerID, september, dollars(50))
			harness.insertRollup(t, httpapi.EnvironmentTest, customers.secondTestCustomerID, september, dollars(50))
			harness.insertRollup(t, httpapi.EnvironmentLive, customers.liveCustomerID, september, dollars(50))
			stateCache := customerstate.NewCache(harness.loader, harness.clock)
			assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentTest, customers.firstTestCustomerID, dollars(50))
			assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentTest, customers.secondTestCustomerID, dollars(50))
			assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentLive, customers.liveCustomerID, dollars(50))
			harness.setEveryRollupRevenue(t.Context(), t, dollars(80))

			test.invalidate(stateCache, customers)

			assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentTest, customers.firstTestCustomerID, revenueAfterInvalidation(test.wantFirstReloaded))
			assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentTest, customers.secondTestCustomerID, revenueAfterInvalidation(test.wantSecondReloaded))
			assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentLive, customers.liveCustomerID, revenueAfterInvalidation(test.wantLiveReloaded))
		})
	}
}

func TestCacheDoesNotStoreALoadThatRacedAnInvalidation(t *testing.T) {
	harness := newHarness(t)
	customerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, september, dollars(50))
	hook := &queryHook{queryName: "SelectPeriodRevenue"}
	stateCache := customerstate.NewCache(harness.newHookedLoader(t, hook), harness.clock)
	hook.run = func(ctx context.Context) {
		harness.setEveryRollupRevenue(ctx, t, dollars(80))
		stateCache.Invalidate(customerInvalidation(httpapi.EnvironmentTest, customerID))
	}
	hook.armed.Store(true)

	assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentTest, customerID, dollars(50))
	assertNetRevenue(t, stateCache, harness, httpapi.EnvironmentTest, customerID, dollars(80))
}

func TestCacheReturnsLoadErrors(t *testing.T) {
	harness := newHarness(t)
	stateCache := customerstate.NewCache(harness.loader, harness.clock)
	if _, err := stateCache.Load(t.Context(), httpapi.EnvironmentTest, identifiers.New(), harness.clock.Now()); !errors.Is(err, httpapi.ErrNotFound) {
		t.Errorf("Load() error = %v, want %v", err, httpapi.ErrNotFound)
	}
}

func assertNetRevenue(t *testing.T, stateCache *customerstate.Cache, harness *harness, environment httpapi.Environment, customerID uuid.UUID, want money.Amount) {
	t.Helper()
	state, err := stateCache.Load(t.Context(), environment, customerID, harness.clock.Now())
	if err != nil {
		t.Fatalf("Load(%s, %s) error = %v", environment, customerID, err)
	}
	if state.CustomerID != customerID || state.NetRevenue != want {
		t.Errorf("Load(%s, %s) = customer %s net revenue %d, want net revenue %d", environment, customerID, state.CustomerID, state.NetRevenue, want)
	}
}

func customerInvalidation(environment httpapi.Environment, customerID uuid.UUID) cache.Invalidation {
	return cache.Invalidation{
		Kind:        cache.InvalidationKindCustomer,
		Environment: string(environment),
		ID:          identifiers.Encode(identifiers.PrefixCustomer, customerID),
	}
}

func revenueAfterInvalidation(reloaded bool) money.Amount {
	if reloaded {
		return dollars(80)
	}
	return dollars(50)
}
