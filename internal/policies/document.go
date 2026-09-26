package policies

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/signals"
)

const (
	documentPath      = ""
	namePath          = "name"
	levelPath         = "level"
	planIDPath        = "plan_id"
	customerIDPath    = "customer_id"
	featurePath       = "feature"
	whenPath          = "when"
	actionPath        = "action"
	outcomePath       = "action.outcome"
	routeChainPath    = "action.route_chain"
	overridesPath     = "action.overrides"
	limitPath         = "action.limit"
	limitKindPath     = "action.limit.kind"
	limitValuePath    = "action.limit.value"
	enforcementPath   = "enforcement"
	onUnreachablePath = "on_unreachable"
	onUncostedPath    = "on_uncosted"
	statusPath        = "status"
)

// Level is the scope level of a policy.
type Level string

const (
	// LevelEveryone policies apply to every customer of the environment.
	LevelEveryone Level = "everyone"
	// LevelPlan policies apply to the customers whose plan, or default plan
	// when they have none, is Scope.PlanID.
	LevelPlan Level = "plan"
	// LevelCustomer policies apply to the customer Scope.CustomerID.
	LevelCustomer Level = "customer"
)

// Quantifier tells how many members of a condition group must hold.
type Quantifier string

const (
	// QuantifierAll groups hold when every member holds, so an empty one
	// always holds.
	QuantifierAll Quantifier = "all"
	// QuantifierAny groups hold when at least one member holds. They need at
	// least one member.
	QuantifierAny Quantifier = "any"
)

// Operator compares a signal with the value of a condition, signal first.
type Operator string

const (
	// OperatorLessThan holds when the signal is below the value.
	OperatorLessThan Operator = "lt"
	// OperatorLessThanOrEqual holds when the signal is at most the value.
	OperatorLessThanOrEqual Operator = "lte"
	// OperatorGreaterThan holds when the signal is above the value.
	OperatorGreaterThan Operator = "gt"
	// OperatorGreaterThanOrEqual holds when the signal is at least the value.
	OperatorGreaterThanOrEqual Operator = "gte"
	// OperatorEqual holds when the signal equals the value.
	OperatorEqual Operator = "eq"
	// OperatorNotEqual holds when the signal differs from the value.
	OperatorNotEqual Operator = "ne"
)

// Outcome is what a check decides for a request.
type Outcome string

const (
	// OutcomeAllow runs the request as asked.
	OutcomeAllow Outcome = "allow"
	// OutcomeRoute runs the request on the first route chain target that can
	// be priced and fits the hard ceilings.
	OutcomeRoute Outcome = "route"
	// OutcomeCap runs the request with overrides applied, within a per
	// period limit, or both.
	OutcomeCap Outcome = "cap"
	// OutcomeDeny rejects the request.
	OutcomeDeny Outcome = "deny"
)

// LimitKind is what the limit of a cap counts per period.
type LimitKind string

const (
	// LimitKindCount limits the number of decisions.
	LimitKindCount LimitKind = "count"
	// LimitKindAmount limits the cost.
	LimitKindAmount LimitKind = "amount"
)

// Enforcement tells how strictly a check holds a policy against concurrent
// requests.
type Enforcement string

const (
	// EnforcementSoft compares the signals once, when the check reads them.
	EnforcementSoft Enforcement = "soft"
	// EnforcementHard reserves the ceiling estimate and has the reservation
	// script re-check allowance_remaining and limits atomically.
	EnforcementHard Enforcement = "hard"
)

// Status is the state of a policy. Only active policies take part in
// resolution.
type Status string

const (
	// StatusActive policies take part in resolution.
	StatusActive Status = "active"
	// StatusDisabled policies are kept for later use and ignored.
	StatusDisabled Status = "disabled"
	// StatusArchived policies are retired and ignored.
	StatusArchived Status = "archived"
)

// Policy is a stored policy: a Document with its server assigned id,
// version and update time.
type Policy struct {
	// ID is the policy's UUID, exposed with the prefix pol.
	ID uuid.UUID
	// Document holds the fields the API client writes.
	Document
	// Version starts at 1 and increments on every update.
	Version int32
	// CreatedAt is when the policy was created, in UTC.
	CreatedAt time.Time
	// UpdatedAt is when the policy last changed, in UTC. The latest update
	// wins a tie in Resolve.
	UpdatedAt time.Time
}

// Document is the part of a policy that the API client writes.
type Document struct {
	// Name is 1 to 120 characters without control characters.
	Name string
	// Scope is who the policy applies to.
	Scope Scope
	// Feature is the feature the policy applies to, or nil for every
	// feature. It matches plans.FeaturePattern.
	Feature *string
	// When holds the conditions under which the policy matches.
	When ConditionGroup
	// Action is what the policy decides when it matches.
	Action Action
	// Enforcement is soft or hard.
	Enforcement Enforcement
	// OnUnreachable is OutcomeAllow or OutcomeDeny. Every check response
	// returns it as fallback_outcome, the outcome the SDK uses when the server
	// cannot be reached.
	OnUnreachable Outcome
	// OnUncosted is OutcomeAllow or OutcomeDeny, the outcome for a request
	// that cannot be priced.
	OnUncosted Outcome
	// Status tells whether the policy takes part in resolution.
	Status Status
}

// Scope is who a policy applies to.
type Scope struct {
	// Level is the scope level.
	Level Level
	// PlanID is the plan of a LevelPlan policy, and nil otherwise.
	PlanID *uuid.UUID
	// CustomerID is the customer of a LevelCustomer policy, and nil
	// otherwise.
	CustomerID *uuid.UUID
}

// GroupMember is a member of a ConditionGroup. Condition and ConditionGroup
// are its only implementations.
type GroupMember interface {
	evaluate(signalValues signals.Signals) evaluation
}

// ConditionGroup holds conditions and nested groups, at most 3 groups deep
// counting itself. Its JSON form is {"all": [...]} or {"any": [...]}.
type ConditionGroup struct {
	// Quantifier tells how many members must hold.
	Quantifier Quantifier
	// Members are Condition and ConditionGroup values.
	Members []GroupMember
}

// Condition compares one signal with a fixed value. Its JSON form is
// {"signal", "operator", "value"} with the value as a decimal string.
type Condition struct {
	// Signal is one of signals.Names.
	Signal signals.Name
	// Operator is how the signal compares with Value.
	Operator Operator
	// Value has the kind of Signal. A ratio value is finite and a count is
	// not negative.
	Value signals.Value
}

// Action is what a policy decides when it matches.
type Action struct {
	// Outcome is the decided outcome.
	Outcome Outcome
	// RouteChain lists 1 to 5 targets in order of preference for
	// OutcomeRoute, and is empty otherwise.
	RouteChain []RouteTarget
	// Overrides maps parameter names of the parameter mappings to their
	// values, for OutcomeRoute and OutcomeCap only.
	Overrides map[string]OverrideValue
	// Limit is the per period limit of an OutcomeCap, and nil otherwise.
	Limit *Limit
}

// RouteTarget is one provider model of a route chain.
type RouteTarget struct {
	// Provider is the provider name, such as fal_ai.
	Provider string `json:"provider"`
	// Model is the model name within the provider, such as
	// fal-ai/veo3.1/lite.
	Model string `json:"model"`
}

// OverrideValue is the value of one parameter override. Type tells which of
// String, Integer and Boolean holds it, and the other two are zero.
type OverrideValue struct {
	// Type is the value type.
	Type catalogfiles.ValueType
	// String holds a catalogfiles.ValueTypeString value.
	String string
	// Integer holds a catalogfiles.ValueTypeInteger value.
	Integer int64
	// Boolean holds a catalogfiles.ValueTypeBoolean value.
	Boolean bool
}

// Limit is the per period limit of a cap, counted for the policy's feature,
// or for all features when the policy has none.
type Limit struct {
	// Kind is what the limit counts.
	Kind LimitKind
	// Count is the most decisions per period for LimitKindCount.
	Count int64
	// Amount is the most cost per period for LimitKindAmount.
	Amount money.Amount
}

// FieldError is one problem in a policy document.
type FieldError struct {
	// Path is the JSON path of the field, such as when.all[1].value, or
	// empty for the document as a whole.
	Path string
	// Message states what the field expects. It never holds the rejected
	// value.
	Message string
}

type fieldErrorList []FieldError

type conditionJSON struct {
	Signal   signals.Name `json:"signal"`
	Operator Operator     `json:"operator"`
	Value    string       `json:"value"`
}

type actionJSON struct {
	Outcome    Outcome                  `json:"outcome"`
	RouteChain []RouteTarget            `json:"route_chain"`
	Overrides  map[string]OverrideValue `json:"overrides"`
	Limit      *Limit                   `json:"limit"`
}

type limitJSON struct {
	Kind  LimitKind `json:"kind"`
	Value string    `json:"value"`
}

// ErrInvalidDocument reports a stored condition group or action that does
// not decode, or a value without a JSON form, such as a condition value of an
// unknown kind.
var ErrInvalidDocument = errors.New("invalid policy document")

// MarshalJSON writes the group as {"all": [...]} or {"any": [...]}.
func (group ConditionGroup) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[Quantifier][]GroupMember{group.Quantifier: append([]GroupMember{}, group.Members...)})
}

// UnmarshalJSON reads a group in the form MarshalJSON writes. It returns an
// error wrapping ErrInvalidDocument that lists every malformed field, and
// leaves the group unchanged then.
func (group *ConditionGroup) UnmarshalJSON(data []byte) error {
	value, err := parseStored(data)
	if err != nil {
		return err
	}
	decoder := &documentDecoder{}
	decoded := decoder.group(whenPath, value)
	if err := decoder.fieldErrors.asError(); err != nil {
		return err
	}
	*group = decoded
	return nil
}

// MarshalJSON writes the condition with its value as a decimal string: an
// amount with 9 decimals, a ratio with 4 decimals or a whole count. A value of
// an unknown kind returns an error wrapping ErrInvalidDocument.
func (condition Condition) MarshalJSON() ([]byte, error) {
	var value string
	switch condition.Value.Kind {
	case signals.KindMoney:
		value = money.FormatAmount(condition.Value.Amount)
	case signals.KindRatio:
		value = money.FormatSignal(condition.Value.Ratio)
	case signals.KindCount:
		value = strconv.FormatInt(condition.Value.Count, 10)
	default:
		return nil, fmt.Errorf("%w: condition value of kind %q", ErrInvalidDocument, condition.Value.Kind)
	}
	return json.Marshal(conditionJSON{Signal: condition.Signal, Operator: condition.Operator, Value: value})
}

// MarshalJSON writes the action with outcome, route_chain, overrides and
// limit, null where the action has none.
func (action Action) MarshalJSON() ([]byte, error) {
	return json.Marshal(actionJSON(action))
}

// UnmarshalJSON reads an action in the form MarshalJSON writes. It returns an
// error wrapping ErrInvalidDocument that lists every malformed field, and
// leaves the action unchanged then.
func (action *Action) UnmarshalJSON(data []byte) error {
	value, err := parseStored(data)
	if err != nil {
		return err
	}
	decoder := &documentDecoder{}
	decoded := decoder.action(actionPath, value)
	if err := decoder.fieldErrors.asError(); err != nil {
		return err
	}
	*action = decoded
	return nil
}

// MarshalJSON writes the value as a JSON string, integer or boolean. A value
// of an unknown type returns an error wrapping ErrInvalidDocument.
func (override OverrideValue) MarshalJSON() ([]byte, error) {
	switch override.Type {
	case catalogfiles.ValueTypeString:
		return json.Marshal(override.String)
	case catalogfiles.ValueTypeInteger:
		return json.Marshal(override.Integer)
	case catalogfiles.ValueTypeBoolean:
		return json.Marshal(override.Boolean)
	}
	return nil, fmt.Errorf("%w: override value of type %q", ErrInvalidDocument, override.Type)
}

// MarshalJSON writes the limit as {"kind", "value"} with the value as a
// whole count or an amount with 9 decimals. A limit of an unknown kind
// returns an error wrapping ErrInvalidDocument.
func (limit Limit) MarshalJSON() ([]byte, error) {
	var value string
	switch limit.Kind {
	case LimitKindCount:
		value = strconv.FormatInt(limit.Count, 10)
	case LimitKindAmount:
		value = money.FormatAmount(limit.Amount)
	default:
		return nil, fmt.Errorf("%w: limit of kind %q", ErrInvalidDocument, limit.Kind)
	}
	return json.Marshal(limitJSON{Kind: limit.Kind, Value: value})
}

func (fieldErrors *fieldErrorList) report(path string, message string) {
	*fieldErrors = append(*fieldErrors, FieldError{Path: path, Message: message})
}

func (fieldErrors *fieldErrorList) require(valid bool, path string, message string) {
	if !valid {
		fieldErrors.report(path, message)
	}
}

func (fieldErrors fieldErrorList) asError() error {
	if len(fieldErrors) == 0 {
		return nil
	}
	problems := make([]string, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		problems = append(problems, fieldError.Path+": "+fieldError.Message)
	}
	return fmt.Errorf("%w: %s", ErrInvalidDocument, strings.Join(problems, ", "))
}
