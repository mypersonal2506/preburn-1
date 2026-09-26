package customerstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/customerstate/queries"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/signals"
)

// Loader reads customer states from Postgres. Create one with NewLoader. It
// is safe for concurrent use.
type Loader struct {
	queries *queries.Queries
}

// NewLoader returns a Loader that reads from pool.
func NewLoader(pool *pgxpool.Pool) *Loader {
	return &Loader{queries: queries.New(pool)}
}

// Load returns the state at now of the customer with customerID in
// environment, or httpapi.ErrNotFound when there is none.
func (loader *Loader) Load(ctx context.Context, environment httpapi.Environment, customerID uuid.UUID, now time.Time) (signals.CustomerState, error) {
	plan, err := loader.queries.SelectCustomerPlan(ctx, queries.SelectCustomerPlanParams{
		Environment: queries.Environment(environment),
		CustomerID:  customerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return signals.CustomerState{}, httpapi.ErrNotFound
	}
	if err != nil {
		return signals.CustomerState{}, fmt.Errorf("select plan of customer %s: %w", customerID, err)
	}
	subscriptionRevenuePeriod, err := loader.subscriptionRevenuePeriod(ctx, environment, customerID, now)
	if err != nil {
		return signals.CustomerState{}, err
	}
	period := signals.ResolvePeriod(now, nil, subscriptionRevenuePeriod)
	netRevenue, err := loader.periodRevenue(ctx, environment, customerID, period)
	if err != nil {
		return signals.CustomerState{}, err
	}
	return newCustomerState(customerID, plan.PlanID, plan.TargetMarginBasisPoints, plan.AllowanceNanos, plan.HoldTimes, period, netRevenue)
}

// LoadAll returns the state at now of every active customer in environment,
// ordered by customer id. It reads every customer in three queries, with no
// limit on their number.
func (loader *Loader) LoadAll(ctx context.Context, environment httpapi.Environment, now time.Time) ([]signals.CustomerState, error) {
	activePlans, err := loader.queries.ListActiveCustomerPlans(ctx, queries.Environment(environment))
	if err != nil {
		return nil, fmt.Errorf("list active customer plans: %w", err)
	}
	customerPlans := make([]queries.ListCustomerPlansRow, len(activePlans))
	for index, plan := range activePlans {
		customerPlans[index] = queries.ListCustomerPlansRow(plan)
	}
	return loader.states(ctx, environment, customerPlans, now)
}

// LoadMany returns the state at now of each customer of customerIDs that
// exists in environment, whatever its status, ordered by customer id. It
// reads them in three queries, so a page of customers costs the same
// queries as one customer.
func (loader *Loader) LoadMany(ctx context.Context, environment httpapi.Environment, customerIDs []uuid.UUID, now time.Time) ([]signals.CustomerState, error) {
	customerPlans, err := loader.queries.ListCustomerPlans(ctx, queries.ListCustomerPlansParams{
		Environment: queries.Environment(environment),
		CustomerIds: customerIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list customer plans: %w", err)
	}
	return loader.states(ctx, environment, customerPlans, now)
}

func (loader *Loader) states(ctx context.Context, environment httpapi.Environment, customerPlans []queries.ListCustomerPlansRow, now time.Time) ([]signals.CustomerState, error) {
	customerIDs := make([]uuid.UUID, len(customerPlans))
	for index, plan := range customerPlans {
		customerIDs[index] = plan.CustomerID
	}
	subscriptionRows, err := loader.queries.ListSubscriptionRevenuePeriods(ctx, queries.ListSubscriptionRevenuePeriodsParams{
		Environment: queries.Environment(environment),
		CustomerIds: customerIDs,
		Now:         now,
	})
	if err != nil {
		return nil, fmt.Errorf("list subscription revenue periods: %w", err)
	}
	subscriptionRevenuePeriods := make(map[uuid.UUID]signals.Period, len(subscriptionRows))
	for _, row := range subscriptionRows {
		subscriptionRevenuePeriods[row.CustomerID] = signals.Period{Start: row.PeriodStart.UTC(), End: row.PeriodEnd.UTC()}
	}
	periods := make([]signals.Period, len(customerPlans))
	periodStarts := make([]time.Time, len(customerPlans))
	for index, plan := range customerPlans {
		var subscriptionRevenuePeriod *signals.Period
		if period, found := subscriptionRevenuePeriods[plan.CustomerID]; found {
			subscriptionRevenuePeriod = &period
		}
		periods[index] = signals.ResolvePeriod(now, nil, subscriptionRevenuePeriod)
		periodStarts[index] = periods[index].Start
	}
	revenueRows, err := loader.queries.ListPeriodRevenues(ctx, queries.ListPeriodRevenuesParams{
		Environment:  queries.Environment(environment),
		CustomerIds:  customerIDs,
		PeriodStarts: periodStarts,
	})
	if err != nil {
		return nil, fmt.Errorf("list period revenues: %w", err)
	}
	netRevenues := make(map[uuid.UUID]money.Amount, len(revenueRows))
	for _, row := range revenueRows {
		netRevenues[row.CustomerID] = row.RevenueNetNanos
	}
	states := make([]signals.CustomerState, len(customerPlans))
	for index, plan := range customerPlans {
		states[index], err = newCustomerState(plan.CustomerID, plan.PlanID, plan.TargetMarginBasisPoints, plan.AllowanceNanos, plan.HoldTimes, periods[index], netRevenues[plan.CustomerID])
		if err != nil {
			return nil, err
		}
	}
	return states, nil
}

func (loader *Loader) subscriptionRevenuePeriod(ctx context.Context, environment httpapi.Environment, customerID uuid.UUID, now time.Time) (*signals.Period, error) {
	row, err := loader.queries.SelectSubscriptionRevenuePeriod(ctx, queries.SelectSubscriptionRevenuePeriodParams{
		Environment: queries.Environment(environment),
		CustomerID:  customerID,
		Now:         now,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select subscription revenue period of customer %s: %w", customerID, err)
	}
	return &signals.Period{Start: row.PeriodStart.UTC(), End: row.PeriodEnd.UTC()}, nil
}

func (loader *Loader) periodRevenue(ctx context.Context, environment httpapi.Environment, customerID uuid.UUID, period signals.Period) (money.Amount, error) {
	netRevenue, err := loader.queries.SelectPeriodRevenue(ctx, queries.SelectPeriodRevenueParams{
		Environment: queries.Environment(environment),
		CustomerID:  customerID,
		PeriodStart: period.Start,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("select period revenue of customer %s: %w", customerID, err)
	}
	return netRevenue, nil
}

func newCustomerState(customerID uuid.UUID, planID *uuid.UUID, targetMargin *money.BasisPoints, allowance *money.Amount, encodedHoldTimes []byte, period signals.Period, netRevenue money.Amount) (signals.CustomerState, error) {
	var holdTimes map[string]int
	if err := json.Unmarshal(encodedHoldTimes, &holdTimes); err != nil {
		return signals.CustomerState{}, fmt.Errorf("decode hold times of customer %s: %w", customerID, err)
	}
	state := signals.CustomerState{
		CustomerID: customerID,
		PlanID:     planID,
		PlanMode:   plans.ModeFixedAllowance,
		HoldTimes:  holdTimes,
		Period:     period,
		NetRevenue: netRevenue,
	}
	if planID == nil {
		return state, nil
	}
	state.TargetMargin = *targetMargin
	if allowance == nil {
		state.PlanMode = plans.ModeMarginTarget
		return state, nil
	}
	state.Allowance = *allowance
	return state, nil
}
