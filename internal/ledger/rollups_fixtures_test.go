package ledger_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/money"
)

const (
	nanosPerDollar = 1_000_000_000
	microsPerUnit  = 1_000_000
	waitTimeout    = 10 * time.Second
	pollInterval   = 10 * time.Millisecond
	testFeature    = "text_to_video"
	testProvider   = "fal_ai"
	testModel      = "fal-ai/veo3"
	testMeter      = "output_seconds"
)

type executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type ledgerEntryFixture struct {
	environment  httpapi.Environment
	customerID   uuid.UUID
	periodStart  time.Time
	periodEnd    time.Time
	cost         *money.Amount
	correctionOf *uuid.UUID
	usage        map[string]money.Quantity
	occurredAt   time.Time
}

type rollup struct {
	PeriodStart    time.Time
	PeriodEnd      time.Time
	RevenueNet     money.Amount
	Cost           money.Amount
	UncostedCount  int32
	DecisionCounts ledger.DecisionCounts
}

func day(month time.Month, dayOfMonth int) time.Time {
	return time.Date(2026, month, dayOfMonth, 0, 0, 0, 0, time.UTC)
}

func dollars(amount int64) money.Amount {
	return money.Amount(amount * nanosPerDollar)
}

func units(amount int64) money.Quantity {
	return money.Quantity(amount * microsPerUnit)
}

func pointer[Value any](value Value) *Value {
	return &value
}

func insertCustomer(t *testing.T, pool *pgxpool.Pool, environment httpapi.Environment) uuid.UUID {
	t.Helper()
	customerID := identifiers.New()
	_, err := pool.Exec(t.Context(),
		"INSERT INTO customers (customer_id, environment, external_id, status) VALUES ($1, $2, $3, 'active')",
		customerID, string(environment), "customer-"+customerID.String(),
	)
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	return customerID
}

func insertRevenueEntry(t *testing.T, target executor, customerID uuid.UUID, kind string, amount money.Amount, periodStart, periodEnd, occurredAt time.Time) {
	t.Helper()
	_, err := target.Exec(t.Context(),
		`INSERT INTO revenue_entries (revenue_entry_id, environment, customer_id, period_start, period_end, kind, amount_nanos, source, source_reference, occurred_at)
		VALUES ($1, 'test', $2, $3, $4, $5, $6, 'api', $7, $8)`,
		identifiers.New(), customerID, periodStart, periodEnd, kind, int64(amount), identifiers.New().String(), occurredAt,
	)
	if err != nil {
		t.Fatalf("insert %s revenue entry: %v", kind, err)
	}
}

func insertSubscription(t *testing.T, pool *pgxpool.Pool, customerID uuid.UUID, amount money.Amount, periodStart, periodEnd time.Time) {
	t.Helper()
	insertRevenueEntry(t, pool, customerID, "subscription", amount, periodStart, periodEnd, periodStart)
}

func insertLedgerEntry(t *testing.T, target executor, entry ledgerEntryFixture) uuid.UUID {
	t.Helper()
	usage, err := json.Marshal(entry.usage)
	if err != nil {
		t.Fatalf("encode usage: %v", err)
	}
	costStatus := "costed"
	var costNanos *int64
	if entry.cost == nil {
		costStatus = "uncosted"
	} else {
		costNanos = pointer(int64(*entry.cost))
	}
	ledgerEntryID := identifiers.New()
	_, err = target.Exec(t.Context(),
		`INSERT INTO ledger_entries (ledger_entry_id, environment, customer_id, idempotency_key, feature, provider, model, attributes,
			usage, cost_nanos, cost_breakdown, cost_status, decision_source, period_start, period_end, correction_of, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, '{}', $8, $9, '{}', $10, 'server', $11, $12, $13, $14)`,
		ledgerEntryID, string(entry.environment), entry.customerID, ledgerEntryID.String(), testFeature, testProvider, testModel,
		usage, costNanos, costStatus, entry.periodStart, entry.periodEnd, entry.correctionOf, entry.occurredAt,
	)
	if err != nil {
		t.Fatalf("insert ledger entry: %v", err)
	}
	return ledgerEntryID
}

func costedEntry(customerID uuid.UUID, periodStart, periodEnd time.Time, cost money.Amount) ledgerEntryFixture {
	return ledgerEntryFixture{
		environment: httpapi.EnvironmentTest,
		customerID:  customerID,
		periodStart: periodStart,
		periodEnd:   periodEnd,
		cost:        &cost,
		usage:       map[string]money.Quantity{testMeter: units(8)},
		occurredAt:  periodStart,
	}
}

func uncostedEntry(customerID uuid.UUID, periodStart, periodEnd time.Time) ledgerEntryFixture {
	entry := costedEntry(customerID, periodStart, periodEnd, 0)
	entry.cost = nil
	return entry
}

func insertDecision(t *testing.T, pool *pgxpool.Pool, customerID uuid.UUID, outcome string, periodStart, periodEnd time.Time) {
	t.Helper()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO decisions (decision_id, environment, customer_id, feature, requested_provider, requested_model, provider, model,
			attributes, overrides, outcome, reason, signals, reserved_nanos, estimate_basis, status, period_start, period_end, expires_at)
		VALUES ($1, 'test', $2, $3, $4, $5, $4, $5, '{}', '{}', $6, 'policy_matched', '{}', 0, 'none', 'released', $7, $8, $7)`,
		identifiers.New(), customerID, testFeature, testProvider, testModel, outcome, periodStart, periodEnd,
	)
	if err != nil {
		t.Fatalf("insert %s decision: %v", outcome, err)
	}
}

func refreshRollups(t *testing.T, pool *pgxpool.Pool, customerID uuid.UUID, anchor time.Time) {
	t.Helper()
	err := database.InTransaction(t.Context(), pool, func(ctx context.Context, transaction pgx.Tx) error {
		return ledger.RefreshRollups(ctx, transaction, httpapi.EnvironmentTest, customerID, anchor)
	})
	if err != nil {
		t.Fatalf("RefreshRollups at %s: %v", anchor, err)
	}
}

func listRollups(t *testing.T, pool *pgxpool.Pool, customerID uuid.UUID) []rollup {
	t.Helper()
	rows, err := pool.Query(t.Context(),
		`SELECT period_start, period_end, revenue_net_nanos, cost_nanos, uncosted_count, decision_counts
		FROM period_rollups
		WHERE customer_id = $1
		ORDER BY period_start`,
		customerID,
	)
	if err != nil {
		t.Fatalf("select rollups: %v", err)
	}
	var rollups []rollup
	for rows.Next() {
		var found rollup
		var revenueNet, cost int64
		var decisionCounts []byte
		if err := rows.Scan(&found.PeriodStart, &found.PeriodEnd, &revenueNet, &cost, &found.UncostedCount, &decisionCounts); err != nil {
			t.Fatalf("scan rollup: %v", err)
		}
		if err := json.Unmarshal(decisionCounts, &found.DecisionCounts); err != nil {
			t.Fatalf("decode decision counts %s: %v", decisionCounts, err)
		}
		found.RevenueNet = money.Amount(revenueNet)
		found.Cost = money.Amount(cost)
		rollups = append(rollups, found)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read rollups: %v", err)
	}
	return rollups
}

func waitForLockWaiters(ctx context.Context, t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		var waiting int
		err := pool.QueryRow(ctx, `SELECT count(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
				AND wait_event_type = 'Lock'`).Scan(&waiting)
		if err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if waiting == want {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("sessions waiting on a lock = %d, want %d", waiting, want)
		}
	}
}
