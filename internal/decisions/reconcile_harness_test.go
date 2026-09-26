package decisions_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/jobs/jobstest"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
)

const (
	videoFeature         = "video"
	decisionHoldTime     = 10 * time.Minute
	fixtureProvider      = "openai"
	fixtureModel         = "gpt-6-sol"
	expiredMetricName    = "preburn_reservations_expired_total"
	repairsMetricName    = "preburn_counter_repairs_total"
	rebuildMetricName    = "preburn_counters_rebuild_duration_seconds"
	reservationsExpiring = "reservations_expiring"
	countersReadyKeyName = "counters_ready"
	pollTimeout          = 10 * time.Second
	pollInterval         = 10 * time.Millisecond
)

type counterJobsHarness struct {
	pool     *pgxpool.Pool
	cache    *cache.Client
	jobs     *river.Client[pgx.Tx]
	counters *decisions.Counters
	clock    *clock.Manual
}

type customerPeriod struct {
	environment httpapi.Environment
	customerID  uuid.UUID
	start       time.Time
	end         time.Time
}

type checkedDecision struct {
	period    customerPeriod
	id        uuid.UUID
	feature   string
	amount    money.Amount
	expiresAt time.Time
}

type keyState struct {
	Fields   map[string]string
	ExpireAt time.Duration
}

// Redis expires counters and reservation hashes at absolute times derived
// from these moments, so they follow the wall clock rather than a fixed date.
var (
	counterJobsStart   = time.Now().UTC().Truncate(time.Hour)
	currentPeriodStart = counterJobsStart.AddDate(0, 0, -25)
	currentPeriodEnd   = counterJobsStart.AddDate(0, 0, 5)
)

func newCounterJobsHarness(t *testing.T) *counterJobsHarness {
	t.Helper()
	cacheClient := cachetest.NewClient(t)
	pool := databasetest.NewPool(t)
	harness := &counterJobsHarness{
		pool:     pool,
		cache:    cacheClient,
		jobs:     jobstest.NewInsertClient(t, pool),
		counters: decisions.NewCounters(cacheClient),
		clock:    clock.NewManual(counterJobsStart),
	}
	if err := harness.counters.SetReady(t.Context(), counterJobsStart); err != nil {
		t.Fatalf("set counters ready: %v", err)
	}
	return harness
}

func (harness *counterJobsHarness) newExpiryWorker(t *testing.T, registry prometheus.Registerer) *decisions.ExpiryWorker {
	t.Helper()
	worker, err := decisions.NewExpiryWorker(harness.pool, harness.counters, harness.jobs, harness.clock, registry)
	if err != nil {
		t.Fatalf("new expiry worker: %v", err)
	}
	return worker
}

func (harness *counterJobsHarness) newReconcileWorker(t *testing.T, registry prometheus.Registerer) *decisions.ReconcileWorker {
	t.Helper()
	worker, err := decisions.NewReconcileWorker(harness.pool, harness.counters, harness.newExpiryWorker(t, registry), harness.clock, registry)
	if err != nil {
		t.Fatalf("new reconcile worker: %v", err)
	}
	return worker
}

func (harness *counterJobsHarness) newBootstrap(t *testing.T, registry prometheus.Registerer) *decisions.CounterBootstrap {
	t.Helper()
	bootstrap, err := decisions.NewCounterBootstrap(harness.pool, harness.counters, harness.jobs, harness.clock, logging.New(t.Output(), slog.LevelDebug), registry)
	if err != nil {
		t.Fatalf("new counter bootstrap: %v", err)
	}
	return bootstrap
}

func workExpiry(t *testing.T, worker *decisions.ExpiryWorker) {
	t.Helper()
	if err := worker.Work(t.Context(), &river.Job[decisions.ExpiryArgs]{JobRow: &rivertype.JobRow{ID: 1}}); err != nil {
		t.Fatalf("expiry work: %v", err)
	}
}

func workReconcile(t *testing.T, worker *decisions.ReconcileWorker) {
	t.Helper()
	if err := worker.Work(t.Context(), &river.Job[decisions.ReconcileArgs]{JobRow: &rivertype.JobRow{ID: 1}}); err != nil {
		t.Fatalf("reconcile work: %v", err)
	}
}

func (harness *counterJobsHarness) currentCustomer(t *testing.T, environment httpapi.Environment) customerPeriod {
	t.Helper()
	return harness.customer(t, environment, currentPeriodStart, currentPeriodEnd)
}

func (harness *counterJobsHarness) customer(t *testing.T, environment httpapi.Environment, periodStart, periodEnd time.Time) customerPeriod {
	t.Helper()
	customerID := uuid.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO customers (customer_id, environment, external_id, status) VALUES ($1, $2, $3, 'active')",
		customerID, string(environment), "customer-"+customerID.String(),
	)
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	return customerPeriod{environment: environment, customerID: customerID, start: periodStart, end: periodEnd}
}

func (harness *counterJobsHarness) reserve(t *testing.T, period customerPeriod, feature string, amount money.Amount, expiresAt time.Time) checkedDecision {
	t.Helper()
	decision := checkedDecision{period: period, id: uuid.New(), feature: feature, amount: amount, expiresAt: expiresAt}
	request := harness.reserveRequest(decision)
	result, err := harness.counters.Reserve(t.Context(), request)
	if err != nil || result != decisions.ReserveResultReserved {
		t.Fatalf("reserve %s %d = %q err=%v, want reserved", feature, amount, result, err)
	}
	harness.insertDecision(t, decision, "allow", "no_policy_matched", "reserved")
	harness.endChange(t, request)
	return decision
}

func (harness *counterJobsHarness) countOnly(t *testing.T, period customerPeriod, feature string) {
	t.Helper()
	decision := checkedDecision{period: period, id: uuid.New(), feature: feature, expiresAt: harness.clock.Now().Add(decisionHoldTime)}
	request := harness.reserveRequest(decision)
	result, err := harness.counters.CountOnly(t.Context(), request)
	if err != nil || result != decisions.ReserveResultCounted {
		t.Fatalf("count only %s = %q err=%v, want counted", feature, result, err)
	}
	harness.insertDecision(t, decision, "deny", "policy_matched", "unreserved")
	harness.endChange(t, request)
}

func (harness *counterJobsHarness) reserveRequest(decision checkedDecision) decisions.ReserveRequest {
	return decisions.ReserveRequest{
		Environment: decision.period.environment,
		CustomerID:  decision.period.customerID,
		PeriodStart: decision.period.start,
		PeriodEnd:   decision.period.end,
		DecisionID:  decision.id,
		Feature:     decision.feature,
		Amount:      decision.amount,
		ExpiresAt:   decision.expiresAt,
		DecidedAt:   harness.clock.Now(),
	}
}

func (harness *counterJobsHarness) endChange(t *testing.T, request decisions.ReserveRequest) {
	t.Helper()
	if err := harness.counters.EndChange(t.Context(), request); err != nil {
		t.Fatalf("end counter change: %v", err)
	}
}

func (harness *counterJobsHarness) limitDenied(t *testing.T, period customerPeriod, feature string) {
	t.Helper()
	decision := checkedDecision{period: period, id: uuid.New(), feature: feature, expiresAt: harness.clock.Now().Add(decisionHoldTime)}
	harness.insertDecision(t, decision, "deny", "hard_limit_reached", "unreserved")
}

func (harness *counterJobsHarness) insertDecision(t *testing.T, decision checkedDecision, outcome, reason, status string) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO decisions (decision_id, environment, customer_id, feature, requested_provider, requested_model,
			provider, model, attributes, overrides, outcome, reason, signals, reserved_nanos, estimate_basis, status,
			period_start, period_end, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $5, $6, '{}', '{}', $7, $8, '{}', $9, 'request_estimate', $10, $11, $12, $13, $14)`,
		decision.id, string(decision.period.environment), decision.period.customerID, decision.feature, fixtureProvider, fixtureModel,
		outcome, reason, int64(decision.amount), status, decision.period.start, decision.period.end, decision.expiresAt, harness.clock.Now(),
	)
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
}

func (harness *counterJobsHarness) settle(t *testing.T, decision checkedDecision, cost money.Amount) {
	t.Helper()
	settlement := harness.recordSettlement(t, decision, cost)
	if err := harness.counters.Settle(t.Context(), decision.id, settlement); err != nil {
		t.Fatalf("settle: %v", err)
	}
}

func (harness *counterJobsHarness) recordSettlement(t *testing.T, decision checkedDecision, cost money.Amount) decisions.Settlement {
	t.Helper()
	ledgerEntryID := harness.insertLedgerEntry(t, decision.period, decision.feature, &decision.id, nil, "decision:"+decision.id.String(), &cost)
	_, err := harness.pool.Exec(t.Context(),
		"UPDATE decisions SET status = 'settled', settled_at = $2 WHERE decision_id = $1",
		decision.id, harness.clock.Now(),
	)
	if err != nil {
		t.Fatalf("mark decision settled: %v", err)
	}
	return harness.beginChange(t, decision.period, decision.feature, ledgerEntryID, &cost, 0)
}

func (harness *counterJobsHarness) release(t *testing.T, decision checkedDecision, status decisions.ReservationStatus) {
	t.Helper()
	result, err := harness.counters.Release(t.Context(), decision.id, status)
	if err != nil || !result.Applied {
		t.Fatalf("release = %+v err=%v, want applied", result, err)
	}
	if _, err := harness.pool.Exec(t.Context(), "UPDATE decisions SET status = $2 WHERE decision_id = $1", decision.id, string(status)); err != nil {
		t.Fatalf("mark decision %s: %v", status, err)
	}
}

func (harness *counterJobsHarness) fallback(t *testing.T, period customerPeriod, feature string, cost *money.Amount) uuid.UUID {
	t.Helper()
	ledgerEntryID := harness.insertLedgerEntry(t, period, feature, nil, nil, uuid.NewString(), cost)
	harness.settleUnreserved(t, harness.beginChange(t, period, feature, ledgerEntryID, cost, 1))
	return ledgerEntryID
}

func (harness *counterJobsHarness) correction(t *testing.T, period customerPeriod, feature string, originalID uuid.UUID, cost money.Amount) {
	t.Helper()
	ledgerEntryID := harness.insertLedgerEntry(t, period, feature, nil, &originalID, "correction:"+originalID.String(), &cost)
	harness.settleUnreserved(t, harness.beginChange(t, period, feature, ledgerEntryID, &cost, 0))
}

func (harness *counterJobsHarness) beginChange(t *testing.T, period customerPeriod, feature string, ledgerEntryID uuid.UUID, cost *money.Amount, countIncrement int64) decisions.Settlement {
	t.Helper()
	settlement := decisions.Settlement{
		Environment:    period.environment,
		CustomerID:     period.customerID,
		PeriodStart:    period.start,
		PeriodEnd:      period.end,
		ChangeID:       ledgerEntryID,
		Feature:        feature,
		CountIncrement: countIncrement,
	}
	if cost != nil {
		settlement.Amount = *cost
	}
	if err := harness.counters.BeginChange(t.Context(), settlement, harness.clock.Now()); err != nil {
		t.Fatalf("begin counter change: %v", err)
	}
	return settlement
}

func (harness *counterJobsHarness) settleUnreserved(t *testing.T, settlement decisions.Settlement) {
	t.Helper()
	if err := harness.counters.SettleUnreserved(t.Context(), settlement); err != nil {
		t.Fatalf("settle unreserved: %v", err)
	}
}

func (harness *counterJobsHarness) insertLedgerEntry(t *testing.T, period customerPeriod, feature string, decisionID, correctionOf *uuid.UUID, idempotencyKey string, cost *money.Amount) uuid.UUID {
	t.Helper()
	ledgerEntryID := uuid.New()
	costStatus := "uncosted"
	decisionSource := "fallback"
	if cost != nil {
		costStatus = "costed"
	}
	if decisionID != nil {
		decisionSource = "server"
	}
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO ledger_entries (ledger_entry_id, environment, customer_id, decision_id, idempotency_key, feature,
			provider, model, attributes, usage, cost_nanos, cost_breakdown, cost_status, decision_source,
			period_start, period_end, correction_of, occurred_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{}', '{}', $9, '{}', $10, $11, $12, $13, $14, $15, $15)`,
		ledgerEntryID, string(period.environment), period.customerID, decisionID, idempotencyKey, feature,
		fixtureProvider, fixtureModel, cost, costStatus, decisionSource, period.start, period.end, correctionOf, harness.clock.Now(),
	)
	if err != nil {
		t.Fatalf("insert ledger entry: %v", err)
	}
	return ledgerEntryID
}

func (harness *counterJobsHarness) counterKey(period customerPeriod) string {
	return harness.counters.CounterKey(period.environment, period.customerID, period.start)
}

func (harness *counterJobsHarness) hash(t *testing.T, key string) map[string]string {
	t.Helper()
	fields, err := harness.cache.Redis().HGetAll(t.Context(), key).Result()
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return fields
}

func (harness *counterJobsHarness) counterFields(t *testing.T, period customerPeriod) map[string]string {
	t.Helper()
	return harness.hash(t, harness.counterKey(period))
}

func (harness *counterJobsHarness) writeCounterFields(t *testing.T, period customerPeriod, fields map[string]string) {
	t.Helper()
	if err := harness.cache.Redis().HSet(t.Context(), harness.counterKey(period), fields).Err(); err != nil {
		t.Fatalf("write counter fields: %v", err)
	}
}

func (harness *counterJobsHarness) expireTime(t *testing.T, key string) time.Duration {
	t.Helper()
	expireAt, err := harness.cache.Redis().ExpireTime(t.Context(), key).Result()
	if err != nil {
		t.Fatalf("read expiry of %s: %v", key, err)
	}
	return expireAt
}

func (harness *counterJobsHarness) expiringMembers(t *testing.T) []redis.Z {
	t.Helper()
	members, err := harness.cache.Redis().ZRangeWithScores(t.Context(), harness.cache.Key(reservationsExpiring), 0, -1).Result()
	if err != nil {
		t.Fatalf("read reservations_expiring: %v", err)
	}
	return members
}

func (harness *counterJobsHarness) reservationStatus(t *testing.T, decisionID uuid.UUID) string {
	t.Helper()
	status, err := harness.cache.Redis().HGet(t.Context(), harness.counters.ReservationKey(decisionID), "status").Result()
	if err != nil {
		t.Fatalf("read reservation status: %v", err)
	}
	return status
}

func (harness *counterJobsHarness) decisionStatus(t *testing.T, decisionID uuid.UUID) string {
	t.Helper()
	var status string
	if err := harness.pool.QueryRow(t.Context(), "SELECT status FROM decisions WHERE decision_id = $1", decisionID).Scan(&status); err != nil {
		t.Fatalf("read decision status: %v", err)
	}
	return status
}

func (harness *counterJobsHarness) decisionStatusCounts(t *testing.T) map[string]int {
	t.Helper()
	rows, err := harness.pool.Query(t.Context(), "SELECT status, count(*) FROM decisions GROUP BY status")
	if err != nil {
		t.Fatalf("count decision statuses: %v", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			t.Fatalf("scan decision status count: %v", err)
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read decision status counts: %v", err)
	}
	return counts
}

func (harness *counterJobsHarness) reservationKeys(checked []checkedDecision) []string {
	keys := make([]string, 0, len(checked))
	for _, decision := range checked {
		keys = append(keys, harness.counters.ReservationKey(decision.id))
	}
	return keys
}

func (harness *counterJobsHarness) keyStates(t *testing.T, keys []string) map[string]keyState {
	t.Helper()
	states := make(map[string]keyState, len(keys))
	for _, key := range keys {
		states[key] = keyState{Fields: harness.hash(t, key), ExpireAt: harness.expireTime(t, key)}
	}
	return states
}

func (harness *counterJobsHarness) rollupRefreshJobs(t *testing.T) []ledger.RollupRefreshArgs {
	t.Helper()
	rows, err := harness.pool.Query(t.Context(), "SELECT args FROM river_job WHERE kind = 'rollup_refresh' ORDER BY id")
	if err != nil {
		t.Fatalf("select rollup refresh jobs: %v", err)
	}
	defer rows.Close()
	var refreshes []ledger.RollupRefreshArgs
	for rows.Next() {
		var encoded []byte
		if err := rows.Scan(&encoded); err != nil {
			t.Fatalf("scan rollup refresh job: %v", err)
		}
		var refresh ledger.RollupRefreshArgs
		if err := json.Unmarshal(encoded, &refresh); err != nil {
			t.Fatalf("decode rollup refresh job %s: %v", encoded, err)
		}
		refreshes = append(refreshes, refresh)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read rollup refresh jobs: %v", err)
	}
	return refreshes
}

func deleteCountersMarker(t *testing.T, cacheClient *cache.Client) {
	t.Helper()
	if err := cacheClient.Redis().Del(t.Context(), cacheClient.Key(countersReadyKeyName)).Err(); err != nil {
		t.Fatalf("delete counters_ready: %v", err)
	}
}

func ignoreZeroFields() cmp.Option {
	return cmpopts.IgnoreMapEntries(func(_ string, value string) bool {
		return value == "0"
	})
}

func counterMetric(t *testing.T, registry prometheus.Gatherer, name string) float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return family.GetMetric()[0].GetCounter().GetValue()
		}
	}
	t.Fatalf("metric %s is not registered", name)
	return 0
}

func histogramSampleCount(t *testing.T, registry prometheus.Gatherer, name string) uint64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return family.GetMetric()[0].GetHistogram().GetSampleCount()
		}
	}
	t.Fatalf("metric %s is not registered", name)
	return 0
}

func pollUntil(t *testing.T, description string, condition func(ctx context.Context) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), pollTimeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for !condition(ctx) {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", description)
		case <-ticker.C:
		}
	}
}
