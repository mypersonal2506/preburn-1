package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/ledger"
)

func TestRefreshRollupsKeepsOverlappingSubscriptionPeriodsApart(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)

	insertSubscription(t, pool, customerID, dollars(30), day(time.August, 1), day(time.September, 1))
	refreshRollups(t, pool, customerID, day(time.August, 1))
	insertSubscription(t, pool, customerID, dollars(30), day(time.August, 2), day(time.September, 2))
	refreshRollups(t, pool, customerID, day(time.August, 2))

	want := []rollup{
		{PeriodStart: day(time.August, 1), PeriodEnd: day(time.September, 1), RevenueNet: dollars(30)},
		{PeriodStart: day(time.August, 2), PeriodEnd: day(time.September, 2), RevenueNet: dollars(30)},
	}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRefreshRollupsFoldsZeroLengthLineIntoContainingPeriod(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	insertSubscription(t, pool, customerID, dollars(30), day(time.August, 1), day(time.September, 1))
	refreshRollups(t, pool, customerID, day(time.August, 1))

	insertSubscription(t, pool, customerID, dollars(5), day(time.August, 15), day(time.August, 15))
	refreshRollups(t, pool, customerID, day(time.August, 15))

	want := []rollup{{PeriodStart: day(time.August, 1), PeriodEnd: day(time.September, 1), RevenueNet: dollars(35)}}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRefreshRollupsSubtractsFeesRefundsAndCreditNotes(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	periodStart, periodEnd := day(time.August, 1), day(time.September, 1)
	insertSubscription(t, pool, customerID, dollars(30), periodStart, periodEnd)
	for kind, amount := range map[string]int64{"adjustment": 4, "stripe_fee": 1, "refund": 5, "credit_note": 2} {
		insertRevenueEntry(t, pool, customerID, kind, dollars(amount), day(time.August, 10), day(time.August, 20), day(time.August, 10))
	}

	refreshRollups(t, pool, customerID, day(time.August, 10))

	want := []rollup{{PeriodStart: periodStart, PeriodEnd: periodEnd, RevenueNet: dollars(30 + 4 - 1 - 5 - 2)}}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRefreshRollupsPutsLateCreditNoteInItsInvoicePeriod(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	periodStarts := []time.Time{day(time.August, 1), day(time.September, 1), day(time.October, 1), day(time.November, 1)}
	for index := range len(periodStarts) - 1 {
		insertSubscription(t, pool, customerID, dollars(30), periodStarts[index], periodStarts[index+1])
		refreshRollups(t, pool, customerID, periodStarts[index])
	}

	insertRevenueEntry(t, pool, customerID, "credit_note", dollars(10), day(time.August, 1), day(time.September, 1), day(time.October, 5))
	refreshRollups(t, pool, customerID, day(time.August, 1))

	want := []rollup{
		{PeriodStart: day(time.August, 1), PeriodEnd: day(time.September, 1), RevenueNet: dollars(20)},
		{PeriodStart: day(time.September, 1), PeriodEnd: day(time.October, 1), RevenueNet: dollars(30)},
		{PeriodStart: day(time.October, 1), PeriodEnd: day(time.November, 1), RevenueNet: dollars(30)},
	}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRefreshRollupsAddsCostToPeriodWithEqualStart(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	insertSubscription(t, pool, customerID, dollars(30), day(time.August, 1), day(time.September, 1))
	insertSubscription(t, pool, customerID, dollars(30), day(time.August, 2), day(time.September, 2))
	for _, entry := range []ledgerEntryFixture{
		costedEntry(customerID, day(time.August, 1), day(time.September, 1), dollars(1)),
		costedEntry(customerID, day(time.August, 1), day(time.September, 1), dollars(2)),
		uncostedEntry(customerID, day(time.August, 1), day(time.September, 1)),
		costedEntry(customerID, day(time.August, 2), day(time.September, 2), dollars(5)),
	} {
		insertLedgerEntry(t, pool, entry)
	}
	uncostedID := insertLedgerEntry(t, pool, uncostedEntry(customerID, day(time.August, 2), day(time.September, 2)))
	correction := costedEntry(customerID, day(time.August, 2), day(time.September, 2), dollars(3))
	correction.correctionOf = &uncostedID
	insertLedgerEntry(t, pool, correction)

	refreshRollups(t, pool, customerID, day(time.August, 2))

	want := []rollup{
		{PeriodStart: day(time.August, 1), PeriodEnd: day(time.September, 1), RevenueNet: dollars(30), Cost: dollars(3), UncostedCount: 1},
		{PeriodStart: day(time.August, 2), PeriodEnd: day(time.September, 2), RevenueNet: dollars(30), Cost: dollars(8)},
	}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRefreshRollupsCountsDecisionsOfPeriodWithEqualStart(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	insertSubscription(t, pool, customerID, dollars(30), day(time.August, 1), day(time.September, 1))
	insertSubscription(t, pool, customerID, dollars(30), day(time.August, 2), day(time.September, 2))
	for _, outcome := range []string{"allow", "allow", "route", "cap", "deny"} {
		insertDecision(t, pool, customerID, outcome, day(time.August, 1), day(time.September, 1))
	}
	insertDecision(t, pool, customerID, "deny", day(time.August, 2), day(time.September, 2))

	refreshRollups(t, pool, customerID, day(time.August, 2))

	want := []rollup{
		{
			PeriodStart:    day(time.August, 1),
			PeriodEnd:      day(time.September, 1),
			RevenueNet:     dollars(30),
			DecisionCounts: ledger.DecisionCounts{Allow: 2, Route: 1, Cap: 1, Deny: 1},
		},
		{
			PeriodStart:    day(time.August, 2),
			PeriodEnd:      day(time.September, 2),
			RevenueNet:     dollars(30),
			DecisionCounts: ledger.DecisionCounts{Deny: 1},
		},
	}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRefreshRollupsCreatesTheRollupOfADecisionPeriod(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	insertDecision(t, pool, customerID, "deny", day(time.September, 1), day(time.October, 1))

	refreshRollups(t, pool, customerID, day(time.September, 1))

	want := []rollup{{PeriodStart: day(time.September, 1), PeriodEnd: day(time.October, 1), DecisionCounts: ledger.DecisionCounts{Deny: 1}}}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRefreshRollupsKeepsLedgerPeriodAsBillingPeriod(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	insertLedgerEntry(t, pool, costedEntry(customerID, day(time.September, 1), day(time.October, 1), dollars(2)))
	refreshRollups(t, pool, customerID, day(time.September, 1))

	insertRevenueEntry(t, pool, customerID, "adjustment", dollars(12), day(time.September, 15), day(time.September, 15), day(time.September, 15))
	refreshRollups(t, pool, customerID, day(time.September, 15))

	want := []rollup{{PeriodStart: day(time.September, 1), PeriodEnd: day(time.October, 1), RevenueNet: dollars(12), Cost: dollars(2)}}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRefreshRollupsLeavesRevenueOutsideBillingPeriods(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)

	insertRevenueEntry(t, pool, customerID, "adjustment", dollars(12), day(time.September, 15), day(time.September, 20), day(time.September, 15))
	refreshRollups(t, pool, customerID, day(time.September, 15))

	if rollups := listRollups(t, pool, customerID); len(rollups) != 0 {
		t.Errorf("rollups = %v, want none", rollups)
	}
}

func TestRefreshRollupsRejectsCustomerOfOtherEnvironment(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)

	err := database.InTransaction(t.Context(), pool, func(ctx context.Context, transaction pgx.Tx) error {
		return ledger.RefreshRollups(ctx, transaction, httpapi.EnvironmentLive, customerID, day(time.August, 1))
	})

	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("RefreshRollups error = %v, want one wrapping pgx.ErrNoRows", err)
	}
}
