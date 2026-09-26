package policies

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/signals"
)

const (
	nameMaximumLength            = 120
	conditionGroupMaximumDepth   = 3
	routeChainMaximumLength      = 5
	routeTargetNameMaximumLength = 200

	groupRule              = "expected an object with exactly one of all or any"
	memberRule             = "expected a condition or a condition group"
	emptyAnyRule           = "expected at least one member in an any group"
	operatorRule           = "expected lt, lte, gt, gte, eq or ne"
	moneyValueRule         = "expected an amount string in USD with at most 9 decimals, such as 2.50"
	ratioValueRule         = "expected a ratio string with at most 4 decimals, such as 0.40, and not inf"
	countValueRule         = "expected a whole number string of 0 or more, such as 100"
	levelRule              = "expected everyone, plan or customer"
	planIDRequiredRule     = "expected a plan id for level plan"
	planIDRejectedRule     = "expected no plan id outside level plan"
	customerIDRequiredRule = "expected a customer id for level customer"
	customerIDRejectedRule = "expected no customer id outside level customer"
	customerNotFoundRule   = "expected the id of a customer in the environment"
	outcomeRule            = "expected allow, route, cap or deny"
	routeChainRejectedRule = "expected no route chain outside outcome route"
	overridesRejectedRule  = "expected no overrides outside outcomes route and cap"
	routeOverrideKeyRule   = "expected a parameter that every route target supports"
	routeOverrideValueRule = "expected a value that every route target allows"
	capOverrideKeyRule     = "expected a parameter of a model in the parameter mappings"
	capOverrideValueRule   = "expected a value that a model with this parameter allows"
	limitRejectedRule      = "expected no limit outside outcome cap"
	limitKindRule          = "expected count or amount"
	amountLimitRule        = "expected an amount string in USD of 0 or more with at most 9 decimals, such as 2.50"
	capRule                = "expected overrides, a limit or both for outcome cap"
	enforcementRule        = "expected soft or hard"
	fallbackOutcomeRule    = "expected allow or deny"
	statusRule             = "expected active, disabled or archived"
)

// ValidationContext supplies the facts outside a policy document that
// Validate checks against.
type ValidationContext struct {
	// CustomerExists reports whether the policy's environment has a customer
	// with customerID. Validate calls it for a LevelCustomer policy only.
	CustomerExists func(customerID uuid.UUID) bool
	// ParameterMappings maps each provider model to its overridable
	// parameters by name, as catalogfiles.Catalog.ParameterMappings does.
	ParameterMappings map[catalogfiles.ModelKey]map[string]catalogfiles.Parameter
}

type documentValidator struct {
	fieldErrors       fieldErrorList
	validationContext ValidationContext
}

var (
	nameRule             = fmt.Sprintf("expected 1 to %d characters without control characters", nameMaximumLength)
	featureRule          = "expected null or a feature name matching " + plans.FeaturePattern.String()
	signalRule           = "expected one of " + strings.Join(signalNames(), ", ")
	routeChainLengthRule = fmt.Sprintf("expected 1 to %d route targets for outcome route", routeChainMaximumLength)
	routeTargetNameRule  = fmt.Sprintf("expected 1 to %d characters without control characters", routeTargetNameMaximumLength)
	depthRule            = fmt.Sprintf("expected condition groups nested at most %d deep", conditionGroupMaximumDepth)
	signalValueRules     = map[signals.Kind]string{
		signals.KindMoney: moneyValueRule,
		signals.KindRatio: ratioValueRule,
		signals.KindCount: countValueRule,
	}
)

// Validate checks policy against every rule of a policy document and
// returns one FieldError per failing JSON path, or none:
//   - name has 1 to 120 characters without control characters.
//   - level is everyone, plan or customer. plan_id is required for plan and
//     forbidden otherwise, customer_id is required for customer and forbidden
//     otherwise, and the customer must exist according to validationContext.
//     Whether the plan exists is the caller's check.
//   - feature is null or matches plans.FeaturePattern.
//   - when nests condition groups at most 3 deep counting itself, has no
//     empty any group, and compares known signals with known operators and
//     values of the signal's kind. Ratio values are finite and counts are not
//     negative.
//   - route needs 1 to 5 route targets, and each override key and value must
//     be allowed by the parameter mapping of every target.
//   - cap needs overrides, a limit or both. Each override key and value must
//     be allowed by the parameter mapping of at least one model.
//   - Only route takes a route chain, only route and cap take overrides and
//     only cap takes a limit, of kind count or amount and not negative.
//   - enforcement is soft or hard, on_unreachable and on_uncosted are allow
//     or deny, and status is active, disabled or archived.
//
// Validate reads only policy.Document. Messages never hold the rejected
// values.
func Validate(policy Policy, validationContext ValidationContext) []FieldError {
	validator := &documentValidator{validationContext: validationContext}
	validator.fieldErrors.require(validText(policy.Name, nameMaximumLength), namePath, nameRule)
	validator.scope(policy.Scope)
	validator.fieldErrors.require(policy.Feature == nil || plans.FeaturePattern.MatchString(*policy.Feature), featurePath, featureRule)
	validator.group(whenPath, policy.When, 1)
	validator.action(policy.Action)
	validator.fieldErrors.require(policy.Enforcement.known(), enforcementPath, enforcementRule)
	validator.fieldErrors.require(policy.OnUnreachable.fallback(), onUnreachablePath, fallbackOutcomeRule)
	validator.fieldErrors.require(policy.OnUncosted.fallback(), onUncostedPath, fallbackOutcomeRule)
	validator.fieldErrors.require(policy.Status.known(), statusPath, statusRule)
	return validator.fieldErrors
}

func (validator *documentValidator) scope(scope Scope) {
	switch scope.Level {
	case LevelEveryone:
		validator.fieldErrors.require(scope.PlanID == nil, planIDPath, planIDRejectedRule)
		validator.fieldErrors.require(scope.CustomerID == nil, customerIDPath, customerIDRejectedRule)
	case LevelPlan:
		validator.fieldErrors.require(scope.PlanID != nil, planIDPath, planIDRequiredRule)
		validator.fieldErrors.require(scope.CustomerID == nil, customerIDPath, customerIDRejectedRule)
	case LevelCustomer:
		validator.fieldErrors.require(scope.PlanID == nil, planIDPath, planIDRejectedRule)
		validator.customerIdentifier(scope.CustomerID)
	default:
		validator.fieldErrors.report(levelPath, levelRule)
	}
}

func (validator *documentValidator) customerIdentifier(customerID *uuid.UUID) {
	if customerID == nil {
		validator.fieldErrors.report(customerIDPath, customerIDRequiredRule)
		return
	}
	validator.fieldErrors.require(validator.validationContext.CustomerExists(*customerID), customerIDPath, customerNotFoundRule)
}

func (validator *documentValidator) group(path string, group ConditionGroup, depth int) {
	if depth > conditionGroupMaximumDepth {
		validator.fieldErrors.report(path, depthRule)
		return
	}
	membersPath := joinPath(path, string(group.Quantifier))
	switch group.Quantifier {
	case QuantifierAll:
	case QuantifierAny:
		validator.fieldErrors.require(len(group.Members) > 0, membersPath, emptyAnyRule)
	default:
		validator.fieldErrors.report(path, groupRule)
		return
	}
	for index, member := range group.Members {
		memberPath := indexPath(membersPath, index)
		switch typed := member.(type) {
		case Condition:
			validator.condition(memberPath, typed)
		case ConditionGroup:
			validator.group(memberPath, typed, depth+1)
		default:
			validator.fieldErrors.report(memberPath, memberRule)
		}
	}
}

func (validator *documentValidator) condition(path string, condition Condition) {
	kind, known := condition.Signal.Kind()
	validator.fieldErrors.require(known, joinPath(path, "signal"), signalRule)
	validator.fieldErrors.require(condition.Operator.known(), joinPath(path, "operator"), operatorRule)
	if known {
		validator.fieldErrors.require(validSignalValue(condition.Value, kind), joinPath(path, "value"), signalValueRules[kind])
	}
}

func (validator *documentValidator) action(action Action) {
	switch action.Outcome {
	case OutcomeAllow, OutcomeDeny:
		validator.fieldErrors.require(len(action.RouteChain) == 0, routeChainPath, routeChainRejectedRule)
		validator.fieldErrors.require(len(action.Overrides) == 0, overridesPath, overridesRejectedRule)
		validator.fieldErrors.require(action.Limit == nil, limitPath, limitRejectedRule)
	case OutcomeRoute:
		validator.routeChain(action.RouteChain)
		validator.routeOverrides(action.RouteChain, action.Overrides)
		validator.fieldErrors.require(action.Limit == nil, limitPath, limitRejectedRule)
	case OutcomeCap:
		validator.fieldErrors.require(len(action.RouteChain) == 0, routeChainPath, routeChainRejectedRule)
		validator.capOverrides(action.Overrides)
		if action.Limit != nil {
			validator.limit(*action.Limit)
		}
		validator.fieldErrors.require(len(action.Overrides) > 0 || action.Limit != nil, actionPath, capRule)
	default:
		validator.fieldErrors.report(outcomePath, outcomeRule)
	}
}

func (validator *documentValidator) routeChain(targets []RouteTarget) {
	validator.fieldErrors.require(len(targets) > 0 && len(targets) <= routeChainMaximumLength, routeChainPath, routeChainLengthRule)
	for index, target := range targets {
		targetPath := indexPath(routeChainPath, index)
		validator.fieldErrors.require(validText(target.Provider, routeTargetNameMaximumLength), joinPath(targetPath, "provider"), routeTargetNameRule)
		validator.fieldErrors.require(validText(target.Model, routeTargetNameMaximumLength), joinPath(targetPath, "model"), routeTargetNameRule)
	}
}

func (validator *documentValidator) routeOverrides(targets []RouteTarget, overrides map[string]OverrideValue) {
	for _, key := range slices.Sorted(maps.Keys(overrides)) {
		supported, allowed := true, true
		for _, target := range targets {
			parameters := validator.validationContext.ParameterMappings[catalogfiles.ModelKey{Provider: target.Provider, Model: target.Model}]
			parameter, found := parameters[key]
			supported = supported && found
			allowed = allowed && found && parameterAllows(parameter, overrides[key])
		}
		validator.overrideProblem(joinPath(overridesPath, key), supported, allowed, routeOverrideKeyRule, routeOverrideValueRule)
	}
}

func (validator *documentValidator) capOverrides(overrides map[string]OverrideValue) {
	for _, key := range slices.Sorted(maps.Keys(overrides)) {
		supported, allowed := false, false
		for _, parameters := range validator.validationContext.ParameterMappings {
			parameter, found := parameters[key]
			supported = supported || found
			allowed = allowed || (found && parameterAllows(parameter, overrides[key]))
		}
		validator.overrideProblem(joinPath(overridesPath, key), supported, allowed, capOverrideKeyRule, capOverrideValueRule)
	}
}

func (validator *documentValidator) overrideProblem(path string, supported bool, allowed bool, keyRule string, valueRule string) {
	switch {
	case !supported:
		validator.fieldErrors.report(path, keyRule)
	case !allowed:
		validator.fieldErrors.report(path, valueRule)
	}
}

func (validator *documentValidator) limit(limit Limit) {
	switch limit.Kind {
	case LimitKindCount:
		validator.fieldErrors.require(limit.Count >= 0, limitValuePath, countValueRule)
	case LimitKindAmount:
		validator.fieldErrors.require(limit.Amount >= 0, limitValuePath, amountLimitRule)
	default:
		validator.fieldErrors.report(limitKindPath, limitKindRule)
	}
}

func parameterAllows(parameter catalogfiles.Parameter, override OverrideValue) bool {
	if override.Type != parameter.ValueType {
		return false
	}
	switch parameter.ValueType {
	case catalogfiles.ValueTypeString:
		return slices.Contains(parameter.AllowedStrings, override.String)
	case catalogfiles.ValueTypeInteger:
		if parameter.Minimum != nil {
			return override.Integer >= *parameter.Minimum && override.Integer <= *parameter.Maximum
		}
		return slices.Contains(parameter.AllowedIntegers, override.Integer)
	case catalogfiles.ValueTypeBoolean:
		return true
	}
	return false
}

func validSignalValue(value signals.Value, kind signals.Kind) bool {
	if value.Kind != kind {
		return false
	}
	switch kind {
	case signals.KindMoney:
		return true
	case signals.KindRatio:
		return !math.IsInf(value.Ratio, 0) && !math.IsNaN(value.Ratio)
	case signals.KindCount:
		return value.Count >= 0
	}
	return false
}

func validText(text string, maximumLength int) bool {
	return strings.TrimSpace(text) != "" &&
		utf8.RuneCountInString(text) <= maximumLength &&
		utf8.ValidString(text) &&
		!strings.ContainsFunc(text, unicode.IsControl)
}

func signalNames() []string {
	names := make([]string, 0, len(signals.Names()))
	for _, name := range signals.Names() {
		names = append(names, string(name))
	}
	return names
}

func (operator Operator) known() bool {
	switch operator {
	case OperatorLessThan, OperatorLessThanOrEqual, OperatorGreaterThan, OperatorGreaterThanOrEqual, OperatorEqual, OperatorNotEqual:
		return true
	}
	return false
}

func (outcome Outcome) fallback() bool {
	return outcome == OutcomeAllow || outcome == OutcomeDeny
}

func (enforcement Enforcement) known() bool {
	return enforcement == EnforcementSoft || enforcement == EnforcementHard
}

func (status Status) known() bool {
	switch status {
	case StatusActive, StatusDisabled, StatusArchived:
		return true
	}
	return false
}
