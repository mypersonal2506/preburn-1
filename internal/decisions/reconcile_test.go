package decisions_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
)

func TestReconcileRepairsCounterEditedOutOfBand(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	edited := harness.currentCustomer(t, httpapi.EnvironmentTest)
	correct := harness.currentCustomer(t, httpapi.EnvironmentLive)
	for _, period := range []customerPeriod{edited, correct} {
		harness.reserve(t, period, chatFeature, 1000, harness.clock.Now().Add(decisionHoldTime))
		harness.settle(t, harness.reserve(t, period, chatFeature, 2000, harness.clock.Now().Add(decisionHoldTime)), 1500)
		harness.countOnly(t, period, imageFeature)
		harness.limitDenied(t, period, chatFeature)
		harness.fallback(t, period, chatFeature, amountPointer(300))
	}
	wantCounter := map[string]string{
		"settled": "1800", "settled:chat": "1800",
		"reserved": "1000", "reserved:chat": "1000",
		"count": "4", "count:chat": "3", "count:image": "1",
	}
	if diff := cmp.Diff(wantCounter, harness.counterFields(t, edited), ignoreZeroFields()); diff != "" {
		t.Fatalf("counter before the edit mismatch (-want +got):\n%s", diff)
	}
	harness.writeCounterFields(t, edited, map[string]string{"settled:chat": "5", "reserved": "42", "count:video": "7"})
	if err := harness.cache.Redis().HDel(t.Context(), harness.counterKey(edited), "count:image").Err(); err != nil {
		t.Fatalf("delete counter field: %v", err)
	}
	if err := harness.cache.Redis().Persist(t.Context(), harness.counterKey(edited)).Err(); err != nil {
		t.Fatalf("remove counter expiry: %v", err)
	}
	correctBefore := harness.counterFields(t, correct)
	correctExpiry := harness.expireTime(t, harness.counterKey(correct))
	registry := prometheus.NewRegistry()

	workReconcile(t, harness.newReconcileWorker(t, registry))

	if diff := cmp.Diff(wantCounter, harness.counterFields(t, edited), ignoreZeroFields()); diff != "" {
		t.Errorf("repaired counter mismatch (-want +got):\n%s", diff)
	}
	wantExpiry := time.Duration(currentPeriodEnd.Add(7*24*time.Hour).Unix()) * time.Second
	if expiry := harness.expireTime(t, harness.counterKey(edited)); expiry != wantExpiry {
		t.Errorf("repaired counter expires at %v, want %v", expiry, wantExpiry)
	}
	if diff := cmp.Diff(correctBefore, harness.counterFields(t, correct)); diff != "" {
		t.Errorf("correct counter changed (-before +after):\n%s", diff)
	}
	if expiry := harness.expireTime(t, harness.counterKey(correct)); expiry != correctExpiry {
		t.Errorf("correct counter expiry changed from %v to %v", correctExpiry, expiry)
	}
	if repairs := counterMetric(t, registry, repairsMetricName); repairs != 1 {
		t.Errorf("%s = %v, want 1", repairsMetricName, repairs)
	}
}

func TestReconcileKeepsAnIncrementThatLandsBetweenReadAndWrite(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	period := harness.currentCustomer(t, httpapi.EnvironmentTest)
	harness.reserve(t, period, chatFeature, 1000, harness.clock.Now().Add(decisionHoldTime))
	harness.writeCounterFields(t, period, map[string]string{"settled": "777", "settled:chat": "777", "count:chat": "9"})
	hook := &scriptHook{
		firstKey: harness.counterKey(period),
		before: func() {
			harness.fallback(t, period, chatFeature, amountPointer(900))
		},
	}
	harness.cache.Redis().AddHook(hook)
	registry := prometheus.NewRegistry()
	worker := harness.newReconcileWorker(t, registry)

	workReconcile(t, worker)

	if !hook.done.Load() {
		t.Fatal("the fallback never landed between the counter read and the reconcile write")
	}
	wantAfterRace := map[string]string{
		"settled": "1677", "settled:chat": "1677",
		"reserved": "1000", "reserved:chat": "1000",
		"count": "2", "count:chat": "10",
	}
	if diff := cmp.Diff(wantAfterRace, harness.counterFields(t, period), ignoreZeroFields()); diff != "" {
		t.Errorf("counter after the interleaved fallback mismatch (-want +got):\n%s", diff)
	}

	harness.clock.Advance(time.Minute)
	workReconcile(t, worker)

	wantRepaired := map[string]string{
		"settled": "900", "settled:chat": "900",
		"reserved": "1000", "reserved:chat": "1000",
		"count": "2", "count:chat": "2",
	}
	if diff := cmp.Diff(wantRepaired, harness.counterFields(t, period), ignoreZeroFields()); diff != "" {
		t.Errorf("counter after the next run mismatch (-want +got):\n%s", diff)
	}
	if repairs := counterMetric(t, registry, repairsMetricName); repairs != 1 {
		t.Errorf("%s = %v, want 1", repairsMetricName, repairs)
	}
}

func TestReconcileDropsAnAbandonedChangeAndRepairsItsCounter(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	period := harness.currentCustomer(t, httpapi.EnvironmentTest)
	decision := harness.reserve(t, period, chatFeature, 1000, harness.clock.Now().Add(decisionHoldTime))
	settlement := harness.recordSettlement(t, decision, 900)
	registry := prometheus.NewRegistry()
	worker := harness.newReconcileWorker(t, registry)
	workReconcile(t, worker)
	pending := map[string]string{"reserved": "1000", "reserved:chat": "1000", "count": "1", "count:chat": "1"}
	if diff := cmp.Diff(pending, harness.counterFields(t, period), ignoreZeroFields()); diff != "" {
		t.Errorf("counter with a pending change mismatch (-want +got):\n%s", diff)
	}
	harness.clock.Advance(time.Minute)

	workReconcile(t, worker)

	repaired := map[string]string{"settled": "900", "settled:chat": "900", "count": "1", "count:chat": "1"}
	if diff := cmp.Diff(repaired, harness.counterFields(t, period), ignoreZeroFields()); diff != "" {
		t.Errorf("counter after the change was abandoned mismatch (-want +got):\n%s", diff)
	}
	if status := harness.reservationStatus(t, decision.id); status != "settled" {
		t.Errorf("reservation status = %s, want settled", status)
	}
	if err := harness.counters.Settle(t.Context(), decision.id, settlement); !errors.Is(err, decisions.ErrChangeNotPending) {
		t.Errorf("late settle = %v, want ErrChangeNotPending", err)
	}
	if diff := cmp.Diff(repaired, harness.counterFields(t, period), ignoreZeroFields()); diff != "" {
		t.Errorf("counter after the late settle mismatch (-want +got):\n%s", diff)
	}
	if repairs := counterMetric(t, registry, repairsMetricName); repairs != 1 {
		t.Errorf("%s = %v, want 1", repairsMetricName, repairs)
	}
}

func TestReconcileSweepsEveryCounterHourlyAndActiveCustomersBetween(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	idle := harness.currentCustomer(t, httpapi.EnvironmentTest)
	harness.clock.Set(counterJobsStart.Add(-48 * time.Hour))
	harness.fallback(t, idle, chatFeature, amountPointer(250))
	harness.clock.Set(counterJobsStart)
	corruptIdle := func() {
		harness.writeCounterFields(t, idle, map[string]string{"settled:chat": "1"})
	}
	idleRepaired := map[string]string{"settled": "250", "settled:chat": "250", "count": "1", "count:chat": "1"}
	idleCorrupted := map[string]string{"settled": "250", "settled:chat": "1", "count": "1", "count:chat": "1"}
	registry := prometheus.NewRegistry()
	worker := harness.newReconcileWorker(t, registry)
	corruptIdle()

	workReconcile(t, worker)

	if diff := cmp.Diff(idleRepaired, harness.counterFields(t, idle), ignoreZeroFields()); diff != "" {
		t.Errorf("idle counter after the first full sweep mismatch (-want +got):\n%s", diff)
	}

	corruptIdle()
	harness.clock.Advance(time.Minute)
	active := harness.currentCustomer(t, httpapi.EnvironmentTest)
	harness.reserve(t, active, videoFeature, 600, harness.clock.Now().Add(decisionHoldTime))
	harness.writeCounterFields(t, active, map[string]string{"reserved:video": "3"})

	workReconcile(t, worker)

	if diff := cmp.Diff(idleCorrupted, harness.counterFields(t, idle), ignoreZeroFields()); diff != "" {
		t.Errorf("idle counter after an incremental run mismatch (-want +got):\n%s", diff)
	}
	activeRepaired := map[string]string{"reserved": "600", "reserved:video": "600", "count": "1", "count:video": "1"}
	if diff := cmp.Diff(activeRepaired, harness.counterFields(t, active), ignoreZeroFields()); diff != "" {
		t.Errorf("active counter after an incremental run mismatch (-want +got):\n%s", diff)
	}

	harness.clock.Set(counterJobsStart.Add(59 * time.Minute))
	workReconcile(t, worker)
	if diff := cmp.Diff(idleCorrupted, harness.counterFields(t, idle), ignoreZeroFields()); diff != "" {
		t.Errorf("idle counter before the hour passed mismatch (-want +got):\n%s", diff)
	}

	harness.clock.Set(counterJobsStart.Add(time.Hour))
	workReconcile(t, worker)

	if diff := cmp.Diff(idleRepaired, harness.counterFields(t, idle), ignoreZeroFields()); diff != "" {
		t.Errorf("idle counter after the hourly full sweep mismatch (-want +got):\n%s", diff)
	}
	if repairs := counterMetric(t, registry, repairsMetricName); repairs != 3 {
		t.Errorf("%s = %v, want 3", repairsMetricName, repairs)
	}
}

func TestReconcileExpiresDueReservationsBeforeComparing(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	period := harness.currentCustomer(t, httpapi.EnvironmentTest)
	due := harness.reserve(t, period, chatFeature, 1000, harness.clock.Now().Add(decisionHoldTime))
	now := harness.clock.Now().Add(decisionHoldTime + 700*time.Millisecond)
	withinLastSecond := harness.reserve(t, period, chatFeature, 200, now.Add(-500*time.Millisecond))
	harness.clock.Set(now)
	registry := prometheus.NewRegistry()

	workReconcile(t, harness.newReconcileWorker(t, registry))

	wantCounter := map[string]string{"reserved": "200", "reserved:chat": "200", "count": "2", "count:chat": "2"}
	if diff := cmp.Diff(wantCounter, harness.counterFields(t, period), ignoreZeroFields()); diff != "" {
		t.Errorf("counter mismatch (-want +got):\n%s", diff)
	}
	if status := harness.decisionStatus(t, due.id); status != "expired" {
		t.Errorf("due decision status = %q, want expired", status)
	}
	if status := harness.reservationStatus(t, withinLastSecond.id); status != "reserved" {
		t.Errorf("reservation expiring within the last second has status %q, want reserved", status)
	}
	if repairs := counterMetric(t, registry, repairsMetricName); repairs != 0 {
		t.Errorf("%s = %v, want 0", repairsMetricName, repairs)
	}
	if expired := counterMetric(t, registry, expiredMetricName); expired != 1 {
		t.Errorf("%s = %v, want 1", expiredMetricName, expired)
	}
}
