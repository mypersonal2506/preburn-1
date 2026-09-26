package policies

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strconv"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/signals"
)

const (
	documentRule        = "expected one JSON object"
	unexpectedFieldRule = "expected no field with this name"
	membersRule         = "expected an array of conditions and condition groups"
	actionRule          = "expected an object with outcome, route_chain, overrides and limit"
	routeChainRule      = "expected null or an array of route targets"
	routeTargetRule     = "expected an object with provider and model"
	overridesRule       = "expected null or an object of parameter overrides"
	overrideValueRule   = "expected a string, integer or boolean"
	limitRule           = "expected null or an object with kind and value"
)

type documentDecoder struct {
	fieldErrors fieldErrorList
}

var (
	documentFields    = []string{"name", "level", "plan_id", "customer_id", "feature", "when", "action", "enforcement", "on_unreachable", "on_uncosted", "status"}
	conditionFields   = []string{"signal", "operator", "value"}
	actionFields      = []string{"outcome", "route_chain", "overrides", "limit"}
	routeTargetFields = []string{"provider", "model"}
	limitFields       = []string{"kind", "value"}

	planIDRule     = fmt.Sprintf("expected null or a plan id starting with %s_", identifiers.PrefixPlan)
	customerIDRule = fmt.Sprintf("expected null or a customer id starting with %s_", identifiers.PrefixCustomer)
)

// DecodeDocument reads the API JSON form of a policy document and returns a
// FieldError for every field of the wrong JSON type or format: an unexpected
// field, a missing required field, an id without its prefix, or a condition or
// limit value that does not parse as its kind. Enumerated fields such as level
// are kept as given, so Validate reports unknown values. The Document is
// complete only when no FieldError is returned, and Validate checks it next.
func DecodeDocument(data []byte) (Document, []FieldError) {
	value, parsed := parseJSON(data)
	if !parsed {
		return Document{}, []FieldError{{Path: documentPath, Message: documentRule}}
	}
	decoder := &documentDecoder{}
	object, isObject := decoder.object(documentPath, value, documentRule, documentFields)
	if !isObject {
		return Document{}, decoder.fieldErrors
	}
	document := Document{
		Name: decoder.requiredString(object, documentPath, "name", nameRule),
		Scope: Scope{
			Level:      Level(decoder.requiredString(object, documentPath, "level", levelRule)),
			PlanID:     decoder.optionalIdentifier(object, documentPath, "plan_id", identifiers.PrefixPlan, planIDRule),
			CustomerID: decoder.optionalIdentifier(object, documentPath, "customer_id", identifiers.PrefixCustomer, customerIDRule),
		},
		Feature:       decoder.optionalString(object, documentPath, "feature", featureRule),
		When:          decoder.group(whenPath, object["when"]),
		Action:        decoder.action(actionPath, object["action"]),
		Enforcement:   Enforcement(decoder.requiredString(object, documentPath, "enforcement", enforcementRule)),
		OnUnreachable: Outcome(decoder.requiredString(object, documentPath, "on_unreachable", fallbackOutcomeRule)),
		OnUncosted:    Outcome(decoder.requiredString(object, documentPath, "on_uncosted", fallbackOutcomeRule)),
		Status:        Status(decoder.requiredString(object, documentPath, "status", statusRule)),
	}
	return document, decoder.fieldErrors
}

func (decoder *documentDecoder) group(path string, value any) ConditionGroup {
	object, isObject := value.(map[string]any)
	if !isObject {
		decoder.fieldErrors.report(path, groupRule)
		return ConditionGroup{}
	}
	var quantifiers []Quantifier
	for _, key := range slices.Sorted(maps.Keys(object)) {
		switch quantifier := Quantifier(key); quantifier {
		case QuantifierAll, QuantifierAny:
			quantifiers = append(quantifiers, quantifier)
		default:
			decoder.fieldErrors.report(joinPath(path, key), unexpectedFieldRule)
		}
	}
	if len(quantifiers) != 1 {
		decoder.fieldErrors.report(path, groupRule)
		return ConditionGroup{}
	}
	quantifier := quantifiers[0]
	membersPath := joinPath(path, string(quantifier))
	memberValues, isArray := object[string(quantifier)].([]any)
	if !isArray {
		decoder.fieldErrors.report(membersPath, membersRule)
		return ConditionGroup{Quantifier: quantifier}
	}
	members := make([]GroupMember, 0, len(memberValues))
	for index, memberValue := range memberValues {
		memberPath := indexPath(membersPath, index)
		memberObject, isMemberObject := memberValue.(map[string]any)
		if !isMemberObject {
			decoder.fieldErrors.report(memberPath, memberRule)
			continue
		}
		_, hasAll := memberObject[string(QuantifierAll)]
		_, hasAny := memberObject[string(QuantifierAny)]
		if hasAll || hasAny {
			members = append(members, decoder.group(memberPath, memberObject))
		} else {
			members = append(members, decoder.condition(memberPath, memberObject))
		}
	}
	return ConditionGroup{Quantifier: quantifier, Members: members}
}

func (decoder *documentDecoder) condition(path string, object map[string]any) Condition {
	decoder.unexpectedFields(path, object, conditionFields)
	condition := Condition{
		Signal:   signals.Name(decoder.requiredString(object, path, "signal", signalRule)),
		Operator: Operator(decoder.requiredString(object, path, "operator", operatorRule)),
	}
	kind, known := condition.Signal.Kind()
	if !known {
		return condition
	}
	text, _ := object["value"].(string)
	value, valid := parseSignalValue(text, kind)
	decoder.fieldErrors.require(valid, joinPath(path, "value"), signalValueRules[kind])
	condition.Value = value
	return condition
}

func (decoder *documentDecoder) action(path string, value any) Action {
	object, isObject := decoder.object(path, value, actionRule, actionFields)
	if !isObject {
		return Action{}
	}
	return Action{
		Outcome:    Outcome(decoder.requiredString(object, path, "outcome", outcomeRule)),
		RouteChain: decoder.routeChain(joinPath(path, "route_chain"), object["route_chain"]),
		Overrides:  decoder.overrides(joinPath(path, "overrides"), object["overrides"]),
		Limit:      decoder.limit(joinPath(path, "limit"), object["limit"]),
	}
}

func (decoder *documentDecoder) routeChain(path string, value any) []RouteTarget {
	if value == nil {
		return nil
	}
	entries, isArray := value.([]any)
	if !isArray {
		decoder.fieldErrors.report(path, routeChainRule)
		return nil
	}
	targets := make([]RouteTarget, 0, len(entries))
	for index, entry := range entries {
		entryPath := indexPath(path, index)
		object, isObject := decoder.object(entryPath, entry, routeTargetRule, routeTargetFields)
		if !isObject {
			continue
		}
		targets = append(targets, RouteTarget{
			Provider: decoder.requiredString(object, entryPath, "provider", routeTargetNameRule),
			Model:    decoder.requiredString(object, entryPath, "model", routeTargetNameRule),
		})
	}
	return targets
}

func (decoder *documentDecoder) overrides(path string, value any) map[string]OverrideValue {
	if value == nil {
		return nil
	}
	object, isObject := value.(map[string]any)
	if !isObject {
		decoder.fieldErrors.report(path, overridesRule)
		return nil
	}
	overrides := make(map[string]OverrideValue, len(object))
	for _, key := range slices.Sorted(maps.Keys(object)) {
		override, valid := parseOverrideValue(object[key])
		decoder.fieldErrors.require(valid, joinPath(path, key), overrideValueRule)
		overrides[key] = override
	}
	return overrides
}

func (decoder *documentDecoder) limit(path string, value any) *Limit {
	if value == nil {
		return nil
	}
	object, isObject := decoder.object(path, value, limitRule, limitFields)
	if !isObject {
		return nil
	}
	limit := Limit{Kind: LimitKind(decoder.requiredString(object, path, "kind", limitKindRule))}
	valuePath := joinPath(path, "value")
	text, _ := object["value"].(string)
	switch limit.Kind {
	case LimitKindCount:
		count, valid := parseCount(text)
		decoder.fieldErrors.require(valid, valuePath, countValueRule)
		limit.Count = count
	case LimitKindAmount:
		amount, err := money.ParseNonNegativeAmount(text)
		decoder.fieldErrors.require(err == nil, valuePath, amountLimitRule)
		limit.Amount = amount
	}
	return &limit
}

func (decoder *documentDecoder) object(path string, value any, rule string, fields []string) (map[string]any, bool) {
	object, isObject := value.(map[string]any)
	if !isObject {
		decoder.fieldErrors.report(path, rule)
		return nil, false
	}
	decoder.unexpectedFields(path, object, fields)
	return object, true
}

func (decoder *documentDecoder) unexpectedFields(path string, object map[string]any, fields []string) {
	for _, key := range slices.Sorted(maps.Keys(object)) {
		decoder.fieldErrors.require(slices.Contains(fields, key), joinPath(path, key), unexpectedFieldRule)
	}
}

func (decoder *documentDecoder) requiredString(object map[string]any, path string, key string, rule string) string {
	text, isString := object[key].(string)
	decoder.fieldErrors.require(isString, joinPath(path, key), rule)
	return text
}

func (decoder *documentDecoder) optionalString(object map[string]any, path string, key string, rule string) *string {
	if object[key] == nil {
		return nil
	}
	text := decoder.requiredString(object, path, key, rule)
	return &text
}

func (decoder *documentDecoder) optionalIdentifier(object map[string]any, path string, key string, prefix identifiers.Prefix, rule string) *uuid.UUID {
	if object[key] == nil {
		return nil
	}
	text, _ := object[key].(string)
	identifier, err := identifiers.Decode(prefix, text)
	if err != nil {
		decoder.fieldErrors.report(joinPath(path, key), rule)
		return nil
	}
	return &identifier
}

func parseJSON(data []byte) (any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	_, err := decoder.Token()
	return value, errors.Is(err, io.EOF)
}

func parseStored(data []byte) (any, error) {
	value, parsed := parseJSON(data)
	if !parsed {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDocument, documentRule)
	}
	return value, nil
}

func parseSignalValue(text string, kind signals.Kind) (signals.Value, bool) {
	switch kind {
	case signals.KindMoney:
		amount, err := money.ParseAmount(text)
		return signals.Value{Kind: kind, Amount: amount}, err == nil
	case signals.KindRatio:
		ratio, err := money.ParseSignalRatio(text)
		return signals.Value{Kind: kind, Ratio: ratio}, err == nil && !math.IsInf(ratio, 0)
	case signals.KindCount:
		count, valid := parseCount(text)
		return signals.Value{Kind: kind, Count: count}, valid
	}
	return signals.Value{}, false
}

func parseCount(text string) (int64, bool) {
	count, err := strconv.ParseInt(text, 10, 64)
	return count, err == nil && count >= 0 && strconv.FormatInt(count, 10) == text
}

func parseOverrideValue(value any) (OverrideValue, bool) {
	switch typed := value.(type) {
	case string:
		return OverrideValue{Type: catalogfiles.ValueTypeString, String: typed}, true
	case bool:
		return OverrideValue{Type: catalogfiles.ValueTypeBoolean, Boolean: typed}, true
	case json.Number:
		integer, err := strconv.ParseInt(typed.String(), 10, 64)
		return OverrideValue{Type: catalogfiles.ValueTypeInteger, Integer: integer}, err == nil
	}
	return OverrideValue{}, false
}

func joinPath(path string, key string) string {
	if path == documentPath {
		return key
	}
	return path + "." + key
}

func indexPath(path string, index int) string {
	return path + "[" + strconv.Itoa(index) + "]"
}
