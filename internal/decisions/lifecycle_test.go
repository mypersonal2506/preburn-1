package decisions_test

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/signals"
)

const (
	concurrentCustomers  = 4
	concurrentClients    = 12
	concurrentIterations = 40
)

func TestReconcileBetweenReportCommitAndSettleAddsNoDrift(t *testing.T) {
	harness := newLifecycleHarness(t)
	workReconcile(t, harness.reconcile)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	customer := harness.ensureCustomer(t)
	hook := harness.hook(&scriptHook{
		firstKey: harness.counters.ReservationKey(harness.decisionID(t, checked)),
		before:   func() { workReconcile(t, harness.reconcile) },
	})

	harness.report(t, serverReport(checked.DecisionID, "6"))

	if !hook.done.Load() {
		t.Fatal("reconcile never ran between the report commit and the settle")
	}
	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
}

func TestReconcileBetweenFallbackCommitAndSettleAddsNoDrift(t *testing.T) {
	harness := newLifecycleHarness(t)
	workReconcile(t, harness.reconcile)
	customer, err := harness.customers.Ensure(t.Context(), httpapi.EnvironmentTest, fallbackCustomer)
	if err != nil {
		t.Fatalf("ensure fallback customer: %v", err)
	}
	hook := harness.hook(&scriptHook{
		firstKey: harness.counters.CounterKey(httpapi.EnvironmentTest, customer.ID, checkPeriod.Start),
		before:   func() { workReconcile(t, harness.reconcile) },
	})

	harness.report(t, fallbackReport(identifiers.New().String(), "6"))

	if !hook.done.Load() {
		t.Fatal("reconcile never ran between the fallback commit and its settle")
	}
	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
}

func TestReconcileBetweenReleaseCommitAndRedisReleaseAddsNoDrift(t *testing.T) {
	harness := newLifecycleHarness(t)
	workReconcile(t, harness.reconcile)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	customer := harness.ensureCustomer(t)
	decisionID := harness.decisionID(t, checked)
	hook := harness.hook(&scriptHook{
		firstKey: harness.counters.ReservationKey(decisionID),
		before:   func() { workReconcile(t, harness.reconcile) },
	})

	if _, err := harness.reports.Release(t.Context(), httpapi.EnvironmentTest, decisionID); err != nil {
		t.Fatalf("release: %v", err)
	}

	if !hook.done.Load() {
		t.Fatal("reconcile never ran between the release commit and the Redis release")
	}
	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
	harness.clock.Advance(2 * time.Minute)
	workReconcile(t, harness.reconcile)
	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
}

func TestReconcileBetweenExpiryMarkAndRedisReleaseAddsNoDrift(t *testing.T) {
	harness := newLifecycleHarness(t)
	workReconcile(t, harness.reconcile)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	customer := harness.ensureCustomer(t)
	harness.clock.Set(checked.ExpiresAt)
	hook := harness.hook(&scriptHook{
		firstKey: harness.counters.ReservationKey(harness.decisionID(t, checked)),
		before: func() {
			harness.clock.Set(checked.ExpiresAt.Add(-time.Second))
			workReconcile(t, harness.reconcile)
			harness.clock.Set(checked.ExpiresAt)
		},
	})

	workExpiry(t, harness.expiry)

	if !hook.done.Load() {
		t.Fatal("reconcile never ran between the expiry mark and the Redis release")
	}
	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
}

func TestReconcileFreesAReservationWhoseReleaseNeverReachedRedis(t *testing.T) {
	harness := newLifecycleHarness(t)
	workReconcile(t, harness.reconcile)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	customer := harness.ensureCustomer(t)
	harness.clock.Advance(2 * time.Minute)
	workReconcile(t, harness.reconcile)
	harness.clock.Advance(time.Minute)
	decisionID := harness.decisionID(t, checked)
	hook := harness.hook(&scriptHook{firstKey: harness.counters.ReservationKey(decisionID), refuse: true})
	if _, err := harness.reports.Release(t.Context(), httpapi.EnvironmentTest, decisionID); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !hook.done.Load() {
		t.Fatal("the Redis release was never refused")
	}
	if reserved := harness.redisCounter(t, customer.ID, checkPeriod.Start)["reserved"]; reserved == "0" {
		t.Fatal("the reservation was freed although the Redis release was refused")
	}
	harness.clock.Advance(time.Minute)

	workReconcile(t, harness.reconcile)

	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
	if status := harness.reservationStatus(t, checked.DecisionID); status != "released" {
		t.Errorf("reservation status = %s, want released", status)
	}
}

func TestReconcileLeavesALostReservationToExpiry(t *testing.T) {
	harness := newLifecycleHarness(t)
	workReconcile(t, harness.reconcile)
	customer := harness.ensureCustomer(t)
	harness.report(t, serverReport(harness.check(t, videoCheck(veoModel, "8")).DecisionID, "6"))
	hook := harness.hook(&scriptHook{
		firstKey:  harness.counters.CounterKey(httpapi.EnvironmentTest, customer.ID, checkPeriod.Start),
		loseReply: true,
	})

	_, err := harness.service.Check(t.Context(), httpapi.EnvironmentTest, videoCheck(veoModel, "8"))

	assertCheckProblem(t, err, "counters_unavailable")
	if !hook.done.Load() {
		t.Fatal("the reserve reply was never lost")
	}
	for _, step := range []time.Duration{2 * time.Minute, 5 * time.Minute, 5 * time.Minute, 2 * time.Minute} {
		harness.clock.Advance(step)
		workReconcile(t, harness.reconcile)
		if reserved := harness.redisCounter(t, customer.ID, checkPeriod.Start)["reserved"]; strings.HasPrefix(reserved, "-") {
			t.Fatalf("reserved = %s after reconcile, want no negative reservation", reserved)
		}
	}
	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
}

func TestReconcileBetweenReserveAndDecisionInsertAddsNoDrift(t *testing.T) {
	harness := newLifecycleHarness(t)
	customer := harness.ensureCustomer(t)
	harness.report(t, serverReport(harness.check(t, videoCheck(veoModel, "8")).DecisionID, "6"))
	workReconcile(t, harness.reconcile)
	hook := harness.hook(&scriptHook{
		firstKey: harness.counters.CounterKey(httpapi.EnvironmentTest, customer.ID, checkPeriod.Start),
		after:    func() { workReconcile(t, harness.reconcile) },
	})

	checked := harness.check(t, videoCheck(veoModel, "8"))

	if !hook.done.Load() {
		t.Fatal("reconcile never ran between the reserve and the decision insert")
	}
	harness.report(t, serverReport(checked.DecisionID, "6"))
	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
}

func TestReconcileDuringConcurrentTrafficAddsNoDrift(t *testing.T) {
	harness := newLifecycleHarness(t)
	workReconcile(t, harness.reconcile)
	customerIDs := make([]string, concurrentCustomers)
	for index := range customerIDs {
		customerIDs[index] = "concurrent-" + strconv.Itoa(index)
	}
	reconcileContext, stopReconcile := context.WithCancel(t.Context())
	reconciled := make(chan int, 1)
	go func() {
		runs := 0
		for reconcileContext.Err() == nil {
			err := harness.reconcile.Work(reconcileContext, nil)
			if err != nil && reconcileContext.Err() == nil {
				t.Errorf("reconcile during traffic: %v", err)
			}
			runs++
		}
		reconciled <- runs
	}()
	var clients sync.WaitGroup
	for client := range concurrentClients {
		clients.Go(func() {
			for iteration := range concurrentIterations {
				request := videoCheck(veoModel, "8")
				request.CustomerID = customerIDs[(client+iteration)%len(customerIDs)]
				checked, err := harness.service.Check(t.Context(), httpapi.EnvironmentTest, request)
				if err != nil {
					t.Errorf("check: %v", err)
					return
				}
				if (client*concurrentIterations+iteration)%4 == 0 {
					decisionID, err := identifiers.Decode(identifiers.PrefixDecision, checked.DecisionID)
					if err != nil {
						t.Errorf("decode decision id %s: %v", checked.DecisionID, err)
						return
					}
					if _, err := harness.reports.Release(t.Context(), httpapi.EnvironmentTest, decisionID); err != nil {
						t.Errorf("release: %v", err)
					}
					continue
				}
				if _, err := harness.reports.Report(t.Context(), httpapi.EnvironmentTest, serverReport(checked.DecisionID, "6")); err != nil {
					t.Errorf("report: %v", err)
				}
			}
		})
	}
	clients.Wait()
	stopReconcile()
	if runs := <-reconciled; runs < 2 {
		t.Fatalf("reconcile ran %d times during the traffic, want at least 2", runs)
	}

	for _, externalID := range customerIDs {
		customer, err := harness.customers.CustomerByExternalID(t.Context(), httpapi.EnvironmentTest, externalID)
		if err != nil {
			t.Fatalf("find customer %s: %v", externalID, err)
		}
		assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
	}
}

func TestCheckRefusesWhileCountersAreNotReady(t *testing.T) {
	harness := newLifecycleHarness(t)
	customer := harness.ensureCustomer(t)
	harness.check(t, videoCheck(veoModel, "8"))
	counterBefore := harness.redisCounter(t, customer.ID, checkPeriod.Start)
	deleteCountersMarker(t, harness.cache)

	_, err := harness.service.Check(t.Context(), httpapi.EnvironmentTest, videoCheck(veoModel, "8"))

	assertCheckProblem(t, err, "counters_unavailable")
	if diff := cmp.Diff(counterBefore, harness.redisCounter(t, customer.ID, checkPeriod.Start)); diff != "" {
		t.Errorf("counter changed by a refused check (-before +after):\n%s", diff)
	}
	if count := harness.decisionCount(t); count != 1 {
		t.Errorf("decisions = %d, want only the first", count)
	}
	rebuilds, stopRebuilds := context.WithCancel(t.Context())
	defer stopRebuilds()
	go harness.bootstrap.ServeRebuildRequests(rebuilds)
	pollUntil(t, "the rebuild the refused check requested", func(ctx context.Context) bool {
		ready, err := harness.counters.IsReady(ctx)
		return err == nil && ready
	})
	harness.check(t, videoCheck(veoModel, "8"))
	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
}

func TestSettleWhileCountersAreNotReadyIsLeftToTheRebuild(t *testing.T) {
	harness := newLifecycleHarness(t)
	customer := harness.ensureCustomer(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	counterBefore := harness.redisCounter(t, customer.ID, checkPeriod.Start)
	deleteCountersMarker(t, harness.cache)

	harness.report(t, serverReport(checked.DecisionID, "6"))

	if diff := cmp.Diff(counterBefore, harness.redisCounter(t, customer.ID, checkPeriod.Start)); diff != "" {
		t.Errorf("counter changed by a settle while the counters were not ready (-before +after):\n%s", diff)
	}
	if !strings.Contains(harness.logs.String(), `"msg":"decisions.settle_deferred"`) {
		t.Errorf("logs = %s, want decisions.settle_deferred", harness.logs.String())
	}
	if err := harness.bootstrap.EnsureCountersReady(t.Context()); err != nil {
		t.Fatalf("ensure counters ready: %v", err)
	}
	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
}

func TestRebuildBetweenReportCommitAndSettleAddsNoDrift(t *testing.T) {
	harness := newLifecycleHarness(t)
	customer := harness.ensureCustomer(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	hook := harness.hook(&scriptHook{
		firstKey: harness.counters.ReservationKey(harness.decisionID(t, checked)),
		before: func() {
			deleteCountersMarker(t, harness.cache)
			if err := harness.bootstrap.EnsureCountersReady(t.Context()); err != nil {
				t.Errorf("ensure counters ready: %v", err)
			}
		},
	})

	harness.report(t, serverReport(checked.DecisionID, "6"))

	if !hook.done.Load() {
		t.Fatal("the rebuild never ran between the report commit and the settle")
	}
	assertCounterMatchesPostgres(t, harness, customer.ID, checkPeriod.Start)
}

func TestLaterCheckKeepsThePeriodOfTheNewestSubscription(t *testing.T) {
	harness := newReportHarness(t)
	customer := harness.ensureCustomer(t)
	older := signals.Period{Start: checkStart.AddDate(0, 0, -19), End: checkStart.AddDate(0, 0, 12)}
	newer := signals.Period{Start: checkStart.AddDate(0, 0, -5), End: checkStart.AddDate(0, 0, 26)}
	for index, period := range []signals.Period{older, newer} {
		_, err := harness.pool.Exec(t.Context(),
			`INSERT INTO revenue_entries (revenue_entry_id, environment, customer_id, period_start, period_end, kind, amount_nanos,
				source, source_reference, occurred_at, created_at)
			VALUES ($1, 'test', $2, $3, $4, 'subscription', 30000000000, 'api', $5, $3, $3)`,
			uuid.New(), customer.ID, period.Start, period.End, "subscription-"+string(rune('a'+index)))
		if err != nil {
			t.Fatalf("insert subscription revenue: %v", err)
		}
	}
	first := harness.check(t, videoCheck(veoModel, "8"))
	occurredAt := checkStart.AddDate(0, 0, -10)
	request := fallbackReport(identifiers.New().String(), "6")
	request.CustomerID = checkCustomer
	request.OccurredAt = &occurredAt
	reported := harness.report(t, request)
	harness.clock.Advance(time.Second)

	second := harness.check(t, videoCheck(veoModel, "8"))

	if start := harness.ledgerEntry(t, reported).PeriodStart; !start.Equal(older.Start) {
		t.Errorf("fallback report period starts %s, want the older subscription %s", start, older.Start)
	}
	for name, checked := range map[string]decisions.CheckResponse{"first": first, "second": second} {
		if start := harness.decision(t, checked).PeriodStart; !start.Equal(newer.Start) {
			t.Errorf("%s check period starts %s, want the newest subscription %s", name, start, newer.Start)
		}
	}
}

func assertCounterMatchesPostgres(t *testing.T, harness *lifecycleHarness, customerID uuid.UUID, periodStart time.Time) {
	t.Helper()
	if diff := cmp.Diff(withoutZeroFields(harness.postgresCounter(t, customerID, periodStart)), withoutZeroFields(harness.redisCounter(t, customerID, periodStart))); diff != "" {
		t.Errorf("counter of customer %s differs from Postgres (-postgres +redis):\n%s", customerID, diff)
	}
}

func withoutZeroFields(fields map[string]string) map[string]string {
	nonZero := map[string]string{}
	for field, value := range fields {
		if value != "0" {
			nonZero[field] = value
		}
	}
	return nonZero
}
