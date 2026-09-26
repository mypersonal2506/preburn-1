package policies

import (
	"bytes"
	"cmp"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/signals"
)

// Reason is why a check decided its outcome. Resolve returns
// ReasonNoPolicyMatched, ReasonPolicyMatched or ReasonCapNotApplicable, and
// the check sets the others once it prices and reserves the request.
type Reason string

const (
	// ReasonNoPolicyMatched means no active policy in scope had conditions
	// that held, so the check allows.
	ReasonNoPolicyMatched Reason = "no_policy_matched"
	// ReasonPolicyMatched means the matched policy decided the outcome.
	ReasonPolicyMatched Reason = "policy_matched"
	// ReasonHardLimitReached means the reservation found the cap limit of the
	// period reached, so the check denies.
	ReasonHardLimitReached Reason = "hard_limit_reached"
	// ReasonRouteChainExhausted means no route chain target could be priced
	// and fit the hard ceilings, so the check denies.
	ReasonRouteChainExhausted Reason = "route_chain_exhausted"
	// ReasonUncostedAllowed means the request could not be priced and
	// on_uncosted allows it.
	ReasonUncostedAllowed Reason = "uncosted_allowed"
	// ReasonUncostedDenied means the request could not be priced and
	// on_uncosted denies it.
	ReasonUncostedDenied Reason = "uncosted_denied"
	// ReasonCapNotApplicable means the matched cap kept no override the
	// requested model supports and has no limit, so the check allows.
	ReasonCapNotApplicable Reason = "cap_not_applicable"
)

// ResolutionRequest is what Resolve needs to know about a check besides its
// signals.
type ResolutionRequest struct {
	// CustomerID is the checked customer. LevelCustomer policies apply when
	// their Scope.CustomerID equals it.
	CustomerID uuid.UUID
	// PlanID is the customer's plan, else the default plan of the
	// environment, or nil when there is neither. LevelPlan policies apply
	// when their Scope.PlanID equals it.
	PlanID *uuid.UUID
	// Feature is the checked feature. Policies apply when their Feature is
	// nil or equal to it.
	Feature string
	// ModelParameters is the parameter mapping of the requested model, nil
	// or empty when it has none. It filters the overrides of a cap.
	ModelParameters map[string]catalogfiles.Parameter
}

// Resolution is the policy decision for a check before pricing and
// reservation.
type Resolution struct {
	// Outcome is the decided outcome.
	Outcome Outcome
	// Reason is ReasonPolicyMatched, ReasonNoPolicyMatched or
	// ReasonCapNotApplicable.
	Reason Reason
	// MatchedPolicy is the policy that decided, with its ID and Version, or
	// nil when none matched.
	MatchedPolicy *Policy
	// RouteChain is the matched route chain for OutcomeRoute, and nil
	// otherwise.
	RouteChain []RouteTarget
	// Overrides are the matched route's overrides, or the matched cap's
	// overrides that the requested model supports. It is empty, never nil,
	// when there are none.
	Overrides map[string]OverrideValue
	// Limit is the matched cap's limit, or nil.
	Limit *Limit
	// Enforcement is the matched policy's enforcement, EnforcementSoft when
	// none matched.
	Enforcement Enforcement
	// OnUncosted is the outcome for a request that cannot be priced: the
	// matched policy's, OutcomeAllow when none matched.
	OnUncosted Outcome
	// FallbackOutcome is the matched policy's OnUnreachable, OutcomeAllow
	// when none matched.
	FallbackOutcome Outcome
	// HardAllowanceCondition reports that the matched policy is hard and a
	// condition on allowance_remaining held as part of its match, so the
	// reservation must re-check the allowance atomically.
	HardAllowanceCondition bool
}

type evaluation struct {
	matched          bool
	allowanceMatched bool
}

var (
	levelsByPrecedence        = []Level{LevelEveryone, LevelPlan, LevelCustomer}
	outcomesByRestrictiveness = []Outcome{OutcomeAllow, OutcomeRoute, OutcomeCap, OutcomeDeny}
)

// Resolve picks the policy that decides a check from policies, the policies
// of the check's environment:
//  1. Candidates are active policies whose Feature is nil or the request's
//     feature, and whose scope is everyone, the request's plan or the
//     request's customer.
//  2. A candidate matches when its When group holds for signalValues. A
//     condition on a signal without a value, such as request_estimated_cost
//     for an uncosted request, does not hold.
//  3. The customer level beats the plan level, which beats everyone.
//  4. Within a level deny beats cap, cap beats route and route beats allow,
//     then the latest UpdatedAt and then the lowest ID win.
//
// Without a match the outcome is allow with ReasonNoPolicyMatched. A matched
// cap keeps the overrides ApplicableOverrides returns for
// request.ModelParameters, and resolves to allow with ReasonCapNotApplicable
// when it keeps none and has no limit.
func Resolve(policies []Policy, request ResolutionRequest, signalValues signals.Signals) Resolution {
	var chosen *Policy
	chosenAllowanceMatched := false
	for index := range policies {
		candidate := &policies[index]
		if !candidate.appliesTo(request) {
			continue
		}
		result := candidate.When.evaluate(signalValues)
		if result.matched && (chosen == nil || comparePrecedence(candidate, chosen) > 0) {
			chosen = candidate
			chosenAllowanceMatched = result.allowanceMatched
		}
	}
	if chosen == nil {
		return Resolution{
			Outcome:         OutcomeAllow,
			Reason:          ReasonNoPolicyMatched,
			Overrides:       map[string]OverrideValue{},
			Enforcement:     EnforcementSoft,
			OnUncosted:      OutcomeAllow,
			FallbackOutcome: OutcomeAllow,
		}
	}
	return matchedResolution(*chosen, chosenAllowanceMatched, request.ModelParameters)
}

// ApplicableOverrides returns the overrides whose key modelParameters lists
// with a parameter that allows the override's value. It returns an empty map,
// never nil, when none applies.
func ApplicableOverrides(overrides map[string]OverrideValue, modelParameters map[string]catalogfiles.Parameter) map[string]OverrideValue {
	applicable := map[string]OverrideValue{}
	for key, override := range overrides {
		if parameter, found := modelParameters[key]; found && parameterAllows(parameter, override) {
			applicable[key] = override
		}
	}
	return applicable
}

func matchedResolution(policy Policy, allowanceMatched bool, modelParameters map[string]catalogfiles.Parameter) Resolution {
	resolution := Resolution{
		Outcome:                policy.Action.Outcome,
		Reason:                 ReasonPolicyMatched,
		MatchedPolicy:          &policy,
		Overrides:              map[string]OverrideValue{},
		Enforcement:            policy.Enforcement,
		OnUncosted:             policy.OnUncosted,
		FallbackOutcome:        policy.OnUnreachable,
		HardAllowanceCondition: policy.Enforcement == EnforcementHard && allowanceMatched,
	}
	switch policy.Action.Outcome {
	case OutcomeRoute:
		resolution.RouteChain = policy.Action.RouteChain
		maps.Copy(resolution.Overrides, policy.Action.Overrides)
	case OutcomeCap:
		resolution.Overrides = ApplicableOverrides(policy.Action.Overrides, modelParameters)
		resolution.Limit = policy.Action.Limit
		if len(resolution.Overrides) == 0 && resolution.Limit == nil {
			resolution.Outcome = OutcomeAllow
			resolution.Reason = ReasonCapNotApplicable
		}
	case OutcomeAllow, OutcomeDeny:
	}
	return resolution
}

func (policy Policy) appliesTo(request ResolutionRequest) bool {
	if policy.Status != StatusActive || (policy.Feature != nil && *policy.Feature != request.Feature) {
		return false
	}
	switch policy.Scope.Level {
	case LevelEveryone:
		return true
	case LevelPlan:
		return request.PlanID != nil && *policy.Scope.PlanID == *request.PlanID
	case LevelCustomer:
		return *policy.Scope.CustomerID == request.CustomerID
	}
	return false
}

func comparePrecedence(first *Policy, second *Policy) int {
	return cmp.Or(
		cmp.Compare(slices.Index(levelsByPrecedence, first.Scope.Level), slices.Index(levelsByPrecedence, second.Scope.Level)),
		cmp.Compare(slices.Index(outcomesByRestrictiveness, first.Action.Outcome), slices.Index(outcomesByRestrictiveness, second.Action.Outcome)),
		first.UpdatedAt.Compare(second.UpdatedAt),
		-bytes.Compare(first.ID[:], second.ID[:]),
	)
}

func (group ConditionGroup) evaluate(signalValues signals.Signals) evaluation {
	matchedMembers := 0
	allowanceMatched := false
	for _, member := range group.Members {
		result := member.evaluate(signalValues)
		if result.matched {
			matchedMembers++
		}
		allowanceMatched = allowanceMatched || result.allowanceMatched
	}
	matched := false
	switch group.Quantifier {
	case QuantifierAll:
		matched = matchedMembers == len(group.Members)
	case QuantifierAny:
		matched = matchedMembers > 0
	}
	return evaluation{matched: matched, allowanceMatched: matched && allowanceMatched}
}

func (condition Condition) evaluate(signalValues signals.Signals) evaluation {
	value, present := signalValues.Value(condition.Signal)
	if !present {
		return evaluation{}
	}
	matched := false
	switch value.Kind {
	case signals.KindMoney:
		matched = compare(condition.Operator, value.Amount, condition.Value.Amount)
	case signals.KindRatio:
		matched = compare(condition.Operator, value.Ratio, condition.Value.Ratio)
	case signals.KindCount:
		matched = compare(condition.Operator, value.Count, condition.Value.Count)
	}
	return evaluation{matched: matched, allowanceMatched: matched && condition.Signal == signals.NameAllowanceRemaining}
}

func compare[Number cmp.Ordered](operator Operator, signal Number, threshold Number) bool {
	switch operator {
	case OperatorLessThan:
		return signal < threshold
	case OperatorLessThanOrEqual:
		return signal <= threshold
	case OperatorGreaterThan:
		return signal > threshold
	case OperatorGreaterThanOrEqual:
		return signal >= threshold
	case OperatorEqual:
		return signal == threshold
	case OperatorNotEqual:
		return signal != threshold
	}
	return false
}
