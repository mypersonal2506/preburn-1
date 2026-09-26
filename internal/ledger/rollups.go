package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/ledger/queries"
	"github.com/preburn/preburn/internal/money"
)

const (
	revenueKindSubscription = "subscription"
	revenueKindAdjustment   = "adjustment"
	revenueKindStripeFee    = "stripe_fee"
	revenueKindRefund       = "refund"
	revenueKindCreditNote   = "credit_note"
)

// DecisionCounts is the decision_counts document of a period rollup: the
// number of decisions of each outcome whose period starts at the rollup's
// period start.
type DecisionCounts struct {
	// Allow counts decisions with outcome allow.
	Allow int64 `json:"allow"`
	// Route counts decisions with outcome route.
	Route int64 `json:"route"`
	// Cap counts decisions with outcome cap.
	Cap int64 `json:"cap"`
	// Deny counts decisions with outcome deny.
	Deny int64 `json:"deny"`
}

type billingPeriod struct {
	start time.Time
	end   time.Time
}

type periodActivity struct {
	ledgerTotals   []queries.SumLedgerEntriesByPeriodRow
	decisionCounts []queries.CountDecisionsByPeriodRow
}

type periodRollup struct {
	billingPeriod
	revenueNet     money.Amount
	cost           money.Amount
	uncostedCount  int32
	decisionCounts DecisionCounts
}

// RefreshRollups recomputes, inside transaction, every period rollup of the
// customer whose billing period contains anchor, and writes them to
// period_rollups. Callers pass the period start of the revenue entry, ledger
// entry or decision they wrote as anchor, which covers every rollup the write
// can change.
//
// The billing periods it knows are the non-empty periods of the customer's
// subscription revenue entries, the periods of existing rollups and the
// periods of the ledger entries and decisions that start at anchor or at the
// start of a known period that contains anchor. A new ledger or decision
// period therefore becomes a rollup when a refresh runs at its start, which
// rollup refresh jobs do after every report, deny, release and expiry, so a
// customer whose decisions were never reported still gets the rollup that
// holds their counts. When periods share a start, the rollup ends at the
// latest end.
//
// Net revenue adds subscription and adjustment entries and subtracts
// stripe_fee, refund and credit_note entries. The uncosted count leaves out
// uncosted ledger entries that a correction entry of the same period names in
// correction_of.
//
// It first locks the customer row with FOR NO KEY UPDATE, so refreshes of one
// customer run one after another and each reads what the previous one
// committed. A rollup refresh job locks its job row before the customer row,
// so a transaction that also inserts a RollupRefreshArgs job inserts it
// before calling RefreshRollups. It returns an error wrapping pgx.ErrNoRows
// when the customer does not exist in environment, and an error wrapping
// money.ErrOverflow when a net revenue does not fit int64.
func RefreshRollups(ctx context.Context, transaction pgx.Tx, environment httpapi.Environment, customerID uuid.UUID, anchor time.Time) error {
	store := queries.New(transaction)
	storeEnvironment := queries.Environment(environment)
	_, err := store.LockRollupCustomer(ctx, queries.LockRollupCustomerParams{Environment: storeEnvironment, CustomerID: customerID})
	if err != nil {
		return fmt.Errorf("lock rollup customer: %w", err)
	}
	periods, activity, err := loadBillingPeriods(ctx, store, storeEnvironment, customerID, anchor)
	if err != nil {
		return err
	}
	rollups := rollupsContaining(periods, anchor)
	if len(rollups) == 0 {
		return nil
	}
	for _, totals := range activity.ledgerTotals {
		rollup := rollupStartingAt(rollups, totals.PeriodStart)
		rollup.cost = money.Amount(totals.CostNanos)
		rollup.uncostedCount = totals.UncostedCount
	}
	for _, counts := range activity.decisionCounts {
		rollupStartingAt(rollups, counts.PeriodStart).decisionCounts = DecisionCounts{
			Allow: counts.AllowCount,
			Route: counts.RouteCount,
			Cap:   counts.CapCount,
			Deny:  counts.DenyCount,
		}
	}
	if err := addRevenue(ctx, store, storeEnvironment, customerID, periods, rollups); err != nil {
		return err
	}
	return writeRollups(ctx, store, storeEnvironment, customerID, rollups)
}

func loadBillingPeriods(ctx context.Context, store *queries.Queries, environment queries.Environment, customerID uuid.UUID, anchor time.Time) ([]billingPeriod, periodActivity, error) {
	recordedPeriods, err := store.ListRecordedBillingPeriods(ctx, queries.ListRecordedBillingPeriodsParams{Environment: environment, CustomerID: customerID})
	if err != nil {
		return nil, periodActivity{}, fmt.Errorf("list recorded billing periods: %w", err)
	}
	periods := make([]billingPeriod, 0, len(recordedPeriods)+1)
	activityStarts := []time.Time{anchor}
	for _, recorded := range recordedPeriods {
		period := billingPeriod{start: recorded.PeriodStart, end: recorded.PeriodEnd}
		periods = append(periods, period)
		if period.contains(anchor) {
			activityStarts = append(activityStarts, period.start)
		}
	}
	var activity periodActivity
	activity.ledgerTotals, err = store.SumLedgerEntriesByPeriod(ctx, queries.SumLedgerEntriesByPeriodParams{
		Environment:  environment,
		CustomerID:   customerID,
		PeriodStarts: activityStarts,
	})
	if err != nil {
		return nil, periodActivity{}, fmt.Errorf("sum ledger entries: %w", err)
	}
	activity.decisionCounts, err = store.CountDecisionsByPeriod(ctx, queries.CountDecisionsByPeriodParams{
		Environment:  environment,
		CustomerID:   customerID,
		PeriodStarts: activityStarts,
	})
	if err != nil {
		return nil, periodActivity{}, fmt.Errorf("count decisions: %w", err)
	}
	for _, totals := range activity.ledgerTotals {
		periods = addBillingPeriod(periods, billingPeriod{start: totals.PeriodStart, end: totals.PeriodEnd})
	}
	for _, counts := range activity.decisionCounts {
		periods = addBillingPeriod(periods, billingPeriod{start: counts.PeriodStart, end: counts.PeriodEnd})
	}
	return periods, activity, nil
}

func addRevenue(ctx context.Context, store *queries.Queries, environment queries.Environment, customerID uuid.UUID, periods []billingPeriod, rollups []*periodRollup) error {
	latestRollup := slices.MaxFunc(rollups, func(left, right *periodRollup) int { return left.end.Compare(right.end) })
	entries, err := store.ListRevenueEntriesStartingWithin(ctx, queries.ListRevenueEntriesStartingWithinParams{
		Environment:   environment,
		CustomerID:    customerID,
		EarliestStart: rollups[0].start,
		LatestEnd:     latestRollup.end,
	})
	if err != nil {
		return fmt.Errorf("list revenue entries: %w", err)
	}
	for _, entry := range entries {
		owner, contained := latestContaining(periods, entry.PeriodStart)
		if !contained {
			continue
		}
		rollup := rollupStartingAt(rollups, owner.start)
		if rollup == nil {
			continue
		}
		revenueNet, err := signedRevenueSum(rollup.revenueNet, entry.Kind, entry.AmountNanos)
		if err != nil {
			return err
		}
		rollup.revenueNet = revenueNet
	}
	return nil
}

func writeRollups(ctx context.Context, store *queries.Queries, environment queries.Environment, customerID uuid.UUID, rollups []*periodRollup) error {
	for _, rollup := range rollups {
		decisionCounts, err := json.Marshal(rollup.decisionCounts)
		if err != nil {
			return fmt.Errorf("encode decision counts: %w", err)
		}
		err = store.UpsertPeriodRollup(ctx, queries.UpsertPeriodRollupParams{
			Environment:     environment,
			CustomerID:      customerID,
			PeriodStart:     rollup.start,
			PeriodEnd:       rollup.end,
			RevenueNetNanos: rollup.revenueNet,
			CostNanos:       rollup.cost,
			UncostedCount:   rollup.uncostedCount,
			DecisionCounts:  decisionCounts,
		})
		if err != nil {
			return fmt.Errorf("upsert period rollup: %w", err)
		}
	}
	return nil
}

func (period billingPeriod) contains(moment time.Time) bool {
	return !moment.Before(period.start) && moment.Before(period.end)
}

func addBillingPeriod(periods []billingPeriod, added billingPeriod) []billingPeriod {
	for index, period := range periods {
		if period.start.Equal(added.start) {
			if added.end.After(period.end) {
				periods[index].end = added.end
			}
			return periods
		}
	}
	return append(periods, added)
}

func rollupsContaining(periods []billingPeriod, anchor time.Time) []*periodRollup {
	var rollups []*periodRollup
	for _, period := range periods {
		if period.contains(anchor) {
			rollups = append(rollups, &periodRollup{billingPeriod: period})
		}
	}
	slices.SortFunc(rollups, func(left, right *periodRollup) int { return left.start.Compare(right.start) })
	return rollups
}

func latestContaining(periods []billingPeriod, moment time.Time) (billingPeriod, bool) {
	var latest billingPeriod
	contained := false
	for _, period := range periods {
		if period.contains(moment) && (!contained || period.start.After(latest.start)) {
			latest = period
			contained = true
		}
	}
	return latest, contained
}

func rollupStartingAt(rollups []*periodRollup, start time.Time) *periodRollup {
	index := slices.IndexFunc(rollups, func(rollup *periodRollup) bool { return rollup.start.Equal(start) })
	if index < 0 {
		return nil
	}
	return rollups[index]
}

func signedRevenueSum(revenueNet money.Amount, kind string, amount money.Amount) (money.Amount, error) {
	switch kind {
	case revenueKindSubscription, revenueKindAdjustment:
		return revenueNet.Add(amount)
	case revenueKindStripeFee, revenueKindRefund, revenueKindCreditNote:
		return revenueNet.Subtract(amount)
	}
	return 0, fmt.Errorf("unknown revenue kind %q", kind)
}
