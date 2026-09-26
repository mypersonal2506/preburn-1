package dashboard

import (
	"cmp"
	"context"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/dashboard/queries"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/signals"
)

const (
	// AttentionPaceThreshold is the pace above which the overview's attention
	// counts a customer.
	AttentionPaceThreshold = 2.0

	lossCustomerLimit  = 5
	policyChangeLimit  = 5
	droppedReportsDays = 7
	lastThirtyDays     = 30
	basisPointsPerUnit = 10_000
)

// OverviewPeriod is what the overview sums.
type OverviewPeriod string

const (
	// OverviewPeriodCurrent sums the rollup of each customer's current
	// period, the latest-starting rollup that contains now.
	OverviewPeriodCurrent OverviewPeriod = "current"
	// OverviewPeriodPrevious sums the rollup of each customer's previous
	// period, the latest-starting rollup that contains the instant before
	// its current period starts. A customer without a current rollup counts
	// the UTC calendar month as its current period.
	OverviewPeriodPrevious OverviewPeriod = "previous"
	// OverviewPeriodLast30Days sums the revenue entries, ledger entries and
	// decisions of today and the 29 UTC days before it.
	OverviewPeriodLast30Days OverviewPeriod = "last_30_days"
)

// PolicyChange tells whether a policy's latest change created it or updated
// it.
type PolicyChange string

const (
	// PolicyChangeCreated is a policy at version 1, never updated.
	PolicyChangeCreated PolicyChange = "created"
	// PolicyChangeUpdated is a policy updated at least once.
	PolicyChangeUpdated PolicyChange = "updated"
)

// OverviewService reads the dashboard overview. Create one with
// NewOverviewService. It is safe for concurrent use.
type OverviewService struct {
	queries *queries.Queries
	cache   *cache.Client
	signals signalReader
	clock   clock.Clock
}

type customerTotals struct {
	customerID    uuid.UUID
	externalID    string
	displayName   *string
	planID        *uuid.UUID
	revenueNet    money.Amount
	cost          money.Amount
	uncostedCount int64
}

type periodSelection struct {
	customers []customerTotals
	decisions queries.SumDecisionsInPeriodsRow
	dates     dateRange
}

type dateRange struct {
	start time.Time
	end   time.Time
}

type totals struct {
	customerCount int64
	revenueNet    money.Amount
	cost          money.Amount
	uncostedCount int64
}

// NewOverviewService returns an OverviewService that reads pool, the dropped
// report counters in cacheClient, customer states through states and period
// counters through counters. It only stores its arguments, so zero values
// serve route registration for the OpenAPI document.
func NewOverviewService(pool *pgxpool.Pool, cacheClient *cache.Client, states *customerstate.Loader, counters *decisions.Counters, timeSource clock.Clock) *OverviewService {
	store := queries.New(pool)
	return &OverviewService{
		queries: store,
		cache:   cacheClient,
		signals: signalReader{queries: store, states: states, counters: counters},
		clock:   timeSource,
	}
}

// Overview returns the overview of environment for period. Revenue, cost,
// the uncosted count, the plan margins and the customers to watch come from
// the customer totals of period, whether rollups or the last 30 days of
// entries. The customers to watch are the 5 customers with revenue above
// zero and a margin below zero, lowest margin first. Customers count under
// their effective plan of today. The target
// margin weights the target of each margin target plan with revenue above
// zero by that revenue. A plan is below target when it has revenue above
// zero and a margin below its target. Decision counts and cost avoided cover
// the decisions of the same customer periods, or of the last 30 days.
// Attention also counts the active customers whose current pace is above
// AttentionPaceThreshold and the reports dropped today and in the 6 UTC days
// before. It returns ErrEnvironmentTooLarge when environment has more than
// ActiveCustomerMaximum active customers.
func (service *OverviewService) Overview(ctx context.Context, environment httpapi.Environment, period OverviewPeriod) (OverviewResponse, error) {
	now := service.clock.Now()
	selection, err := service.selectPeriod(ctx, environment, period, now)
	if err != nil {
		return OverviewResponse{}, err
	}
	planRows, err := service.queries.ListPlans(ctx, queries.Environment(environment))
	if err != nil {
		return OverviewResponse{}, fmt.Errorf("list plans of environment %s: %w", environment, err)
	}
	var sum totals
	for _, customer := range selection.customers {
		if err := sum.add(customer); err != nil {
			return OverviewResponse{}, err
		}
	}
	planMargins, targetMargin, err := planMargins(planRows, selection.customers)
	if err != nil {
		return OverviewResponse{}, err
	}
	overview := OverviewResponse{
		Period:         period,
		Revenue:        money.FormatAmount(sum.revenueNet),
		Cost:           money.FormatAmount(sum.cost),
		Margin:         formatMargin(sum.revenueNet, sum.cost),
		TargetMargin:   targetMargin,
		PlanMargins:    planMargins,
		DecisionCounts: newDecisionCountsResponse(selection.decisions),
		UncostedCount:  sum.uncostedCount,
	}
	if overview.CostAvoided, err = newCostAvoidedResponse(selection.decisions); err != nil {
		return OverviewResponse{}, err
	}
	if overview.Daily, err = service.daily(ctx, environment, selection.dates); err != nil {
		return OverviewResponse{}, err
	}
	if overview.LossCustomers, err = service.lossCustomers(ctx, environment, selection.customers); err != nil {
		return OverviewResponse{}, err
	}
	if overview.Attention, err = service.attention(ctx, environment, now, planMargins, sum.uncostedCount); err != nil {
		return OverviewResponse{}, err
	}
	if overview.PolicyChanges, err = service.policyChanges(ctx, environment); err != nil {
		return OverviewResponse{}, err
	}
	return overview, nil
}

func (service *OverviewService) selectPeriod(ctx context.Context, environment httpapi.Environment, period OverviewPeriod, now time.Time) (periodSelection, error) {
	storeEnvironment := queries.Environment(environment)
	calendarMonthStart := signals.ResolvePeriod(now, nil, nil).Start
	switch period {
	case OverviewPeriodCurrent:
		rows, err := service.queries.ListCurrentPeriodTotals(ctx, queries.ListCurrentPeriodTotalsParams{Environment: storeEnvironment, Now: now})
		if err != nil {
			return periodSelection{}, fmt.Errorf("list current period totals: %w", err)
		}
		selection, err := service.rollupSelection(ctx, storeEnvironment, rows, dateRange{start: calendarMonthStart, end: now})
		if err != nil {
			return periodSelection{}, err
		}
		selection.dates.end = now
		return selection, nil
	case OverviewPeriodPrevious:
		rows, err := service.queries.ListPreviousPeriodTotals(ctx, queries.ListPreviousPeriodTotalsParams{
			Environment:        storeEnvironment,
			CalendarMonthStart: calendarMonthStart,
			Now:                now,
		})
		if err != nil {
			return periodSelection{}, fmt.Errorf("list previous period totals: %w", err)
		}
		currentRows := make([]queries.ListCurrentPeriodTotalsRow, len(rows))
		for index, row := range rows {
			currentRows[index] = queries.ListCurrentPeriodTotalsRow(row)
		}
		selection, err := service.rollupSelection(ctx, storeEnvironment, currentRows, dateRange{start: calendarMonthStart.AddDate(0, -1, 0), end: calendarMonthStart})
		if err != nil {
			return periodSelection{}, err
		}
		if selection.dates.end.After(now) {
			selection.dates.end = now
		}
		return selection, nil
	case OverviewPeriodLast30Days:
		return service.windowSelection(ctx, storeEnvironment, dateRange{start: startOfDay(now).AddDate(0, 0, 1-lastThirtyDays), end: now})
	}
	return periodSelection{}, fmt.Errorf("unknown overview period %q", period)
}

func (service *OverviewService) rollupSelection(ctx context.Context, environment queries.Environment, rows []queries.ListCurrentPeriodTotalsRow, emptyDates dateRange) (periodSelection, error) {
	selection := periodSelection{customers: make([]customerTotals, len(rows)), dates: emptyDates}
	customerIDs := make([]uuid.UUID, len(rows))
	periodStarts := make([]time.Time, len(rows))
	for index, row := range rows {
		selection.customers[index] = customerTotals{
			customerID:    row.CustomerID,
			externalID:    row.ExternalID,
			displayName:   row.DisplayName,
			planID:        row.PlanID,
			revenueNet:    row.RevenueNetNanos,
			cost:          row.CostNanos,
			uncostedCount: int64(row.UncostedCount),
		}
		customerIDs[index] = row.CustomerID
		periodStarts[index] = row.PeriodStart
		if index == 0 || row.PeriodStart.Before(selection.dates.start) {
			selection.dates.start = startOfDay(row.PeriodStart)
		}
		if index == 0 || row.PeriodEnd.After(selection.dates.end) {
			selection.dates.end = row.PeriodEnd.UTC()
		}
	}
	decisionSums, err := service.queries.SumDecisionsInPeriods(ctx, queries.SumDecisionsInPeriodsParams{
		Environment:  environment,
		CustomerIds:  customerIDs,
		PeriodStarts: periodStarts,
	})
	if err != nil {
		return periodSelection{}, fmt.Errorf("sum decisions of customer periods: %w", err)
	}
	selection.decisions = decisionSums
	return selection, nil
}

func (service *OverviewService) windowSelection(ctx context.Context, environment queries.Environment, window dateRange) (periodSelection, error) {
	rows, err := service.queries.ListCustomerTotalsBetween(ctx, queries.ListCustomerTotalsBetweenParams{
		Environment: environment,
		WindowStart: window.start,
		WindowEnd:   window.end,
	})
	if err != nil {
		return periodSelection{}, fmt.Errorf("list customer totals from %s: %w", window.start, err)
	}
	decisionSums, err := service.queries.SumDecisionsBetween(ctx, queries.SumDecisionsBetweenParams{
		Environment: environment,
		WindowStart: window.start,
		WindowEnd:   window.end,
	})
	if err != nil {
		return periodSelection{}, fmt.Errorf("sum decisions from %s: %w", window.start, err)
	}
	selection := periodSelection{
		customers: make([]customerTotals, len(rows)),
		decisions: queries.SumDecisionsInPeriodsRow(decisionSums),
		dates:     window,
	}
	for index, row := range rows {
		selection.customers[index] = customerTotals{
			customerID:    row.CustomerID,
			externalID:    row.ExternalID,
			displayName:   row.DisplayName,
			planID:        row.PlanID,
			revenueNet:    money.Amount(row.RevenueNetNanos),
			cost:          money.Amount(row.CostNanos),
			uncostedCount: int64(row.UncostedCount),
		}
	}
	return selection, nil
}

func (service *OverviewService) daily(ctx context.Context, environment httpapi.Environment, dates dateRange) ([]DailyTotalsResponse, error) {
	rows, err := service.queries.ListDailyTotals(ctx, queries.ListDailyTotalsParams{
		Environment: queries.Environment(environment),
		RangeStart:  dates.start,
		RangeEnd:    dates.end,
	})
	if err != nil {
		return nil, fmt.Errorf("list daily totals from %s: %w", dates.start, err)
	}
	byDay := make(map[time.Time]queries.ListDailyTotalsRow, len(rows))
	for _, row := range rows {
		byDay[row.Day.UTC()] = row
	}
	series := []DailyTotalsResponse{}
	for day := dates.start; day.Before(dates.end); day = day.AddDate(0, 0, 1) {
		totalsOfDay := byDay[day]
		series = append(series, DailyTotalsResponse{
			Date:    day.Format(time.DateOnly),
			Revenue: money.FormatAmount(money.Amount(totalsOfDay.RevenueNetNanos)),
			Cost:    money.FormatAmount(money.Amount(totalsOfDay.CostNanos)),
		})
	}
	return series, nil
}

func (service *OverviewService) lossCustomers(ctx context.Context, environment httpapi.Environment, customers []customerTotals) ([]LossCustomerResponse, error) {
	losing := slices.DeleteFunc(slices.Clone(customers), func(customer customerTotals) bool {
		return customer.revenueNet <= 0 || customer.cost <= customer.revenueNet
	})
	slices.SortFunc(losing, func(left, right customerTotals) int {
		leftMargin, _ := marginOf(left.revenueNet, left.cost)
		rightMargin, _ := marginOf(right.revenueNet, right.cost)
		return cmp.Or(cmp.Compare(leftMargin, rightMargin), strings.Compare(left.externalID, right.externalID))
	})
	watched := losing[:min(len(losing), lossCustomerLimit)]
	customerIDs := make([]uuid.UUID, len(watched))
	for index, customer := range watched {
		customerIDs[index] = customer.customerID
	}
	outcomeRows, err := service.queries.ListLatestPolicyOutcomes(ctx, queries.ListLatestPolicyOutcomesParams{
		Environment: queries.Environment(environment),
		CustomerIds: customerIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list latest policy outcomes: %w", err)
	}
	latestOutcomes := make(map[uuid.UUID]queries.ListLatestPolicyOutcomesRow, len(outcomeRows))
	for _, row := range outcomeRows {
		latestOutcomes[row.CustomerID] = row
	}
	responses := make([]LossCustomerResponse, len(watched))
	for index, customer := range watched {
		responses[index] = LossCustomerResponse{
			ID:          identifiers.Encode(identifiers.PrefixCustomer, customer.customerID),
			ExternalID:  customer.externalID,
			DisplayName: customer.displayName,
			Revenue:     money.FormatAmount(customer.revenueNet),
			Cost:        money.FormatAmount(customer.cost),
			Margin:      *formatMargin(customer.revenueNet, customer.cost),
		}
		if customer.planID != nil {
			planID := identifiers.Encode(identifiers.PrefixPlan, *customer.planID)
			responses[index].PlanID = &planID
		}
		if latest, found := latestOutcomes[customer.customerID]; found {
			responses[index].LatestPolicyOutcome = &PolicyOutcomeResponse{
				Outcome:   policies.Outcome(latest.Outcome),
				PolicyID:  identifiers.Encode(identifiers.PrefixPolicy, *latest.MatchedPolicyID),
				DecidedAt: latest.CreatedAt.UTC(),
			}
		}
	}
	return responses, nil
}

func (service *OverviewService) attention(ctx context.Context, environment httpapi.Environment, now time.Time, planMargins []PlanMarginResponse, uncostedCount int64) (AttentionResponse, error) {
	attention := AttentionResponse{UncostedRequests: uncostedCount}
	for _, planMargin := range planMargins {
		if planMargin.BelowTarget {
			attention.PlansBelowTarget++
		}
	}
	current, err := service.signals.activeCustomers(ctx, environment, now)
	if err != nil {
		return AttentionResponse{}, err
	}
	for _, customer := range current {
		if customer.signals.Pace > AttentionPaceThreshold {
			attention.CustomersAbovePace++
		}
	}
	keys := make([]string, droppedReportsDays)
	for index := range keys {
		keys[index] = decisions.DroppedReportsKey(service.cache, environment, startOfDay(now).AddDate(0, 0, -index))
	}
	counts, err := service.cache.Redis().MGet(ctx, keys...).Result()
	if err != nil {
		return AttentionResponse{}, fmt.Errorf("read dropped report counters: %w", err)
	}
	for index, count := range counts {
		if count == nil {
			continue
		}
		text, isText := count.(string)
		if !isText {
			return AttentionResponse{}, fmt.Errorf("dropped report counter %s holds %T", keys[index], count)
		}
		dropped, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return AttentionResponse{}, fmt.Errorf("parse dropped report counter %s: %w", keys[index], err)
		}
		attention.DroppedReports += dropped
	}
	return attention, nil
}

func (service *OverviewService) policyChanges(ctx context.Context, environment httpapi.Environment) ([]PolicyChangeResponse, error) {
	rows, err := service.queries.ListRecentPolicyChanges(ctx, queries.ListRecentPolicyChangesParams{
		Environment: queries.Environment(environment),
		RowLimit:    policyChangeLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list recent policy changes: %w", err)
	}
	changes := make([]PolicyChangeResponse, len(rows))
	for index, row := range rows {
		change := PolicyChangeUpdated
		if row.Version == 1 {
			change = PolicyChangeCreated
		}
		changes[index] = PolicyChangeResponse{
			ID:        identifiers.Encode(identifiers.PrefixPolicy, row.PolicyID),
			Name:      row.Name,
			Status:    policies.Status(row.Status),
			Version:   int64(row.Version),
			Change:    change,
			CreatedAt: row.CreatedAt.UTC(),
			UpdatedAt: row.UpdatedAt.UTC(),
		}
	}
	return changes, nil
}

func (sum *totals) add(customer customerTotals) error {
	revenueNet, err := sum.revenueNet.Add(customer.revenueNet)
	if err != nil {
		return fmt.Errorf("sum revenue: %w", err)
	}
	cost, err := sum.cost.Add(customer.cost)
	if err != nil {
		return fmt.Errorf("sum cost: %w", err)
	}
	sum.customerCount++
	sum.revenueNet = revenueNet
	sum.cost = cost
	sum.uncostedCount += customer.uncostedCount
	return nil
}

func planMargins(planRows []queries.ListPlansRow, customers []customerTotals) ([]PlanMarginResponse, *string, error) {
	byPlan := make(map[uuid.UUID]*totals)
	var withoutPlan totals
	for _, customer := range customers {
		if customer.planID == nil {
			if err := withoutPlan.add(customer); err != nil {
				return nil, nil, err
			}
			continue
		}
		planSum, found := byPlan[*customer.planID]
		if !found {
			planSum = &totals{}
			byPlan[*customer.planID] = planSum
		}
		if err := planSum.add(customer); err != nil {
			return nil, nil, err
		}
	}
	margins := make([]PlanMarginResponse, 0, len(byPlan)+1)
	var weightedTargets, targetRevenue float64
	for _, plan := range planRows {
		planSum, found := byPlan[plan.PlanID]
		if !found {
			continue
		}
		mode := planMode(&plan)
		if mode == plans.ModeMarginTarget && planSum.revenueNet > 0 {
			weightedTargets += float64(plan.TargetMarginBasisPoints) / basisPointsPerUnit * float64(planSum.revenueNet)
			targetRevenue += float64(planSum.revenueNet)
		}
		margin := newPlanMarginResponse(*planSum)
		margin.PlanID, margin.Name, margin.TargetMargin = planFields(&plan)
		margin.Mode = &mode
		margin.BelowTarget = belowTarget(*planSum, &plan)
		margins = append(margins, margin)
	}
	if len(margins) != len(byPlan) {
		return nil, nil, fmt.Errorf("customers follow %d plans and %d of them are plans of the environment", len(byPlan), len(margins))
	}
	if withoutPlan.customerCount > 0 {
		margins = append(margins, newPlanMarginResponse(withoutPlan))
	}
	if targetRevenue == 0 {
		return margins, nil, nil
	}
	targetMargin := money.FormatSignal(weightedTargets / targetRevenue)
	return margins, &targetMargin, nil
}

func belowTarget(planSum totals, plan *queries.ListPlansRow) bool {
	if planSum.revenueNet <= 0 {
		return false
	}
	targetMargin := plan.TargetMarginBasisPoints
	if planMode(plan) == plans.ModeFixedAllowance {
		targetMargin = 0
	}
	keptShare := new(big.Int).Sub(big.NewInt(int64(planSum.revenueNet)), big.NewInt(int64(planSum.cost)))
	keptShare.Mul(keptShare, big.NewInt(basisPointsPerUnit))
	targetShare := new(big.Int).Mul(big.NewInt(int64(targetMargin)), big.NewInt(int64(planSum.revenueNet)))
	return keptShare.Cmp(targetShare) < 0
}

func marginOf(revenueNet money.Amount, cost money.Amount) (float64, bool) {
	if revenueNet <= 0 {
		return 0, false
	}
	return 1 - float64(cost)/float64(revenueNet), true
}

func formatMargin(revenueNet money.Amount, cost money.Amount) *string {
	margin, defined := marginOf(revenueNet, cost)
	if !defined {
		return nil
	}
	formatted := money.FormatSignal(margin)
	return &formatted
}

func startOfDay(moment time.Time) time.Time {
	year, month, dayOfMonth := moment.UTC().Date()
	return time.Date(year, month, dayOfMonth, 0, 0, 0, 0, time.UTC)
}

func newPlanMarginResponse(planSum totals) PlanMarginResponse {
	return PlanMarginResponse{
		CustomerCount: planSum.customerCount,
		Revenue:       money.FormatAmount(planSum.revenueNet),
		Cost:          money.FormatAmount(planSum.cost),
		Margin:        formatMargin(planSum.revenueNet, planSum.cost),
	}
}

func newDecisionCountsResponse(sums queries.SumDecisionsInPeriodsRow) DecisionCountsResponse {
	return DecisionCountsResponse{Allow: sums.AllowCount, Route: sums.RouteCount, Cap: sums.CapCount, Deny: sums.DenyCount}
}

func newCostAvoidedResponse(sums queries.SumDecisionsInPeriodsRow) (CostAvoidedResponse, error) {
	denied, routed, capped := money.Amount(sums.DeniedNanos), money.Amount(sums.RoutedNanos), money.Amount(sums.CappedNanos)
	total, err := money.Sum([]money.Amount{denied, routed, capped})
	if err != nil {
		return CostAvoidedResponse{}, fmt.Errorf("sum cost avoided: %w", err)
	}
	return CostAvoidedResponse{
		Total:                money.FormatAmount(total),
		Denied:               money.FormatAmount(denied),
		Routed:               money.FormatAmount(routed),
		Capped:               money.FormatAmount(capped),
		ChangedDecisionCount: sums.RouteCount + sums.CapCount + sums.DenyCount,
	}, nil
}
