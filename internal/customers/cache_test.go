package customers_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
)

const userInsertQueryPrefix = "-- name: InsertCustomerUserIfMissing "

type userInsertCounter struct {
	inserts atomic.Int64
}

func TestCacheEnsureCreatesMissingCustomer(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	customer, err := harness.service.Cache().Ensure(t.Context(), httpapi.EnvironmentTest, testExternalID)
	if err != nil {
		t.Fatalf("ensure through the cache: %v", err)
	}

	stored, err := harness.service.CustomerByExternalID(t.Context(), httpapi.EnvironmentTest, testExternalID)
	if err != nil {
		t.Fatalf("load customer: %v", err)
	}
	if diff := cmp.Diff(stored, customer); diff != "" {
		t.Errorf("cached customer mismatch (-stored +cached):\n%s", diff)
	}
}

func TestCachedCustomerExpiresAfterThirtySeconds(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	customerCache := harness.service.Cache()
	customer := ensureDisplayName(t, customerCache, nil)
	harness.renameInDatabase(t, customer.ID)

	harness.clock.Advance(30*time.Second - time.Nanosecond)
	ensureDisplayName(t, customerCache, nil)
	harness.clock.Advance(time.Nanosecond)
	ensureDisplayName(t, customerCache, pointer(renamedDisplayName))
}

func TestCacheKeepsEnvironmentsApart(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	customerCache := harness.service.Cache()

	testCustomer, err := customerCache.Ensure(t.Context(), httpapi.EnvironmentTest, testExternalID)
	if err != nil {
		t.Fatalf("ensure test customer: %v", err)
	}
	liveCustomer, err := customerCache.Ensure(t.Context(), httpapi.EnvironmentLive, testExternalID)
	if err != nil {
		t.Fatalf("ensure live customer: %v", err)
	}

	if testCustomer.ID == liveCustomer.ID || liveCustomer.Environment != httpapi.EnvironmentLive {
		t.Errorf("live customer = %+v, want a separate live customer from test customer %s", liveCustomer, testCustomer.ID)
	}
}

func TestUpsertClearsCacheOfProcess(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	customerCache := harness.service.Cache()
	ensureDisplayName(t, customerCache, nil)

	harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{DisplayName: pointer("Acme")})

	ensureDisplayName(t, customerCache, pointer("Acme"))
}

func TestUpsertInAnotherProcessClearsCacheThroughInvalidation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	otherProcess := customers.NewService(harness.pool, harness.cache, harness.clock).Cache()
	customer := ensureDisplayName(t, otherProcess, nil)
	harness.renameInDatabase(t, customer.ID)
	ensureDisplayName(t, otherProcess, nil)
	subscribe(t, harness.cache, otherProcess)

	harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{DisplayName: pointer("Acme")})

	waitFor(t, "upserted customer in the other process", func() bool {
		cached, err := otherProcess.Ensure(t.Context(), httpapi.EnvironmentTest, testExternalID)
		if err != nil {
			t.Fatalf("ensure in the other process: %v", err)
		}
		return cached.DisplayName != nil && *cached.DisplayName == "Acme"
	})
}

func TestLookupRacingUpsertIsNotCached(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		upsert func(ctx context.Context, harness *harness, hooked *customers.Service) error
	}{
		{name: "upsert in this process", upsert: func(ctx context.Context, _ *harness, hooked *customers.Service) error {
			_, err := hooked.Upsert(ctx, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{DisplayName: pointer("Acme")})
			return err
		}},
		{name: "invalidation from another process", upsert: func(ctx context.Context, harness *harness, hooked *customers.Service) error {
			customer, err := harness.service.Upsert(ctx, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{DisplayName: pointer("Acme")})
			if err != nil {
				return err
			}
			hooked.Cache().Invalidate(cache.Invalidation{
				Kind:        cache.InvalidationKindCustomer,
				Environment: string(httpapi.EnvironmentTest),
				ID:          identifiers.Encode(identifiers.PrefixCustomer, customer.ID),
			})
			return nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			harness := newHarness(t)
			hook := &queryHook{queryName: "SelectCustomerByExternalID"}
			hooked := harness.newHookedService(t, hook)
			harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{})
			hook.run = func(ctx context.Context) {
				if err := test.upsert(context.WithoutCancel(ctx), harness, hooked); err != nil {
					t.Errorf("upsert during the lookup: %v", err)
				}
			}
			hook.armed.Store(true)
			ensureDisplayName(t, hooked.Cache(), nil)

			ensureDisplayName(t, hooked.Cache(), pointer("Acme"))
		})
	}
}

func TestInvalidationOfOtherKindsKeepsCache(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	customerCache := harness.service.Cache()
	customer := ensureDisplayName(t, customerCache, nil)
	harness.renameInDatabase(t, customer.ID)

	customerCache.Invalidate(cache.Invalidation{Kind: cache.InvalidationKindPlan, Environment: string(httpapi.EnvironmentTest)})
	ensureDisplayName(t, customerCache, nil)
	customerCache.Invalidate(cache.Invalidation{
		Kind:        cache.InvalidationKindCustomer,
		Environment: string(httpapi.EnvironmentTest),
		ID:          identifiers.Encode(identifiers.PrefixCustomer, customer.ID),
	})
	ensureDisplayName(t, customerCache, pointer(renamedDisplayName))
}

func TestClearForgetsCustomers(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	customerCache := harness.service.Cache()
	customer := ensureDisplayName(t, customerCache, nil)
	harness.renameInDatabase(t, customer.ID)

	customerCache.Clear()

	ensureDisplayName(t, customerCache, pointer(renamedDisplayName))
}

func ensureDisplayName(t *testing.T, customerCache *customers.Cache, want *string) customers.Customer {
	t.Helper()
	customer, err := customerCache.Ensure(t.Context(), httpapi.EnvironmentTest, testExternalID)
	if err != nil {
		t.Fatalf("ensure %s through the cache: %v", testExternalID, err)
	}
	if diff := cmp.Diff(want, customer.DisplayName); diff != "" {
		t.Errorf("display name mismatch (-want +got):\n%s", diff)
	}
	return customer
}

func TestCacheEnsureUserWritesAKnownUserOnce(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	counter := &userInsertCounter{}
	customerCache := harness.newCountingService(t, counter).Cache()
	customer := harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{})

	first := ensureUser(t, customerCache, customer.ID)
	second := ensureUser(t, customerCache, customer.ID)
	if first.ID != second.ID || first.ExternalID != "user-1" || counter.inserts.Load() != 1 {
		t.Errorf("users %s and %s after %d inserts, want the same user-1 after 1 insert", first.ID, second.ID, counter.inserts.Load())
	}

	harness.clock.Advance(30 * time.Second)
	ensureUser(t, customerCache, customer.ID)
	customerCache.Invalidate(cache.Invalidation{
		Kind:        cache.InvalidationKindCustomer,
		Environment: string(httpapi.EnvironmentTest),
		ID:          identifiers.Encode(identifiers.PrefixCustomer, customer.ID),
	})
	ensureUser(t, customerCache, customer.ID)
	customerCache.Clear()
	ensureUser(t, customerCache, customer.ID)
	if inserts := counter.inserts.Load(); inserts != 4 {
		t.Errorf("inserts = %d, want 4 after expiry, invalidation and clear", inserts)
	}
}

func (harness *harness) newCountingService(t *testing.T, counter *userInsertCounter) *customers.Service {
	t.Helper()
	configuration := harness.pool.Config().Copy()
	configuration.ConnConfig.Tracer = counter
	countingPool, err := pgxpool.NewWithConfig(t.Context(), configuration)
	if err != nil {
		t.Fatalf("open counting pool: %v", err)
	}
	t.Cleanup(countingPool.Close)
	return customers.NewService(countingPool, harness.cache, harness.clock)
}

func (counter *userInsertCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, userInsertQueryPrefix) {
		counter.inserts.Add(1)
	}
	return ctx
}

func (*userInsertCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func ensureUser(t *testing.T, customerCache *customers.Cache, customerID uuid.UUID) customers.CustomerUser {
	t.Helper()
	user, err := customerCache.EnsureUser(t.Context(), httpapi.EnvironmentTest, customerID, "user-1")
	if err != nil {
		t.Fatalf("ensure user through the cache: %v", err)
	}
	return user
}
