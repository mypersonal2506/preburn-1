package ledger_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/money"
)

const (
	minimumSamples  = 50
	roundingSamples = 52
)

type usageEstimate struct {
	Environment       string
	Model             string
	Meter             string
	P95QuantityMicros int64
	SampleCount       int32
	RefreshedAt       time.Time
}

var estimatesNow = time.Date(2026, time.September, 26, 10, 0, 0, 0, time.UTC)

func TestUsageEstimatesRefreshNeedsFiftySamples(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	recent := estimatesNow.Add(-24 * time.Hour)
	insertUsageSamples(t, pool, httpapi.EnvironmentTest, customerID, "model-with-49", testMeter, tailedQuantities(minimumSamples-1, 4, units(1), units(9)), recent, nil)
	insertUsageSamples(t, pool, httpapi.EnvironmentTest, customerID, "model-with-50", testMeter, tailedQuantities(minimumSamples, 4, units(1), units(9)), recent, nil)
	insertUsageSamples(t, pool, httpapi.EnvironmentTest, customerID, "model-rounded-up", testMeter, tailedQuantities(roundingSamples, 3, 1, 2), recent, nil)

	workUsageEstimatesRefresh(t, pool)

	want := []usageEstimate{
		{Environment: "test", Model: "model-rounded-up", Meter: testMeter, P95QuantityMicros: 2, SampleCount: roundingSamples, RefreshedAt: estimatesNow},
		{Environment: "test", Model: "model-with-50", Meter: testMeter, P95QuantityMicros: int64(units(9)), SampleCount: minimumSamples, RefreshedAt: estimatesNow},
	}
	if diff := cmp.Diff(want, listUsageEstimates(t, pool)); diff != "" {
		t.Errorf("usage estimates mismatch (-want +got):\n%s", diff)
	}
}

func TestUsageEstimatesRefreshCountsRecentOriginalEntriesPerEnvironmentAndMeter(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	testCustomerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	liveCustomerID := insertCustomer(t, pool, httpapi.EnvironmentLive)
	recent := estimatesNow.Add(-29 * 24 * time.Hour)
	insertUsageSamples(t, pool, httpapi.EnvironmentTest, testCustomerID, testModel, testMeter, tailedQuantities(minimumSamples, 4, units(1), units(9)), recent, nil)
	insertUsageSamples(t, pool, httpapi.EnvironmentTest, testCustomerID, testModel, "input_tokens", tailedQuantities(minimumSamples, 4, units(100), units(400)), recent, nil)
	insertUsageSamples(t, pool, httpapi.EnvironmentTest, testCustomerID, testModel, testMeter, tailedQuantities(minimumSamples, 4, units(90), units(90)), estimatesNow.Add(-31*24*time.Hour), nil)
	corrected := uncostedEntry(testCustomerID, day(time.August, 1), day(time.September, 1))
	corrected.occurredAt = estimatesNow.Add(-40 * 24 * time.Hour)
	correctedID := insertLedgerEntry(t, pool, corrected)
	insertUsageSamples(t, pool, httpapi.EnvironmentTest, testCustomerID, testModel, testMeter, tailedQuantities(minimumSamples, 4, units(70), units(70)), recent, &correctedID)
	insertUsageSamples(t, pool, httpapi.EnvironmentLive, liveCustomerID, testModel, testMeter, tailedQuantities(minimumSamples, 4, units(3), units(5)), recent, nil)

	workUsageEstimatesRefresh(t, pool)

	want := []usageEstimate{
		{Environment: "test", Model: testModel, Meter: "input_tokens", P95QuantityMicros: int64(units(400)), SampleCount: minimumSamples, RefreshedAt: estimatesNow},
		{Environment: "test", Model: testModel, Meter: testMeter, P95QuantityMicros: int64(units(9)), SampleCount: minimumSamples, RefreshedAt: estimatesNow},
		{Environment: "live", Model: testModel, Meter: testMeter, P95QuantityMicros: int64(units(5)), SampleCount: minimumSamples, RefreshedAt: estimatesNow},
	}
	if diff := cmp.Diff(want, listUsageEstimates(t, pool)); diff != "" {
		t.Errorf("usage estimates mismatch (-want +got):\n%s", diff)
	}
}

func TestUsageEstimatesRefreshRemovesEstimatesBelowFiftySamples(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	_, err := pool.Exec(t.Context(),
		`INSERT INTO usage_estimates (environment, feature, provider, model, meter, p95_quantity_micros, sample_count, refreshed_at)
		VALUES ('test', $1, $2, $3, $4, 9000000, 80, $5)`,
		testFeature, testProvider, testModel, testMeter, estimatesNow.Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("insert usage estimate: %v", err)
	}
	insertUsageSamples(t, pool, httpapi.EnvironmentTest, customerID, testModel, testMeter, tailedQuantities(10, 4, units(1), units(9)), estimatesNow.Add(-time.Hour), nil)

	workUsageEstimatesRefresh(t, pool)

	if estimates := listUsageEstimates(t, pool); len(estimates) != 0 {
		t.Errorf("usage estimates = %v, want none", estimates)
	}
}

func tailedQuantities(count, tailCount int, body, tail money.Quantity) []money.Quantity {
	return append(slices.Repeat([]money.Quantity{body}, count-tailCount), slices.Repeat([]money.Quantity{tail}, tailCount)...)
}

func insertUsageSamples(t *testing.T, pool *pgxpool.Pool, environment httpapi.Environment, customerID uuid.UUID, model, meter string, quantities []money.Quantity, occurredAt time.Time, correctionOf *uuid.UUID) {
	t.Helper()
	micros := make([]int64, len(quantities))
	for index, quantity := range quantities {
		micros[index] = int64(quantity)
	}
	_, err := pool.Exec(t.Context(),
		`INSERT INTO ledger_entries (ledger_entry_id, environment, customer_id, idempotency_key, feature, provider, model, attributes,
			usage, cost_nanos, cost_breakdown, cost_status, decision_source, period_start, period_end, correction_of, occurred_at)
		SELECT gen_random_uuid(), $1, $2, gen_random_uuid()::text, $3, $4, $5, '{}', jsonb_build_object($6::text, quantity_micros), 0, '{}',
			'costed', 'server', $7::timestamptz, $7::timestamptz + interval '1 month', $8::uuid, $7::timestamptz
		FROM unnest($9::bigint[]) AS quantity_micros`,
		string(environment), customerID, testFeature, testProvider, model, meter, occurredAt, correctionOf, micros,
	)
	if err != nil {
		t.Fatalf("insert usage samples: %v", err)
	}
}

func workUsageEstimatesRefresh(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	worker := ledger.NewUsageEstimatesRefreshWorker(pool, clock.NewManual(estimatesNow))
	job := &river.Job[ledger.UsageEstimatesRefreshArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: ledger.UsageEstimatesRefreshArgs{}}
	if err := worker.Work(t.Context(), job); err != nil {
		t.Fatalf("Work: %v", err)
	}
}

func listUsageEstimates(t *testing.T, pool *pgxpool.Pool) []usageEstimate {
	t.Helper()
	rows, err := pool.Query(t.Context(),
		`SELECT environment, model, meter, p95_quantity_micros, sample_count, refreshed_at
		FROM usage_estimates
		ORDER BY environment, model, meter`,
	)
	if err != nil {
		t.Fatalf("select usage estimates: %v", err)
	}
	var estimates []usageEstimate
	for rows.Next() {
		var estimate usageEstimate
		if err := rows.Scan(&estimate.Environment, &estimate.Model, &estimate.Meter, &estimate.P95QuantityMicros, &estimate.SampleCount, &estimate.RefreshedAt); err != nil {
			t.Fatalf("scan usage estimate: %v", err)
		}
		estimates = append(estimates, estimate)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read usage estimates: %v", err)
	}
	return estimates
}
