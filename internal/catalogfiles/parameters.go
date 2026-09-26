package catalogfiles

import (
	"fmt"
	"maps"
	"slices"

	"github.com/preburn/preburn/internal/money"
)

const minimumIntegerValue = 1

// ValueType is the type of a parameter's value.
type ValueType string

const (
	// ValueTypeString is a string parameter that takes one of AllowedStrings.
	ValueTypeString ValueType = "string"
	// ValueTypeInteger is an integer parameter that takes one of
	// AllowedIntegers, or any value from Minimum to Maximum.
	ValueTypeInteger ValueType = "integer"
	// ValueTypeBoolean is a boolean parameter.
	ValueTypeBoolean ValueType = "boolean"
)

// Effect is how a parameter's value changes the rating of a request.
type Effect string

const (
	// EffectSets sets the quantity of the meter. A string value sets the
	// quantity in Quantities, an integer value sets that many whole units.
	EffectSets Effect = "sets"
	// EffectBounds caps the quantity of the meter at the integer value in whole
	// units.
	EffectBounds Effect = "bounds"
	// EffectPrices sets the pricing attribute named like the parameter, which
	// selects the rule that prices the meter.
	EffectPrices Effect = "prices"
)

// Parameter is one overridable parameter of a provider model, keyed in
// Catalog.ParameterMappings by the name that policy overrides use.
//
// A string parameter lists AllowedStrings. An integer parameter lists
// AllowedIntegers or sets Minimum and Maximum, never both, and every integer
// is at least 1. A boolean parameter lists no values. EffectSets needs a
// string or integer parameter that is not a pricing attribute, and a string
// one maps every allowed value in Quantities. EffectBounds needs an integer
// parameter that is not a pricing attribute. EffectPrices needs a parameter
// named like a pricing attribute whose type and values that attribute
// accepts.
type Parameter struct {
	// ProviderParameter is the parameter's name in the provider's API.
	ProviderParameter string
	// ValueType is the type of the parameter's value.
	ValueType ValueType
	// AllowedStrings lists the values of a string parameter, nil otherwise.
	AllowedStrings []string
	// AllowedIntegers lists the values of an integer parameter with a fixed
	// set of values, nil otherwise.
	AllowedIntegers []int64
	// Minimum is the lowest value of an integer parameter with a range, nil
	// otherwise.
	Minimum *int64
	// Maximum is the highest value of an integer parameter with a range, nil
	// otherwise.
	Maximum *int64
	// Effect is how the value changes the rating of a request.
	Effect Effect
	// Meter is the meter whose quantity or price the parameter changes.
	Meter string
	// Quantities maps every value of a string parameter with EffectSets to the
	// quantity it sets, nil otherwise.
	Quantities map[string]money.Quantity
}

type parameterMappingsDocument struct {
	CuratedModels map[string]map[string]map[string]parameterDocument `yaml:"curated_models"`
	LiteLLMModels map[string]map[string]map[string]parameterDocument `yaml:"litellm_models"`
}

type parameterDocument struct {
	ProviderParameter string            `yaml:"provider_parameter"`
	ValueType         ValueType         `yaml:"value_type"`
	AllowedValues     []any             `yaml:"allowed_values"`
	Minimum           *int64            `yaml:"minimum"`
	Maximum           *int64            `yaml:"maximum"`
	Effect            Effect            `yaml:"effect"`
	Meter             string            `yaml:"meter"`
	Quantities        map[string]string `yaml:"quantities"`
}

func (loader *catalogLoader) loadParameterMappings(data []byte) error {
	var document parameterMappingsDocument
	if err := decodeStrict(data, &document); err != nil {
		return err
	}
	return forEachModel(loader, document.CuratedModels, document.LiteLLMModels, func(model ModelKey, entries map[string]parameterDocument) error {
		parameters := make(map[string]Parameter, len(entries))
		for _, name := range slices.Sorted(maps.Keys(entries)) {
			parameter, err := parseParameter(name, entries[name])
			if err != nil {
				return fmt.Errorf("parameter %q: %w", name, err)
			}
			parameters[name] = parameter
		}
		loader.catalog.ParameterMappings[model] = parameters
		return nil
	})
}

func parseParameter(name string, entry parameterDocument) (Parameter, error) {
	if entry.ProviderParameter == "" {
		return Parameter{}, fmt.Errorf("%w: provider_parameter is missing", ErrInvalidParameter)
	}
	if err := validateMeter(entry.Meter); err != nil {
		return Parameter{}, err
	}
	if err := validateEffect(name, entry); err != nil {
		return Parameter{}, err
	}
	parameter := Parameter{
		ProviderParameter: entry.ProviderParameter,
		ValueType:         entry.ValueType,
		Minimum:           entry.Minimum,
		Maximum:           entry.Maximum,
		Effect:            entry.Effect,
		Meter:             entry.Meter,
	}
	var err error
	switch entry.ValueType {
	case ValueTypeString:
		parameter.AllowedStrings, err = parseAllowedStrings(entry)
	case ValueTypeInteger:
		parameter.AllowedIntegers, err = parseAllowedIntegers(entry)
	case ValueTypeBoolean:
		if len(entry.AllowedValues) > 0 || entry.Minimum != nil || entry.Maximum != nil {
			err = fmt.Errorf("%w: a boolean parameter takes no allowed_values, minimum or maximum", ErrInvalidParameter)
		}
	default:
		err = fmt.Errorf("%w: unknown value_type %q", ErrInvalidParameter, entry.ValueType)
	}
	if err != nil {
		return Parameter{}, err
	}
	switch parameter.Effect {
	case EffectSets:
		parameter.Quantities, err = parseSetQuantities(parameter, entry.Quantities)
	case EffectBounds:
		if parameter.ValueType != ValueTypeInteger {
			err = fmt.Errorf("%w: only an integer parameter can bound a meter", ErrInvalidParameter)
		}
	case EffectPrices:
		err = validatePricedValues(name, parameter)
	}
	if err != nil {
		return Parameter{}, err
	}
	return parameter, nil
}

func validateEffect(name string, entry parameterDocument) error {
	_, isAttribute := vocabularyRule(name)
	switch {
	case entry.Effect != EffectSets && entry.Effect != EffectBounds && entry.Effect != EffectPrices:
		return fmt.Errorf("%w: unknown effect %q", ErrInvalidParameter, entry.Effect)
	case isAttribute && entry.Effect != EffectPrices:
		return fmt.Errorf("%w: %s is a pricing attribute, so its effect is prices", ErrInvalidParameter, name)
	case !isAttribute && entry.Effect == EffectPrices:
		return fmt.Errorf("%w: %s is not a pricing attribute, so it cannot price a meter", ErrInvalidParameter, name)
	case entry.Effect != EffectSets && len(entry.Quantities) > 0:
		return fmt.Errorf("%w: only a parameter that sets a meter takes quantities", ErrInvalidParameter)
	}
	return nil
}

func parseAllowedStrings(entry parameterDocument) ([]string, error) {
	if entry.Minimum != nil || entry.Maximum != nil {
		return nil, fmt.Errorf("%w: a string parameter takes allowed_values, not minimum or maximum", ErrInvalidParameter)
	}
	if len(entry.AllowedValues) == 0 {
		return nil, fmt.Errorf("%w: a string parameter needs allowed_values", ErrInvalidParameter)
	}
	values := make([]string, 0, len(entry.AllowedValues))
	for _, value := range entry.AllowedValues {
		text, isString := value.(string)
		switch {
		case !isString || text == "":
			return nil, fmt.Errorf("%w: allowed value %v is not a non-empty string", ErrInvalidParameter, value)
		case slices.Contains(values, text):
			return nil, fmt.Errorf("%w: allowed value %q is listed twice", ErrInvalidParameter, text)
		}
		values = append(values, text)
	}
	return values, nil
}

func parseAllowedIntegers(entry parameterDocument) ([]int64, error) {
	if entry.Minimum != nil || entry.Maximum != nil {
		return nil, validateIntegerRange(entry)
	}
	if len(entry.AllowedValues) == 0 {
		return nil, fmt.Errorf("%w: an integer parameter needs allowed_values or minimum and maximum", ErrInvalidParameter)
	}
	values := make([]int64, 0, len(entry.AllowedValues))
	for _, value := range entry.AllowedValues {
		integer, isInteger := value.(int)
		switch {
		case !isInteger || integer < minimumIntegerValue:
			return nil, fmt.Errorf("%w: allowed value %v is not an integer of %d or above", ErrInvalidParameter, value, minimumIntegerValue)
		case slices.Contains(values, int64(integer)):
			return nil, fmt.Errorf("%w: allowed value %d is listed twice", ErrInvalidParameter, integer)
		}
		values = append(values, int64(integer))
	}
	return values, nil
}

func validateIntegerRange(entry parameterDocument) error {
	switch {
	case len(entry.AllowedValues) > 0:
		return fmt.Errorf("%w: an integer parameter takes allowed_values or minimum and maximum, not both", ErrInvalidParameter)
	case entry.Minimum == nil || entry.Maximum == nil:
		return fmt.Errorf("%w: an integer range needs both minimum and maximum", ErrInvalidParameter)
	case *entry.Minimum < minimumIntegerValue || *entry.Minimum > *entry.Maximum:
		return fmt.Errorf("%w: range %d to %d must start at %d or above and not end below its start", ErrInvalidParameter, *entry.Minimum, *entry.Maximum, minimumIntegerValue)
	}
	return nil
}

func parseSetQuantities(parameter Parameter, quantities map[string]string) (map[string]money.Quantity, error) {
	if parameter.ValueType == ValueTypeBoolean {
		return nil, fmt.Errorf("%w: a boolean parameter cannot set a meter", ErrInvalidParameter)
	}
	if parameter.ValueType == ValueTypeInteger {
		if len(quantities) > 0 {
			return nil, fmt.Errorf("%w: an integer parameter sets its own value, so it takes no quantities", ErrInvalidParameter)
		}
		return nil, nil
	}
	if len(quantities) != len(parameter.AllowedStrings) {
		return nil, fmt.Errorf("%w: quantities must map exactly the allowed values", ErrInvalidParameter)
	}
	parsed := make(map[string]money.Quantity, len(quantities))
	for _, value := range parameter.AllowedStrings {
		text, found := quantities[value]
		if !found {
			return nil, fmt.Errorf("%w: no quantity for allowed value %q", ErrInvalidParameter, value)
		}
		quantity, err := money.ParseQuantity(text)
		if err != nil {
			return nil, fmt.Errorf("%w: quantity for %q: %w", ErrInvalidParameter, value, err)
		}
		if quantity == 0 {
			return nil, fmt.Errorf("%w: quantity for %q must be above zero", ErrInvalidParameter, value)
		}
		parsed[value] = quantity
	}
	return parsed, nil
}

func validatePricedValues(name string, parameter Parameter) error {
	rule, _ := vocabularyRule(name)
	attributeType := ValueTypeString
	if rule.boolean {
		attributeType = ValueTypeBoolean
	}
	if parameter.ValueType != attributeType {
		return fmt.Errorf("%w: the %s attribute takes %s values", ErrInvalidParameter, name, attributeType)
	}
	for _, value := range parameter.AllowedStrings {
		if err := validateAttribute(name, value); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidParameter, err)
		}
	}
	return nil
}
