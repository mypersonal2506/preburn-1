package decisions_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/ledger"
)

const waitingAdvisoryLocksQuery = `SELECT count(*) FROM pg_locks
WHERE locktype = 'advisory' AND NOT granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`

func TestEnsureCountersReadyRebuildsDeletedCounters(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	first := harness.currentCustomer(t, httpapi.EnvironmentTest)
	second := harness.currentCustomer(t, httpapi.EnvironmentLive)
	endedWithReservation := harness.customer(t, httpapi.EnvironmentTest, counterJobsStart.AddDate(0, -1, 0), counterJobsStart.Add(-time.Minute))
	endedWithoutReservation := harness.customer(t, httpapi.EnvironmentTest, counterJobsStart.AddDate(0, -1, 0), counterJobsStart.Add(-time.Minute))
	harness.clock.Set(counterJobsStart.Add(-5 * time.Minute))
	holdUntil := harness.clock.Now().Add(decisionHoldTime)
	open := []checkedDecision{
		harness.reserve(t, first, chatFeature, 1000, holdUntil),
		harness.reserve(t, second, videoFeature, 700, holdUntil.Add(time.Minute)),
		harness.reserve(t, endedWithReservation, chatFeature, 90, holdUntil.Add(2*time.Minute)),
	}
	settled := harness.reserve(t, first, chatFeature, 2000, holdUntil)
	harness.settle(t, settled, 1500)
	released := harness.reserve(t, first, imageFeature, 300, holdUntil)
	harness.release(t, released, decisions.ReservationStatusReleased)
	expired := harness.reserve(t, first, chatFeature, 400, holdUntil)
	harness.release(t, expired, decisions.ReservationStatusExpired)
	harness.countOnly(t, first, imageFeature)
	harness.limitDenied(t, first, chatFeature)
	uncosted := harness.fallback(t, first, chatFeature, nil)
	harness.correction(t, first, chatFeature, uncosted, 50)
	harness.fallback(t, second, videoFeature, amountPointer(80))
	harness.fallback(t, endedWithoutReservation, chatFeature, amountPointer(60))
	harness.clock.Set(counterJobsStart)
	counterKeys := []string{harness.counterKey(first), harness.counterKey(second), harness.counterKey(endedWithReservation)}
	openReservationKeys := harness.reservationKeys(open)
	wantCounters := harness.keyStates(t, counterKeys)
	wantReservations := harness.keyStates(t, openReservationKeys)
	wantMembers := harness.expiringMembers(t)
	notRebuiltKeys := append(harness.reservationKeys([]checkedDecision{settled, released, expired}), harness.counterKey(endedWithoutReservation))
	deletedKeys := slices.Concat(counterKeys, openReservationKeys, notRebuiltKeys, []string{harness.cache.Key(reservationsExpiring), harness.cache.Key(countersReadyKeyName)})
	if err := harness.cache.Redis().Del(t.Context(), deletedKeys...).Err(); err != nil {
		t.Fatalf("delete counter keys: %v", err)
	}
	registry := prometheus.NewRegistry()
	bootstrap := harness.newBootstrap(t, registry)

	if err := bootstrap.EnsureCountersReady(t.Context()); err != nil {
		t.Fatalf("ensure counters ready: %v", err)
	}

	if diff := cmp.Diff(wantCounters, harness.keyStates(t, counterKeys), ignoreZeroFields()); diff != "" {
		t.Errorf("rebuilt counters mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantReservations, harness.keyStates(t, openReservationKeys)); diff != "" {
		t.Errorf("rebuilt reservations mismatch (-want +got):\n%s", diff)
	}
	if rebuilt, err := harness.cache.Redis().Exists(t.Context(), notRebuiltKeys...).Result(); err != nil || rebuilt != 0 {
		t.Errorf("finished reservations and ended counters rebuilt = %d err=%v, want 0", rebuilt, err)
	}
	if diff := cmp.Diff(wantMembers, harness.expiringMembers(t)); diff != "" {
		t.Errorf("reservations_expiring mismatch (-want +got):\n%s", diff)
	}
	marker, err := harness.cache.Redis().Get(t.Context(), harness.cache.Key(countersReadyKeyName)).Result()
	if err != nil || marker != counterJobsStart.Format(time.RFC3339Nano) {
		t.Errorf("counters_ready = %q err=%v, want the rebuild time", marker, err)
	}
	if err := bootstrap.EnsureCountersReady(t.Context()); err != nil {
		t.Fatalf("ensure counters ready once the marker exists: %v", err)
	}
	if rebuilds := histogramSampleCount(t, registry, rebuildMetricName); rebuilds != 1 {
		t.Errorf("%s sample count = %d, want 1", rebuildMetricName, rebuilds)
	}
}

func TestEnsureCountersReadyExpiresLapsedReservations(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	period := harness.currentCustomer(t, httpapi.EnvironmentTest)
	now := counterJobsStart.Add(700 * time.Millisecond)
	lapsed := harness.reserve(t, period, chatFeature, 500, counterJobsStart.Add(-time.Minute))
	settled := harness.reserve(t, period, chatFeature, 200, counterJobsStart.Add(-2*time.Minute))
	harness.settle(t, settled, 150)
	withinLastSecond := harness.reserve(t, period, chatFeature, 300, now.Add(-500*time.Millisecond))
	deletedKeys := append(harness.reservationKeys([]checkedDecision{lapsed, settled, withinLastSecond}), harness.counterKey(period), harness.cache.Key(reservationsExpiring), harness.cache.Key(countersReadyKeyName))
	if err := harness.cache.Redis().Del(t.Context(), deletedKeys...).Err(); err != nil {
		t.Fatalf("delete counter keys: %v", err)
	}
	harness.clock.Set(now)

	if err := harness.newBootstrap(t, prometheus.NewRegistry()).EnsureCountersReady(t.Context()); err != nil {
		t.Fatalf("ensure counters ready: %v", err)
	}

	wantStatuses := map[string]string{"lapsed": "expired", "settled": "settled", "within the last second": "reserved"}
	gotStatuses := map[string]string{
		"lapsed":                 harness.decisionStatus(t, lapsed.id),
		"settled":                harness.decisionStatus(t, settled.id),
		"within the last second": harness.decisionStatus(t, withinLastSecond.id),
	}
	if diff := cmp.Diff(wantStatuses, gotStatuses); diff != "" {
		t.Errorf("decision statuses mismatch (-want +got):\n%s", diff)
	}
	wantCounter := map[string]string{
		"settled": "150", "settled:chat": "150",
		"reserved": "300", "reserved:chat": "300",
		"count": "3", "count:chat": "3",
	}
	if diff := cmp.Diff(wantCounter, harness.counterFields(t, period), ignoreZeroFields()); diff != "" {
		t.Errorf("rebuilt counter mismatch (-want +got):\n%s", diff)
	}
	wantMembers := []redis.Z{{Score: float64(counterJobsStart.Unix() + 1), Member: withinLastSecond.id.String()}}
	if diff := cmp.Diff(wantMembers, harness.expiringMembers(t)); diff != "" {
		t.Errorf("reservations_expiring mismatch (-want +got):\n%s", diff)
	}
	wantRefreshes := []ledger.RollupRefreshArgs{ledger.NewRollupRefreshArgs(period.environment, period.customerID, period.start)}
	if diff := cmp.Diff(wantRefreshes, harness.rollupRefreshJobs(t)); diff != "" {
		t.Errorf("rollup refresh jobs mismatch (-want +got):\n%s", diff)
	}
}

func TestEnsureCountersReadyRebuildsOnceForConcurrentCallers(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	period := harness.currentCustomer(t, httpapi.EnvironmentTest)
	harness.reserve(t, period, chatFeature, 1000, harness.clock.Now().Add(decisionHoldTime))
	if err := harness.cache.Redis().Del(t.Context(), harness.counterKey(period), harness.cache.Key(countersReadyKeyName)).Err(); err != nil {
		t.Fatalf("delete counter: %v", err)
	}
	releaseLock, err := database.AcquireAdvisoryLock(t.Context(), harness.pool, "counters_rebuild")
	if err != nil {
		t.Fatalf("acquire rebuild lock: %v", err)
	}
	releaseLock = sync.OnceFunc(releaseLock)
	defer releaseLock()
	registries := []*prometheus.Registry{prometheus.NewRegistry(), prometheus.NewRegistry()}
	results := make(chan error, len(registries))

	for _, registry := range registries {
		bootstrap := harness.newBootstrap(t, registry)
		go func() {
			results <- bootstrap.EnsureCountersReady(t.Context())
		}()
	}
	pollUntil(t, "both callers to wait for the rebuild lock", func(ctx context.Context) bool {
		var waiting int
		if err := harness.pool.QueryRow(ctx, waitingAdvisoryLocksQuery).Scan(&waiting); err != nil {
			t.Fatalf("count waiting advisory locks: %v", err)
		}
		return waiting == len(registries)
	})
	releaseLock()

	for range registries {
		if err := <-results; err != nil {
			t.Errorf("ensure counters ready: %v", err)
		}
	}
	var rebuilds uint64
	for _, registry := range registries {
		rebuilds += histogramSampleCount(t, registry, rebuildMetricName)
	}
	if rebuilds != 1 {
		t.Errorf("rebuilds = %d, want 1", rebuilds)
	}
	wantCounter := map[string]string{"reserved": "1000", "reserved:chat": "1000", "count": "1", "count:chat": "1"}
	if diff := cmp.Diff(wantCounter, harness.counterFields(t, period), ignoreZeroFields()); diff != "" {
		t.Errorf("rebuilt counter mismatch (-want +got):\n%s", diff)
	}
}

func TestCountersReadinessCheckFollowsTheMarker(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	deleteCountersMarker(t, harness.cache)
	check := decisions.CountersReadinessCheck(harness.counters)
	if check.Name != "counters" {
		t.Errorf("readiness check name = %q, want counters", check.Name)
	}

	if err := check.Check(t.Context()); !errors.Is(err, decisions.ErrCountersNotReady) {
		t.Errorf("check before the rebuild = %v, want ErrCountersNotReady", err)
	}
	if err := harness.newBootstrap(t, prometheus.NewRegistry()).EnsureCountersReady(t.Context()); err != nil {
		t.Fatalf("ensure counters ready: %v", err)
	}
	if err := check.Check(t.Context()); err != nil {
		t.Errorf("check after the rebuild = %v, want nil", err)
	}
}
