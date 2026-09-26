package signals

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/plans"
)

const (
	// MinimumElapsedFraction is the floor of the elapsed fraction, so pace
	// and projected margin stay finite at the start of a period.
	MinimumElapsedFraction = 0.01

	basisPointsPerUnit = 10_000
)

// CustomerState is what the signals of a customer depend on besides the
// period counter: the customer's effective plan, current period and the net
// revenue attributed to that period.
type CustomerState struct {
	// CustomerID is the customer's UUID.
	CustomerID uuid.UUID
	// PlanID is the customer's plan, else the default plan of its
	// environment, or nil when there is neither.
	PlanID *uuid.UUID
	// PlanMode tells whether TargetMargin or Allowance sets the cost
	// allowance. A customer without a plan is plans.ModeFixedAllowance with
	// an Allowance of 0.
	PlanMode plans.Mode
	// TargetMargin is the plan's target margin, which sets the cost allowance
	// in plans.ModeMarginTarget.
	TargetMargin money.BasisPoints
	// Allowance is the plan's cost allowance per period in
	// plans.ModeFixedAllowance, and 0 in plans.ModeMarginTarget.
	Allowance money.Amount
	// HoldTimes maps a feature to the seconds a check reserves cost for it
	// under the plan. It is empty, never nil, when the plan has none or the
	// customer has no plan.
	HoldTimes map[string]int
	// Period is the customer's current period.
	Period Period
	// NetRevenue is the revenue attributed to Period: subscription plus
	// adjustment minus stripe_fee, refund and credit_note. It can be
	// negative.
	NetRevenue money.Amount
}

// CounterSnapshot is the Redis counter of a customer period at one moment.
type CounterSnapshot struct {
	// Settled is the settled cost of the period.
	Settled money.Amount
	// Reserved is the cost held by open reservations.
	Reserved money.Amount
	// Count is the number of decisions in the period.
	Count int64
	// Features holds the same values per feature.
	Features map[string]FeatureCounter
}

// FeatureCounter is the part of a CounterSnapshot for one feature.
type FeatureCounter struct {
	// Settled is the settled cost of the feature in the period.
	Settled money.Amount
	// Reserved is the cost held by open reservations of the feature.
	Reserved money.Amount
	// Count is the number of decisions for the feature in the period.
	Count int64
}

// Signals are the margin signals of a customer period, with one field per
// signal.
type Signals struct {
	// PeriodRevenueNet is the net revenue attributed to the period.
	PeriodRevenueNet money.Amount
	// CostAllowance is the cost the plan allows in the period, never below 0.
	CostAllowance money.Amount
	// CostToDate is the settled cost of the period.
	CostToDate money.Amount
	// Reserved is the cost held by open reservations in the period.
	Reserved money.Amount
	// ElapsedFraction is the elapsed share of the period, at least
	// MinimumElapsedFraction.
	ElapsedFraction float64
	// AllowanceRemaining is CostAllowance minus CostToDate and Reserved. It
	// is negative once the period is over its allowance.
	AllowanceRemaining money.Amount
	// Pace is the share of the allowance spent divided by ElapsedFraction.
	// It is +Inf for cost against a zero allowance, and 0 without cost.
	Pace float64
	// ProjectedMargin is the margin the period ends with if cost keeps its
	// pace. It is -Inf for cost without positive net revenue, and 0 without
	// cost.
	ProjectedMargin float64
	// RequestEstimatedCost is the rated cost of the checked request's usage
	// estimate, or nil when there is no request or its estimate is uncosted.
	RequestEstimatedCost *money.Amount
	// PeriodDecisionCount is the number of decisions in the period.
	PeriodDecisionCount int64
	// Features holds the per-feature values of the period counter. Compute
	// never leaves it nil.
	Features map[string]FeatureSignals
}

// FeatureSignals are the signals of one feature in a customer period.
type FeatureSignals struct {
	// CostToDate is the settled cost of the feature in the period.
	CostToDate money.Amount
	// Reserved is the cost held by open reservations of the feature.
	Reserved money.Amount
	// DecisionCount is the number of decisions for the feature in the
	// period.
	DecisionCount int64
}

// Compute returns the signals of state's period at now from its counter and
// the rated cost of the checked request's usage estimate, which is nil when
// there is no request or the estimate is uncosted. It returns
// money.ErrOverflow when AllowanceRemaining does not fit int64 and an error
// for a PlanMode other than the two of package plans.
func Compute(state CustomerState, counter CounterSnapshot, requestEstimatedCost *money.Amount, now time.Time) (Signals, error) {
	costAllowance, err := allowance(state)
	if err != nil {
		return Signals{}, err
	}
	allowanceRemaining, err := costAllowance.Subtract(counter.Settled)
	if err != nil {
		return Signals{}, fmt.Errorf("allowance remaining of customer %s: %w", state.CustomerID, err)
	}
	allowanceRemaining, err = allowanceRemaining.Subtract(counter.Reserved)
	if err != nil {
		return Signals{}, fmt.Errorf("allowance remaining of customer %s: %w", state.CustomerID, err)
	}
	elapsedFraction := max(float64(now.Sub(state.Period.Start))/float64(state.Period.End.Sub(state.Period.Start)), MinimumElapsedFraction)
	computed := Signals{
		PeriodRevenueNet:    state.NetRevenue,
		CostAllowance:       costAllowance,
		CostToDate:          counter.Settled,
		Reserved:            counter.Reserved,
		ElapsedFraction:     elapsedFraction,
		AllowanceRemaining:  allowanceRemaining,
		Pace:                pace(counter.Settled, costAllowance, elapsedFraction),
		ProjectedMargin:     projectedMargin(counter.Settled, state.NetRevenue, elapsedFraction),
		PeriodDecisionCount: counter.Count,
		Features:            make(map[string]FeatureSignals, len(counter.Features)),
	}
	if requestEstimatedCost != nil {
		estimate := *requestEstimatedCost
		computed.RequestEstimatedCost = &estimate
	}
	for feature, featureCounter := range counter.Features {
		computed.Features[feature] = FeatureSignals{
			CostToDate:    featureCounter.Settled,
			Reserved:      featureCounter.Reserved,
			DecisionCount: featureCounter.Count,
		}
	}
	return computed, nil
}

func allowance(state CustomerState) (money.Amount, error) {
	switch state.PlanMode {
	case plans.ModeFixedAllowance:
		return state.Allowance, nil
	case plans.ModeMarginTarget:
		if state.NetRevenue <= 0 {
			return 0, nil
		}
		keptShare := money.Amount(basisPointsPerUnit - state.TargetMargin)
		return state.NetRevenue/basisPointsPerUnit*keptShare + state.NetRevenue%basisPointsPerUnit*keptShare/basisPointsPerUnit, nil
	}
	return 0, fmt.Errorf("unknown plan mode %q of customer %s", state.PlanMode, state.CustomerID)
}

func pace(costToDate money.Amount, costAllowance money.Amount, elapsedFraction float64) float64 {
	switch {
	case costAllowance > 0:
		return float64(costToDate) / float64(costAllowance) / elapsedFraction
	case costToDate > 0:
		return math.Inf(1)
	}
	return 0
}

func projectedMargin(costToDate money.Amount, netRevenue money.Amount, elapsedFraction float64) float64 {
	switch {
	case netRevenue > 0:
		return 1 - float64(costToDate)/elapsedFraction/float64(netRevenue)
	case costToDate > 0:
		return math.Inf(-1)
	}
	return 0
}
