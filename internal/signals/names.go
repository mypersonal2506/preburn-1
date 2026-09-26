package signals

import "github.com/preburn/preburn/internal/money"

// Name is the name of a signal that a policy condition compares.
type Name string

const (
	// NamePeriodRevenueNet is the net revenue attributed to the period.
	NamePeriodRevenueNet Name = "period_revenue_net"
	// NameCostToDate is the settled cost of the period.
	NameCostToDate Name = "cost_to_date"
	// NameElapsedFraction is the elapsed share of the period.
	NameElapsedFraction Name = "elapsed_fraction"
	// NameAllowanceRemaining is the cost allowance left after settled and
	// reserved cost.
	NameAllowanceRemaining Name = "allowance_remaining"
	// NamePace is the share of the allowance spent against the share of the
	// period elapsed.
	NamePace Name = "pace"
	// NameProjectedMargin is the margin the period ends with at the current
	// pace.
	NameProjectedMargin Name = "projected_margin"
	// NameRequestEstimatedCost is the rated cost of the checked request's
	// usage estimate.
	NameRequestEstimatedCost Name = "request_estimated_cost"
	// NamePeriodDecisionCount is the number of decisions in the period.
	NamePeriodDecisionCount Name = "period_decision_count"
)

// Kind is the value kind of a signal, which sets the type a policy condition
// compares it with.
type Kind string

const (
	// KindMoney signals are money.Amount values and compare exactly.
	KindMoney Kind = "money"
	// KindRatio signals are float64 values that can be +Inf or -Inf.
	KindRatio Kind = "ratio"
	// KindCount signals are int64 values.
	KindCount Kind = "count"
)

// Value is the value of one signal. Kind tells which of Amount, Ratio and
// Count holds it, and the other two are zero.
type Value struct {
	// Kind is the value kind of the signal.
	Kind Kind
	// Amount holds a KindMoney value.
	Amount money.Amount
	// Ratio holds a KindRatio value.
	Ratio float64
	// Count holds a KindCount value.
	Count int64
}

// Names returns the names of the eight signals policies compare, in a fixed
// order. The caller owns the returned slice.
func Names() []Name {
	return []Name{
		NamePeriodRevenueNet,
		NameCostToDate,
		NameElapsedFraction,
		NameAllowanceRemaining,
		NamePace,
		NameProjectedMargin,
		NameRequestEstimatedCost,
		NamePeriodDecisionCount,
	}
}

// Kind returns the value kind of the signal name and true, or false when name
// is not one of Names.
func (name Name) Kind() (Kind, bool) {
	switch name {
	case NamePeriodRevenueNet, NameCostToDate, NameAllowanceRemaining, NameRequestEstimatedCost:
		return KindMoney, true
	case NameElapsedFraction, NamePace, NameProjectedMargin:
		return KindRatio, true
	case NamePeriodDecisionCount:
		return KindCount, true
	}
	return "", false
}

// Value returns the value of the signal name and true. It returns false when
// the signal has no value, which is request_estimated_cost for a request
// without a costed estimate, and for a name that is not one of Names. A
// policy condition on a signal without a value does not match.
func (signals Signals) Value(name Name) (Value, bool) {
	switch name {
	case NamePeriodRevenueNet:
		return Value{Kind: KindMoney, Amount: signals.PeriodRevenueNet}, true
	case NameCostToDate:
		return Value{Kind: KindMoney, Amount: signals.CostToDate}, true
	case NameElapsedFraction:
		return Value{Kind: KindRatio, Ratio: signals.ElapsedFraction}, true
	case NameAllowanceRemaining:
		return Value{Kind: KindMoney, Amount: signals.AllowanceRemaining}, true
	case NamePace:
		return Value{Kind: KindRatio, Ratio: signals.Pace}, true
	case NameProjectedMargin:
		return Value{Kind: KindRatio, Ratio: signals.ProjectedMargin}, true
	case NameRequestEstimatedCost:
		if signals.RequestEstimatedCost == nil {
			return Value{}, false
		}
		return Value{Kind: KindMoney, Amount: *signals.RequestEstimatedCost}, true
	case NamePeriodDecisionCount:
		return Value{Kind: KindCount, Count: signals.PeriodDecisionCount}, true
	}
	return Value{}, false
}
