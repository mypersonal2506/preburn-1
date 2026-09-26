package customerstate_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/signals"
)

const nanosPerDollar = 1_000_000_000

type harness struct {
	pool   *pgxpool.Pool
	clock  *clock.Manual
	loader *customerstate.Loader
}

type queryHook struct {
	queryName string
	run       func(ctx context.Context)
	armed     atomic.Bool
}

type hookedQueryKey struct{}

var (
	testStart        = time.Date(2026, time.September, 26, 10, 0, 0, 0, time.UTC)
	august           = signals.Period{Start: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)}
	september        = signals.Period{Start: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)}
	october          = signals.Period{Start: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)}
	marginTarget     = money.BasisPoints(4000)
	fixedAllowance   = pointer(dollars(5))
	withoutAllowance *money.Amount
	withoutPlan      *uuid.UUID
	withoutHoldTimes = map[string]int{}
)

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := databasetest.NewPool(t)
	return &harness{
		pool:   pool,
		clock:  clock.NewManual(testStart),
		loader: customerstate.NewLoader(pool),
	}
}

func (harness *harness) newHookedLoader(t *testing.T, hook *queryHook) *customerstate.Loader {
	t.Helper()
	configuration := harness.pool.Config().Copy()
	configuration.ConnConfig.Tracer = hook
	hookedPool, err := pgxpool.NewWithConfig(t.Context(), configuration)
	if err != nil {
		t.Fatalf("open hooked pool: %v", err)
	}
	t.Cleanup(hookedPool.Close)
	return customerstate.NewLoader(hookedPool)
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

func (harness *harness) createPlan(t *testing.T, environment httpapi.Environment, targetMargin money.BasisPoints, allowance *money.Amount, holdTimes map[string]int) uuid.UUID {
	t.Helper()
	encodedHoldTimes, err := json.Marshal(holdTimes)
	if err != nil {
		t.Fatalf("encode hold times: %v", err)
	}
	planID := identifiers.New()
	_, err = harness.pool.Exec(t.Context(),
		"INSERT INTO plans (plan_id, environment, name, target_margin_basis_points, allowance_nanos, hold_times, status) VALUES ($1, $2, $3, $4, $5, $6, 'active')",
		planID, environment, "plan "+planID.String(), targetMargin, allowance, encodedHoldTimes)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	return planID
}

func (harness *harness) setDefaultPlan(t *testing.T, environment httpapi.Environment, planID uuid.UUID) {
	t.Helper()
	if _, err := harness.pool.Exec(t.Context(), "UPDATE environment_settings SET default_plan_id = $2 WHERE environment = $1", environment, planID); err != nil {
		t.Fatalf("set default plan: %v", err)
	}
}

func (harness *harness) createCustomer(t *testing.T, environment httpapi.Environment, planID *uuid.UUID, status string) uuid.UUID {
	t.Helper()
	customerID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO customers (customer_id, environment, external_id, plan_id, status) VALUES ($1, $2, $3, $4, $5)",
		customerID, environment, "customer-"+customerID.String(), planID, status)
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	return customerID
}

func (harness *harness) insertRevenue(t *testing.T, environment httpapi.Environment, customerID uuid.UUID, kind string, amount money.Amount, period signals.Period) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO revenue_entries (revenue_entry_id, environment, customer_id, period_start, period_end, kind, amount_nanos, source, source_reference, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'api', $8, $4)`,
		identifiers.New(), environment, customerID, period.Start, period.End, kind, amount, identifiers.New().String())
	if err != nil {
		t.Fatalf("insert %s revenue: %v", kind, err)
	}
}

func (harness *harness) insertRollup(t *testing.T, environment httpapi.Environment, customerID uuid.UUID, period signals.Period, netRevenue money.Amount) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO period_rollups (environment, customer_id, period_start, period_end, revenue_net_nanos, cost_nanos, uncosted_count, decision_counts)
		VALUES ($1, $2, $3, $4, $5, 0, 0, '{}')`,
		environment, customerID, period.Start, period.End, netRevenue)
	if err != nil {
		t.Fatalf("insert rollup: %v", err)
	}
}

func (harness *harness) setEveryRollupRevenue(ctx context.Context, t *testing.T, netRevenue money.Amount) {
	t.Helper()
	if _, err := harness.pool.Exec(ctx, "UPDATE period_rollups SET revenue_net_nanos = $1", netRevenue); err != nil {
		t.Fatalf("update rollups: %v", err)
	}
}

func (harness *harness) loadTest(t *testing.T, customerID uuid.UUID) signals.CustomerState {
	t.Helper()
	state, err := harness.loader.Load(t.Context(), httpapi.EnvironmentTest, customerID, harness.clock.Now())
	if err != nil {
		t.Fatalf("Load(test, %s) error = %v", customerID, err)
	}
	return state
}

func pointer[Value any](value Value) *Value {
	return &value
}

func dollars(amount int64) money.Amount {
	return money.Amount(amount * nanosPerDollar)
}
