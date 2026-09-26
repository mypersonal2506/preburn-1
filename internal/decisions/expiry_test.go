package decisions_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/logging"
)

const dueReservationCount = 501

func TestCounterJobsDeclareKindAndQueue(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		arguments interface {
			Kind() string
			InsertOpts() river.InsertOpts
		}
		wantKind string
	}{
		{arguments: decisions.ExpiryArgs{}, wantKind: "reservations_expire"},
		{arguments: decisions.ReconcileArgs{}, wantKind: "counters_reconcile"},
	}
	for _, testCase := range testCases {
		if kind := testCase.arguments.Kind(); kind != testCase.wantKind {
			t.Errorf("kind = %s, want %s", kind, testCase.wantKind)
		}
		if queue := testCase.arguments.InsertOpts().Queue; queue != jobs.QueueDefault {
			t.Errorf("%s queue = %s, want %s", testCase.wantKind, queue, jobs.QueueDefault)
		}
	}
}

func TestCounterJobConstructorsRejectDuplicateMetrics(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	registry := prometheus.NewRegistry()
	expiry := harness.newExpiryWorker(t, registry)
	if _, err := decisions.NewReconcileWorker(harness.pool, harness.counters, expiry, harness.clock, registry); err != nil {
		t.Fatalf("first reconcile worker: %v", err)
	}
	harness.newBootstrap(t, registry)

	if _, err := decisions.NewExpiryWorker(harness.pool, harness.counters, harness.jobs, harness.clock, registry); err == nil {
		t.Error("second expiry worker on the same registry succeeded, want an error")
	}
	if _, err := decisions.NewReconcileWorker(harness.pool, harness.counters, expiry, harness.clock, registry); err == nil {
		t.Error("second reconcile worker on the same registry succeeded, want an error")
	}
	if _, err := decisions.NewCounterBootstrap(harness.pool, harness.counters, harness.jobs, harness.clock, logging.New(t.Output(), slog.LevelDebug), registry); err == nil {
		t.Error("second counter bootstrap on the same registry succeeded, want an error")
	}
}

func TestExpiryRefreshesTheRollupsOfExpiredDecisions(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	first := harness.currentCustomer(t, httpapi.EnvironmentTest)
	second := harness.currentCustomer(t, httpapi.EnvironmentLive)
	dueAt := harness.clock.Now().Add(decisionHoldTime)
	harness.reserve(t, first, chatFeature, 100, dueAt)
	harness.reserve(t, first, imageFeature, 200, dueAt)
	harness.reserve(t, second, chatFeature, 300, dueAt)
	harness.settle(t, harness.reserve(t, second, chatFeature, 400, dueAt), 350)
	harness.clock.Set(dueAt)

	workExpiry(t, harness.newExpiryWorker(t, prometheus.NewRegistry()))

	want := []ledger.RollupRefreshArgs{
		ledger.NewRollupRefreshArgs(first.environment, first.customerID, first.start),
		ledger.NewRollupRefreshArgs(second.environment, second.customerID, second.start),
	}
	if diff := cmp.Diff(want, harness.rollupRefreshJobs(t), cmpopts.SortSlices(func(left, right ledger.RollupRefreshArgs) bool {
		return left.CustomerID.String() < right.CustomerID.String()
	})); diff != "" {
		t.Errorf("rollup refresh jobs mismatch (-want +got):\n%s", diff)
	}
}

func TestExpiryReleasesDueReservationsAndMarksDecisionsExpired(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	period := harness.currentCustomer(t, httpapi.EnvironmentTest)
	dueAt := harness.clock.Now().Add(decisionHoldTime)
	due := make([]checkedDecision, 0, dueReservationCount)
	for range dueReservationCount {
		due = append(due, harness.reserve(t, period, chatFeature, 100, dueAt))
	}
	reported := harness.reserve(t, period, chatFeature, 700, dueAt)
	harness.settle(t, reported, 650)
	withoutHash := harness.reserve(t, period, chatFeature, 50, dueAt)
	if err := harness.cache.Redis().Del(t.Context(), harness.counters.ReservationKey(withoutHash.id)).Err(); err != nil {
		t.Fatalf("delete reservation hash: %v", err)
	}
	pending := harness.reserve(t, period, imageFeature, 300, dueAt.Add(time.Second))
	harness.clock.Set(dueAt)
	registry := prometheus.NewRegistry()

	workExpiry(t, harness.newExpiryWorker(t, registry))

	for _, decision := range due {
		if status := harness.reservationStatus(t, decision.id); status != "expired" {
			t.Fatalf("due reservation status = %q, want expired", status)
		}
	}
	if status := harness.reservationStatus(t, pending.id); status != "reserved" {
		t.Errorf("pending reservation status = %q, want reserved", status)
	}
	wantCounter := map[string]string{
		"settled": "650", "settled:chat": "650",
		"reserved": "350", "reserved:chat": "50", "reserved:image": "300",
		"count": "504", "count:chat": "503", "count:image": "1",
	}
	if diff := cmp.Diff(wantCounter, harness.counterFields(t, period), ignoreZeroFields()); diff != "" {
		t.Errorf("counter mismatch (-want +got):\n%s", diff)
	}
	wantMembers := []redis.Z{{Score: float64(pending.expiresAt.Unix()), Member: pending.id.String()}}
	if diff := cmp.Diff(wantMembers, harness.expiringMembers(t)); diff != "" {
		t.Errorf("reservations_expiring mismatch (-want +got):\n%s", diff)
	}
	wantStatuses := map[string]int{"expired": dueReservationCount + 1, "settled": 1, "reserved": 1}
	if diff := cmp.Diff(wantStatuses, harness.decisionStatusCounts(t)); diff != "" {
		t.Errorf("decision statuses mismatch (-want +got):\n%s", diff)
	}
	if status := harness.decisionStatus(t, pending.id); status != "reserved" {
		t.Errorf("pending decision status = %q, want reserved", status)
	}
	if expired := counterMetric(t, registry, expiredMetricName); expired != dueReservationCount {
		t.Errorf("%s = %v, want %d", expiredMetricName, expired, dueReservationCount)
	}
}
