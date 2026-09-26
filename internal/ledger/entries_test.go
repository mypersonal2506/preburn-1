package ledger_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing"
)

var compareAttributeValues = cmp.Comparer(func(left, right pricing.AttributeValue) bool { return left == right })

func TestInsertEntryStoresTheEntry(t *testing.T) {
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	entry := costedLedgerEntry(customerID)

	stored, duplicate := insertEntry(t, pool, entry)

	if duplicate {
		t.Error("duplicate = true, want false for a new idempotency key")
	}
	if diff := cmp.Diff(entry, stored, compareAttributeValues); diff != "" {
		t.Errorf("stored entry mismatch (-want +got):\n%s", diff)
	}
	var usage, costBreakdown map[string]any
	var costNanos *int64
	var attributes string
	err := pool.QueryRow(t.Context(), "SELECT attributes::text, usage, cost_breakdown, cost_nanos FROM ledger_entries WHERE ledger_entry_id = $1", entry.ID).
		Scan(&attributes, &usage, &costBreakdown, &costNanos)
	if err != nil {
		t.Fatalf("select ledger entry: %v", err)
	}
	if attributes != `{"audio": false}` {
		t.Errorf("attributes column = %s, want {\"audio\": false}", attributes)
	}
	if diff := cmp.Diff(map[string]any{"output_seconds": float64(6_500_000), "requests": float64(1_000_000)}, usage); diff != "" {
		t.Errorf("usage column mismatch (-want +got):\n%s", diff)
	}
	wantBreakdown := map[string]any{"lines": []any{
		map[string]any{
			"meter": "output_seconds", "quantity_micros": float64(7_000_000), "unit_price_nanos": float64(100_000_000), "unit_quantity": float64(1),
			"cost_nanos": float64(700_000_000), "pricing_rule_id": entry.Rating.Lines[0].RuleID.String(), "pricing_override_id": nil, "missing": false,
		},
		map[string]any{
			"meter": "requests", "quantity_micros": float64(1_000_000), "unit_price_nanos": float64(50_000_000), "unit_quantity": float64(1),
			"cost_nanos": float64(50_000_000), "pricing_rule_id": nil, "pricing_override_id": entry.Rating.Lines[1].OverrideID.String(), "missing": false,
		},
	}}
	if diff := cmp.Diff(wantBreakdown, costBreakdown); diff != "" {
		t.Errorf("cost breakdown column mismatch (-want +got):\n%s", diff)
	}
	if costNanos == nil || *costNanos != int64(dollars(1)*3/4) {
		t.Errorf("cost_nanos = %v, want 750000000", costNanos)
	}
}

func TestInsertEntryReturnsTheStoredEntryForARepeatedIdempotencyKey(t *testing.T) {
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	first := costedLedgerEntry(customerID)
	insertEntry(t, pool, first)
	repeated := costedLedgerEntry(customerID)
	repeated.Usage = map[pricing.Meter]money.Quantity{pricing.MeterOutputSeconds: units(9)}
	repeated.OccurredAt = first.OccurredAt.Add(time.Minute)

	stored, duplicate := insertEntry(t, pool, repeated)

	if !duplicate {
		t.Error("duplicate = false, want true for a repeated idempotency key")
	}
	if diff := cmp.Diff(first, stored, compareAttributeValues); diff != "" {
		t.Errorf("returned entry mismatch (-want +got):\n%s", diff)
	}
	liveEntry := costedLedgerEntry(insertCustomer(t, pool, httpapi.EnvironmentLive))
	liveEntry.Environment = httpapi.EnvironmentLive
	if _, duplicate := insertEntry(t, pool, liveEntry); duplicate {
		t.Error("duplicate = true for the same idempotency key in the live environment, want false")
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM ledger_entries").Scan(&count); err != nil || count != 2 {
		t.Errorf("ledger entries = %d, %v, want one per environment", count, err)
	}
}

func TestInsertEntryStoresUncostedUsageWithoutCost(t *testing.T) {
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	entry := costedLedgerEntry(customerID)
	entry.DecisionID = nil
	entry.DecisionSource = ledger.DecisionSourceFallback
	entry.IdempotencyKey = identifiers.New().String()
	entry.Attributes = pricing.Attributes{}
	entry.Rating = pricing.RatedRequest{
		CostStatus: pricing.CostStatusUncosted,
		Lines:      []pricing.RatedLine{{Meter: pricing.MeterOutputSeconds, Quantity: units(6), Missing: true}},
	}

	stored, _ := insertEntry(t, pool, entry)

	if diff := cmp.Diff(entry, stored, compareAttributeValues); diff != "" {
		t.Errorf("stored entry mismatch (-want +got):\n%s", diff)
	}
	var costNanos *int64
	var costStatus string
	if err := pool.QueryRow(t.Context(), "SELECT cost_nanos, cost_status FROM ledger_entries WHERE ledger_entry_id = $1", entry.ID).Scan(&costNanos, &costStatus); err != nil {
		t.Fatalf("select ledger entry: %v", err)
	}
	if costNanos != nil || costStatus != "uncosted" {
		t.Errorf("cost_nanos %v and cost_status %s, want null and uncosted", costNanos, costStatus)
	}
}

func costedLedgerEntry(customerID uuid.UUID) ledger.Entry {
	decisionID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	cost := dollars(1) * 3 / 4
	return ledger.Entry{
		ID:             identifiers.New(),
		Environment:    httpapi.EnvironmentTest,
		CustomerID:     customerID,
		DecisionID:     &decisionID,
		IdempotencyKey: "decision:" + decisionID.String(),
		Feature:        testFeature,
		Provider:       testProvider,
		Model:          testModel,
		Attributes:     pricing.Attributes{"audio": pricing.BooleanAttribute(false)},
		Usage: map[pricing.Meter]money.Quantity{
			pricing.MeterOutputSeconds: units(13) / 2,
			pricing.MeterRequests:      units(1),
		},
		Rating: pricing.RatedRequest{
			CostStatus: pricing.CostStatusCosted,
			Cost:       &cost,
			Lines: []pricing.RatedLine{
				{
					Meter:     pricing.MeterOutputSeconds,
					Quantity:  units(7),
					UnitPrice: money.UnitPrice{Nanos: dollars(1) / 10, UnitQuantity: 1},
					Cost:      dollars(1) * 7 / 10,
					RuleID:    pointer(identifiers.New()),
				},
				{
					Meter:      pricing.MeterRequests,
					Quantity:   units(1),
					UnitPrice:  money.UnitPrice{Nanos: dollars(1) / 20, UnitQuantity: 1},
					Cost:       dollars(1) / 20,
					OverrideID: pointer(identifiers.New()),
				},
			},
		},
		DecisionSource: ledger.DecisionSourceServer,
		PeriodStart:    day(time.September, 1),
		PeriodEnd:      day(time.October, 1),
		OccurredAt:     day(time.September, 12).Add(90 * time.Minute),
		CreatedAt:      day(time.September, 12).Add(91 * time.Minute),
	}
}

func insertEntry(t *testing.T, pool *pgxpool.Pool, entry ledger.Entry) (ledger.Entry, bool) {
	t.Helper()
	var stored ledger.Entry
	var duplicate bool
	err := database.InTransaction(t.Context(), pool, func(ctx context.Context, transaction pgx.Tx) error {
		var err error
		stored, duplicate, err = ledger.InsertEntry(ctx, transaction, entry)
		return err
	})
	if err != nil {
		t.Fatalf("insert ledger entry: %v", err)
	}
	return stored, duplicate
}
