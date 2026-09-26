package ledger_test

import (
	"bufio"
	"bytes"
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
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/jobs/jobstest"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	unpricedProvider   = "acme"
	unpricedVideoModel = "acme-video-1"
	unpricedImageModel = "acme-image-1"
	invalidateChannel  = "invalidate"
	completedEvent     = "ledger.uncosted_rerate_completed"
)

type rerateHarness struct {
	pool      *pgxpool.Pool
	cache     *cache.Client
	clock     *clock.Manual
	pricing   *pricing.Service
	counters  *decisions.Counters
	jobs      *river.Client[pgx.Tx]
	logs      *bytes.Buffer
	settled   []ledger.Entry
	onSettled func()
}

type customerPeriod struct {
	customerID uuid.UUID
	start      time.Time
	end        time.Time
}

type storedCorrection struct {
	CorrectionOf   uuid.UUID
	IdempotencyKey string
	CustomerID     uuid.UUID
	DecisionID     *uuid.UUID
	Provider       string
	Model          string
	CostNanos      int64
	CostStatus     string
	DecisionSource string
	PeriodStart    time.Time
	PeriodEnd      time.Time
	OccurredAt     time.Time
	CreatedAt      time.Time
}

var rerateNow = time.Now().UTC().Truncate(time.Hour)

func TestUncostedRerateWorkerTimesOutAfterAnHour(t *testing.T) {
	t.Parallel()
	worker := ledger.NewUncostedRerateWorker(ledger.RerateDependencies{})

	if timeout := worker.Timeout(&river.Job[pricing.UncostedRerateArgs]{}); timeout != time.Hour {
		t.Errorf("timeout = %s, want 1h", timeout)
	}
}

func TestUncostedRerateCorrectsEachEntryOnceAfterAnOverride(t *testing.T) {
	t.Parallel()
	harness := newRerateHarness(t)
	first := harness.currentCustomer(t, httpapi.EnvironmentTest)
	second := harness.currentCustomer(t, httpapi.EnvironmentTest)
	live := harness.currentCustomer(t, httpapi.EnvironmentLive)
	firstEarly := harness.insertUncosted(t, httpapi.EnvironmentTest, first, unpricedVideoModel, rerateNow.Add(-2*time.Hour))
	firstLate := harness.insertUncosted(t, httpapi.EnvironmentTest, first, unpricedVideoModel, rerateNow.Add(-time.Hour))
	secondEntry := harness.insertUncosted(t, httpapi.EnvironmentTest, second, unpricedVideoModel, rerateNow.Add(-pricing.RerateWindow))
	harness.insertUncosted(t, httpapi.EnvironmentTest, second, unpricedVideoModel, rerateNow.Add(-pricing.RerateWindow-time.Second))
	harness.insertUncosted(t, httpapi.EnvironmentTest, second, unpricedImageModel, rerateNow.Add(-time.Hour))
	harness.insertUncosted(t, httpapi.EnvironmentLive, live, unpricedVideoModel, rerateNow.Add(-time.Hour))
	harness.createOverride(t, unpricedVideoModel)
	subscription := harness.subscribeInvalidations(t)

	harness.work(t)

	want := []storedCorrection{
		harness.wantCorrection(first, firstEarly, rerateNow.Add(-2*time.Hour)),
		harness.wantCorrection(first, firstLate, rerateNow.Add(-time.Hour)),
		harness.wantCorrection(second, secondEntry, rerateNow.Add(-pricing.RerateWindow)),
	}
	if diff := cmp.Diff(want, harness.corrections(t), sortCorrections()); diff != "" {
		t.Errorf("corrections mismatch (-want +got):\n%s", diff)
	}
	wantRollups := map[uuid.UUID][]rollup{
		first.customerID:  {{PeriodStart: first.start, PeriodEnd: first.end, Cost: dollars(8)}},
		second.customerID: {{PeriodStart: second.start, PeriodEnd: second.end, Cost: dollars(4), UncostedCount: 2}},
	}
	for customerID, rollups := range wantRollups {
		if diff := cmp.Diff(rollups, listRollups(t, harness.pool, customerID)); diff != "" {
			t.Errorf("rollups of customer %s mismatch (-want +got):\n%s", customerID, diff)
		}
	}
	wantInvalidations := []cache.Invalidation{customerInvalidation(first), customerInvalidation(second)}
	sortInvalidations := cmpopts.SortSlices(func(left, right cache.Invalidation) bool { return left.ID < right.ID })
	if diff := cmp.Diff(wantInvalidations, receiveInvalidations(t, subscription, len(wantInvalidations)), sortInvalidations); diff != "" {
		t.Errorf("invalidations mismatch (-want +got):\n%s", diff)
	}

	harness.work(t)

	if diff := cmp.Diff(want, harness.corrections(t), sortCorrections()); diff != "" {
		t.Errorf("corrections after the second run mismatch (-want +got):\n%s", diff)
	}
	wantLogs := []map[string]any{
		{"msg": completedEvent, "environment": "test", "corrections": float64(3)},
		{"msg": completedEvent, "environment": "test", "corrections": float64(0)},
	}
	if diff := cmp.Diff(wantLogs, harness.completedLogs(t)); diff != "" {
		t.Errorf("completed log lines mismatch (-want +got):\n%s", diff)
	}
}

func TestUncostedRerateAddsCorrectedCostToCurrentPeriodCounters(t *testing.T) {
	t.Parallel()
	harness := newRerateHarness(t)
	current := harness.currentCustomer(t, httpapi.EnvironmentTest)
	ended := harness.customer(t, httpapi.EnvironmentTest, rerateNow.AddDate(0, -1, -1), rerateNow.AddDate(0, 0, -1))
	harness.insertUncosted(t, httpapi.EnvironmentTest, current, unpricedVideoModel, rerateNow.Add(-time.Hour))
	harness.insertUncosted(t, httpapi.EnvironmentTest, ended, unpricedVideoModel, ended.end.Add(-time.Hour))
	harness.settleUnreserved(t, current, dollars(1), 2)
	harness.createOverride(t, unpricedVideoModel)

	harness.work(t)

	wantCounter := map[string]string{
		"settled": "5000000000", "settled:" + testFeature: "5000000000",
		"count": "2", "count:" + testFeature: "2",
	}
	if diff := cmp.Diff(wantCounter, harness.counterFields(t, current)); diff != "" {
		t.Errorf("current counter mismatch (-want +got):\n%s", diff)
	}
	if fields := harness.counterFields(t, ended); len(fields) != 0 {
		t.Errorf("ended period counter = %v, want no counter", fields)
	}
	if corrections := len(harness.corrections(t)); corrections != 2 {
		t.Errorf("corrections = %d, want 2", corrections)
	}
}

func TestUncostedRerateRunsAgainWhenOverridesChangeDuringTheRun(t *testing.T) {
	t.Parallel()
	harness := newRerateHarness(t)
	period := harness.currentCustomer(t, httpapi.EnvironmentTest)
	video := harness.insertUncosted(t, httpapi.EnvironmentTest, period, unpricedVideoModel, rerateNow.Add(-2*time.Hour))
	image := harness.insertUncosted(t, httpapi.EnvironmentTest, period, unpricedImageModel, rerateNow.Add(-time.Hour))
	harness.createOverride(t, unpricedVideoModel)
	harness.onSettled = func() {
		if len(harness.settled) == 1 {
			harness.createOverride(t, unpricedImageModel)
		}
	}

	job := harness.work(t)

	want := []storedCorrection{
		harness.wantCorrection(period, video, rerateNow.Add(-2*time.Hour)),
		withModel(harness.wantCorrection(period, image, rerateNow.Add(-time.Hour)), unpricedImageModel),
	}
	if diff := cmp.Diff(want, harness.corrections(t), sortCorrections()); diff != "" {
		t.Errorf("corrections mismatch (-want +got):\n%s", diff)
	}
	if state := jobState(t, harness.pool, job.ID); state != rivertype.JobStateCompleted {
		t.Errorf("job state = %s, want completed", state)
	}
}

func newRerateHarness(t *testing.T) *rerateHarness {
	t.Helper()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(rerateNow)
	jobs := jobstest.NewInsertClient(t, pool)
	harness := &rerateHarness{
		pool:     pool,
		cache:    cacheClient,
		clock:    manualClock,
		pricing:  pricing.NewService(pool, cacheClient, jobs, catalogfiles.Catalog{}, manualClock),
		counters: decisions.NewCounters(cacheClient),
		jobs:     jobs,
		logs:     &bytes.Buffer{},
	}
	if err := harness.counters.SetReady(t.Context(), rerateNow); err != nil {
		t.Fatalf("set counters ready: %v", err)
	}
	return harness
}

func (harness *rerateHarness) worker() *ledger.UncostedRerateWorker {
	return ledger.NewUncostedRerateWorker(ledger.RerateDependencies{
		Pool:    harness.pool,
		Pricing: harness.pricing,
		Cache:   harness.cache,
		BeginCorrection: func(ctx context.Context, correction ledger.Entry) error {
			return harness.counters.BeginChange(ctx, correctionSettlement(correction), correction.CreatedAt)
		},
		SettleCorrection: func(ctx context.Context, correction ledger.Entry) error {
			harness.settled = append(harness.settled, correction)
			if harness.onSettled != nil {
				harness.onSettled()
			}
			return harness.counters.SettleUnreserved(ctx, correctionSettlement(correction))
		},
		Clock:  harness.clock,
		Logger: logging.New(harness.logs, slog.LevelDebug),
	})
}

func (harness *rerateHarness) work(t *testing.T) *river.Job[pricing.UncostedRerateArgs] {
	t.Helper()
	arguments := pricing.UncostedRerateArgs{Environment: httpapi.EnvironmentTest}
	inserted, err := harness.jobs.Insert(t.Context(), arguments, nil)
	if err != nil {
		t.Fatalf("insert uncosted rerate job: %v", err)
	}
	setJobState(t, harness.pool, inserted.Job.ID, rivertype.JobStateRunning)
	row := *inserted.Job
	row.State = rivertype.JobStateRunning
	row.Attempt = 1
	job := &river.Job[pricing.UncostedRerateArgs]{JobRow: &row, Args: arguments}
	if err := harness.worker().Work(rivertest.WorkContext(t.Context(), harness.jobs), job); err != nil {
		t.Fatalf("Work: %v", err)
	}
	return job
}

func (harness *rerateHarness) currentCustomer(t *testing.T, environment httpapi.Environment) customerPeriod {
	t.Helper()
	return harness.customer(t, environment, rerateNow.AddDate(0, 0, -10), rerateNow.AddDate(0, 0, 20))
}

func (harness *rerateHarness) customer(t *testing.T, environment httpapi.Environment, periodStart, periodEnd time.Time) customerPeriod {
	t.Helper()
	return customerPeriod{customerID: insertCustomer(t, harness.pool, environment), start: periodStart, end: periodEnd}
}

func (harness *rerateHarness) insertUncosted(t *testing.T, environment httpapi.Environment, period customerPeriod, model string, occurredAt time.Time) uuid.UUID {
	t.Helper()
	ledgerEntryID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO ledger_entries (ledger_entry_id, environment, customer_id, idempotency_key, feature, provider, model, attributes,
			usage, cost_nanos, cost_breakdown, cost_status, decision_source, period_start, period_end, occurred_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, '{}', $8, NULL, '{"lines": []}', 'uncosted', 'fallback', $9, $10, $11, $11)`,
		ledgerEntryID, string(environment), period.customerID, ledgerEntryID.String(), testFeature, unpricedProvider, model,
		`{"output_seconds": 8000000}`, period.start, period.end, occurredAt,
	)
	if err != nil {
		t.Fatalf("insert uncosted ledger entry: %v", err)
	}
	return ledgerEntryID
}

func (harness *rerateHarness) createOverride(t *testing.T, model string) {
	t.Helper()
	_, err := harness.pricing.CreateOverride(t.Context(), httpapi.EnvironmentTest, pricing.CreateOverrideInput{
		Provider:     unpricedProvider,
		Model:        model,
		Meter:        testMeter,
		UnitPrice:    "0.50",
		UnitQuantity: 1,
	})
	if err != nil {
		t.Fatalf("create override of %s: %v", model, err)
	}
}

func (harness *rerateHarness) settleUnreserved(t *testing.T, period customerPeriod, amount money.Amount, count int64) {
	t.Helper()
	settlement := decisions.Settlement{
		Environment:    httpapi.EnvironmentTest,
		CustomerID:     period.customerID,
		PeriodStart:    period.start,
		PeriodEnd:      period.end,
		ChangeID:       identifiers.New(),
		Feature:        testFeature,
		Amount:         amount,
		CountIncrement: count,
	}
	if err := harness.counters.BeginChange(t.Context(), settlement, harness.clock.Now()); err != nil {
		t.Fatalf("begin counter change: %v", err)
	}
	if err := harness.counters.SettleUnreserved(t.Context(), settlement); err != nil {
		t.Fatalf("settle unreserved: %v", err)
	}
}

func correctionSettlement(correction ledger.Entry) decisions.Settlement {
	return decisions.Settlement{
		Environment: correction.Environment,
		CustomerID:  correction.CustomerID,
		PeriodStart: correction.PeriodStart,
		PeriodEnd:   correction.PeriodEnd,
		ChangeID:    correction.ID,
		Feature:     correction.Feature,
		Amount:      *correction.Rating.Cost,
	}
}

func (harness *rerateHarness) counterFields(t *testing.T, period customerPeriod) map[string]string {
	t.Helper()
	fields, err := harness.cache.Redis().HGetAll(t.Context(), harness.counters.CounterKey(httpapi.EnvironmentTest, period.customerID, period.start)).Result()
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return fields
}

func (harness *rerateHarness) wantCorrection(period customerPeriod, originalID uuid.UUID, occurredAt time.Time) storedCorrection {
	return storedCorrection{
		CorrectionOf:   originalID,
		IdempotencyKey: "correction:" + originalID.String(),
		CustomerID:     period.customerID,
		Provider:       unpricedProvider,
		Model:          unpricedVideoModel,
		CostNanos:      int64(dollars(4)),
		CostStatus:     "costed",
		DecisionSource: "fallback",
		PeriodStart:    period.start,
		PeriodEnd:      period.end,
		OccurredAt:     occurredAt,
		CreatedAt:      rerateNow,
	}
}

func (harness *rerateHarness) corrections(t *testing.T) []storedCorrection {
	t.Helper()
	rows, err := harness.pool.Query(t.Context(),
		`SELECT correction_of, idempotency_key, customer_id, decision_id, provider, model, cost_nanos, cost_status, decision_source,
			period_start, period_end, occurred_at, created_at
		FROM ledger_entries
		WHERE correction_of IS NOT NULL`)
	if err != nil {
		t.Fatalf("select corrections: %v", err)
	}
	corrections, err := pgx.CollectRows(rows, pgx.RowToStructByPos[storedCorrection])
	if err != nil {
		t.Fatalf("read corrections: %v", err)
	}
	for index := range corrections {
		corrections[index].PeriodStart = corrections[index].PeriodStart.UTC()
		corrections[index].PeriodEnd = corrections[index].PeriodEnd.UTC()
		corrections[index].OccurredAt = corrections[index].OccurredAt.UTC()
		corrections[index].CreatedAt = corrections[index].CreatedAt.UTC()
	}
	return corrections
}

func (harness *rerateHarness) subscribeInvalidations(t *testing.T) *redis.PubSub {
	t.Helper()
	subscription := harness.cache.Redis().Subscribe(t.Context(), harness.cache.Key(invalidateChannel))
	t.Cleanup(func() {
		if err := subscription.Close(); err != nil {
			t.Errorf("close subscription: %v", err)
		}
	})
	if _, err := subscription.ReceiveTimeout(t.Context(), waitTimeout); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	return subscription
}

func (harness *rerateHarness) completedLogs(t *testing.T) []map[string]any {
	t.Helper()
	var completed []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(harness.logs.Bytes()))
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("decode log line %q: %v", scanner.Text(), err)
		}
		if line["msg"] != completedEvent {
			continue
		}
		delete(line, "time")
		delete(line, "level")
		completed = append(completed, line)
	}
	return completed
}

func receiveInvalidations(t *testing.T, subscription *redis.PubSub, count int) []cache.Invalidation {
	t.Helper()
	invalidations := make([]cache.Invalidation, 0, count)
	for range count {
		received, err := subscription.ReceiveTimeout(t.Context(), waitTimeout)
		if err != nil {
			t.Fatalf("receive invalidation: %v", err)
		}
		message, isMessage := received.(*redis.Message)
		if !isMessage {
			t.Fatalf("received %T, want *redis.Message", received)
		}
		var invalidation cache.Invalidation
		if err := json.Unmarshal([]byte(message.Payload), &invalidation); err != nil {
			t.Fatalf("decode invalidation %q: %v", message.Payload, err)
		}
		invalidations = append(invalidations, invalidation)
	}
	return invalidations
}

func customerInvalidation(period customerPeriod) cache.Invalidation {
	return cache.Invalidation{
		Kind:        cache.InvalidationKindCustomer,
		Environment: string(httpapi.EnvironmentTest),
		ID:          identifiers.Encode(identifiers.PrefixCustomer, period.customerID),
	}
}

func withModel(correction storedCorrection, model string) storedCorrection {
	correction.Model = model
	return correction
}

func sortCorrections() cmp.Option {
	return cmpopts.SortSlices(func(left, right storedCorrection) bool {
		return left.CorrectionOf.String() < right.CorrectionOf.String()
	})
}
