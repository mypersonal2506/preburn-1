package policies_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/policies"
)

type queryHook struct {
	queryName string
	run       func(ctx context.Context)
	armed     atomic.Bool
}

type hookedQueryKey struct{}

func TestCacheSeesANewPolicyAfterTheInvalidation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	otherProcess := policies.NewService(harness.pool, harness.cache, harness.files, harness.clock)
	assertActiveNames(t, otherProcess.Cache(), httpapi.EnvironmentTest)
	subscribeCache(t, harness.cache, otherProcess.Cache())

	harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"name": `"Fresh"`}))

	waitFor(t, "new policy in the other process", func() bool {
		return slices.Equal(activeNames(t, otherProcess.Cache(), httpapi.EnvironmentTest), []string{"Fresh"})
	})
}

func TestCacheSeesTheChangesOfItsServiceAtOnce(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	activePolicies := harness.service.Cache()
	assertActiveNames(t, activePolicies, httpapi.EnvironmentTest)

	created := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"name": `"Fresh"`}))

	assertActiveNames(t, activePolicies, httpapi.EnvironmentTest, "Fresh")
	assertStatus(t, harness.memberRequest(t, http.MethodPatch, policyPath(created["id"]), httpapi.EnvironmentTest, `{"status": "disabled"}`), http.StatusOK)
	assertActiveNames(t, activePolicies, httpapi.EnvironmentTest)
}

func TestCacheKeepsActivePoliciesForSixtySeconds(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	activePolicies := harness.service.Cache()
	assertActiveNames(t, activePolicies, httpapi.EnvironmentTest)
	createThrough(t, policies.NewService(harness.pool, harness.cache, harness.files, harness.clock), httpapi.EnvironmentTest, "Fresh")

	harness.clock.Advance(time.Minute - time.Nanosecond)
	assertActiveNames(t, activePolicies, httpapi.EnvironmentTest)
	harness.clock.Advance(time.Nanosecond)
	assertActiveNames(t, activePolicies, httpapi.EnvironmentTest, "Fresh")
}

func TestCacheHoldsTheActivePoliciesOfEachEnvironment(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"name": `"Active"`}))
	harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"name": `"Disabled"`, "status": `"disabled"`}))
	archived := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"name": `"Archived"`}))
	assertStatus(t, harness.memberRequest(t, http.MethodPatch, policyPath(archived["id"]), httpapi.EnvironmentTest, `{"status": "archived"}`), http.StatusOK)
	harness.createPolicy(t, httpapi.EnvironmentLive, documentJSON(map[string]string{"name": `"Live"`}))

	assertActiveNames(t, harness.service.Cache(), httpapi.EnvironmentTest, "Active")
	assertActiveNames(t, harness.service.Cache(), httpapi.EnvironmentLive, "Live")
}

func TestCacheClearsOnlyOnThePoliciesInvalidation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	activePolicies := harness.service.Cache()
	assertActiveNames(t, activePolicies, httpapi.EnvironmentTest)
	createThrough(t, policies.NewService(harness.pool, harness.cache, harness.files, harness.clock), httpapi.EnvironmentTest, "Fresh")

	activePolicies.Invalidate(cache.Invalidation{Kind: cache.InvalidationKindPlan, Environment: string(httpapi.EnvironmentTest)})
	assertActiveNames(t, activePolicies, httpapi.EnvironmentTest)
	activePolicies.Invalidate(cache.Invalidation{Kind: cache.InvalidationKindPolicies, Environment: string(httpapi.EnvironmentTest)})
	assertActiveNames(t, activePolicies, httpapi.EnvironmentTest, "Fresh")
}

func TestCacheLoadRacingAPolicyChangeIsNotCached(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	hook := &queryHook{queryName: "ListActivePolicies"}
	hooked := newHookedService(t, harness, hook)
	hook.run = func(ctx context.Context) {
		if _, err := hooked.Create(context.WithoutCancel(ctx), httpapi.EnvironmentTest, namedDocument("Fresh")); err != nil {
			t.Errorf("create policy during the load: %v", err)
		}
	}
	hook.armed.Store(true)

	assertActiveNames(t, hooked.Cache(), httpapi.EnvironmentTest)
	assertActiveNames(t, hooked.Cache(), httpapi.EnvironmentTest, "Fresh")
}

func createThrough(t *testing.T, service *policies.Service, environment httpapi.Environment, name string) {
	t.Helper()
	if _, err := service.Create(t.Context(), environment, namedDocument(name)); err != nil {
		t.Fatalf("create policy %s: %v", name, err)
	}
}

func namedDocument(name string) []byte {
	return []byte(documentJSON(map[string]string{"name": quoted(name)}))
}

func newHookedService(t *testing.T, harness *harness, hook *queryHook) *policies.Service {
	t.Helper()
	configuration := harness.pool.Config().Copy()
	configuration.ConnConfig.Tracer = hook
	hookedPool, err := pgxpool.NewWithConfig(t.Context(), configuration)
	if err != nil {
		t.Fatalf("open hooked pool: %v", err)
	}
	t.Cleanup(hookedPool.Close)
	return policies.NewService(hookedPool, harness.cache, harness.files, harness.clock)
}

func (hook *queryHook) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name: "+hook.queryName+" ") {
		return context.WithValue(ctx, hookedQueryKey{}, true)
	}
	return ctx
}

func (hook *queryHook) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(hookedQueryKey{}) != nil && hook.armed.Swap(false) {
		hook.run(ctx)
	}
}

func assertActiveNames(t *testing.T, activePolicies *policies.Cache, environment httpapi.Environment, want ...string) {
	t.Helper()
	if diff := cmp.Diff(want, activeNames(t, activePolicies, environment)); diff != "" {
		t.Errorf("active policies of %s mismatch (-want +got):\n%s", environment, diff)
	}
}

func activeNames(t *testing.T, activePolicies *policies.Cache, environment httpapi.Environment) []string {
	t.Helper()
	active, err := activePolicies.Active(t.Context(), environment)
	if err != nil {
		t.Fatalf("active policies of %s: %v", environment, err)
	}
	var names []string
	for _, policy := range active {
		names = append(names, policy.Name)
	}
	slices.Sort(names)
	return names
}
