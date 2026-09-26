package decisions

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	attributesMaximum     = 32
	nameMaximumLength     = 200
	quantityMicrosPerUnit = 1_000_000

	featureLocation       = "body.feature"
	providerLocation      = "body.provider"
	modelLocation         = "body.model"
	attributesLocation    = "body.attributes"
	usageEstimateLocation = "body.usage_estimate"
	usageCeilingLocation  = "body.usage_ceiling"

	meterRule    = "expected a meter listed by GET /api/v1/pricing/meters"
	quantityRule = "expected a non-negative quantity with at most 6 decimals, such as 8.5"
)

type usage map[pricing.Meter]money.Quantity

type parsedCheck struct {
	attributes pricing.Attributes
	estimate   usage
	ceiling    usage
}

type plannedRequest struct {
	provider   string
	model      string
	attributes pricing.Attributes
	usages     map[EstimateBasis]usage
	overrides  map[string]policies.OverrideValue
}

type pricedRequest struct {
	plannedRequest
	costs map[EstimateBasis]*money.Amount
}

type checkProblems []httpapi.ProblemError

var (
	featureRule    = "expected a feature name matching " + plans.FeaturePattern.String()
	nameRule       = fmt.Sprintf("expected 1 to %d characters without control characters", nameMaximumLength)
	attributesRule = fmt.Sprintf("expected at most %d attributes", attributesMaximum)
)

var reservationBases = map[policies.Enforcement][]EstimateBasis{
	policies.EnforcementHard: {EstimateBasisCeiling, EstimateBasisRequestEstimate},
	policies.EnforcementSoft: {EstimateBasisP95, EstimateBasisCeiling, EstimateBasisRequestEstimate},
}

func parseCheckRequest(request CheckRequest) (parsedCheck, error) {
	var problems checkProblems
	problems.require(plans.FeaturePattern.MatchString(request.Feature), featureLocation, featureRule)
	problems.require(validName(request.Provider), providerLocation, nameRule)
	problems.require(validName(request.Model), modelLocation, nameRule)
	problems.require(len(request.Attributes) <= attributesMaximum, attributesLocation, attributesRule)
	for _, key := range slices.Sorted(maps.Keys(request.Attributes)) {
		err := pricing.ValidateAttributes(pricing.Attributes{key: request.Attributes[key]})
		if invalid, isInvalid := errors.AsType[*pricing.InvalidAttributeError](err); isInvalid {
			problems.add(attributesLocation+"."+key, "expected "+invalid.Requirement)
		}
	}
	parsed := parsedCheck{
		attributes: pricing.Attributes{},
		estimate:   problems.usage(request.UsageEstimate, usageEstimateLocation),
		ceiling:    problems.usage(request.UsageCeiling, usageCeilingLocation),
	}
	maps.Copy(parsed.attributes, request.Attributes)
	if len(problems) > 0 {
		return parsedCheck{}, httpapi.NewValidationProblem(problems...)
	}
	return parsed, nil
}

func (parsed parsedCheck) plan(provider string, model string, usageEstimates map[catalogfiles.ModelKey]usage) plannedRequest {
	planned := plannedRequest{
		provider:   provider,
		model:      model,
		attributes: parsed.attributes,
		usages:     map[EstimateBasis]usage{},
		overrides:  map[string]policies.OverrideValue{},
	}
	if len(parsed.estimate) > 0 {
		planned.usages[EstimateBasisRequestEstimate] = parsed.estimate
	}
	if len(parsed.ceiling) > 0 {
		planned.usages[EstimateBasisCeiling] = parsed.ceiling
	}
	if p95, found := usageEstimates[catalogfiles.ModelKey{Provider: provider, Model: model}]; found {
		planned.usages[EstimateBasisP95] = p95
	}
	return planned
}

func (planned plannedRequest) retarget(provider string, model string, usageEstimates map[catalogfiles.ModelKey]usage) plannedRequest {
	parsed := parsedCheck{
		attributes: planned.attributes,
		estimate:   planned.usages[EstimateBasisRequestEstimate],
		ceiling:    planned.usages[EstimateBasisCeiling],
	}
	return parsed.plan(provider, model, usageEstimates)
}

// Every key of overrides must be in parameters, as
// policies.ApplicableOverrides guarantees: a missing key reads as a zero
// Parameter whose empty Effect applies nothing.
func (planned plannedRequest) withOverrides(overrides map[string]policies.OverrideValue, parameters map[string]catalogfiles.Parameter) plannedRequest {
	adjusted := plannedRequest{
		provider:   planned.provider,
		model:      planned.model,
		attributes: maps.Clone(planned.attributes),
		usages:     make(map[EstimateBasis]usage, len(planned.usages)),
		overrides:  overrides,
	}
	for basis, meters := range planned.usages {
		adjusted.usages[basis] = maps.Clone(meters)
	}
	for key, override := range overrides {
		parameter := parameters[key]
		meter := pricing.Meter(parameter.Meter)
		switch parameter.Effect {
		case catalogfiles.EffectSets:
			for _, meters := range adjusted.usages {
				meters[meter] = overrideQuantity(parameter, override)
			}
		case catalogfiles.EffectBounds:
			for _, meters := range adjusted.usages {
				if quantity, present := meters[meter]; present {
					meters[meter] = min(quantity, overrideQuantity(parameter, override))
				}
			}
		case catalogfiles.EffectPrices:
			adjusted.attributes[key] = overrideAttribute(override)
		}
	}
	return adjusted
}

func (planned plannedRequest) price(ruleSet *pricing.RuleSet, at time.Time) (pricedRequest, error) {
	priced := pricedRequest{plannedRequest: planned, costs: make(map[EstimateBasis]*money.Amount, len(planned.usages))}
	for basis, meters := range planned.usages {
		rated, err := pricing.Rate(pricing.RatingRequest{
			Provider:   planned.provider,
			Model:      planned.model,
			Attributes: planned.attributes,
			Usage:      meters,
			OccurredAt: at,
		}, ruleSet)
		if err != nil {
			return pricedRequest{}, fmt.Errorf("rate %s usage of %s %s: %w", basis, planned.provider, planned.model, err)
		}
		priced.costs[basis] = rated.Cost
	}
	return priced, nil
}

func (priced pricedRequest) expectedCost() *money.Amount {
	for _, basis := range []EstimateBasis{EstimateBasisRequestEstimate, EstimateBasisCeiling} {
		if _, present := priced.usages[basis]; present {
			return priced.costs[basis]
		}
	}
	return new(money.Amount(0))
}

func (priced pricedRequest) reservation(enforcement policies.Enforcement) (money.Amount, EstimateBasis) {
	for _, basis := range reservationBases[enforcement] {
		if cost := priced.costs[basis]; cost != nil {
			return *cost, basis
		}
	}
	return 0, EstimateBasisNone
}

func (problems *checkProblems) usage(quantities map[string]string, location string) usage {
	if len(quantities) == 0 {
		return nil
	}
	parsed := make(usage, len(quantities))
	unknownMeter := false
	for _, name := range slices.Sorted(maps.Keys(quantities)) {
		meter, err := pricing.ParseMeter(name)
		if err != nil {
			unknownMeter = true
			continue
		}
		quantity, err := money.ParseQuantity(quantities[name])
		problems.require(err == nil, location+"."+name, quantityRule)
		parsed[meter] = quantity
	}
	problems.require(!unknownMeter, location, meterRule)
	return parsed
}

func (problems *checkProblems) require(valid bool, location string, message string) {
	if !valid {
		problems.add(location, message)
	}
}

func (problems *checkProblems) add(location string, message string) {
	*problems = append(*problems, httpapi.ProblemError{Location: location, Message: message})
}

func overrideQuantity(parameter catalogfiles.Parameter, override policies.OverrideValue) money.Quantity {
	if override.Type == catalogfiles.ValueTypeString {
		return parameter.Quantities[override.String]
	}
	return money.Quantity(override.Integer * quantityMicrosPerUnit)
}

func overrideAttribute(override policies.OverrideValue) pricing.AttributeValue {
	switch override.Type {
	case catalogfiles.ValueTypeString:
		return pricing.StringAttribute(override.String)
	case catalogfiles.ValueTypeInteger:
		return pricing.IntegerAttribute(override.Integer)
	case catalogfiles.ValueTypeBoolean:
		return pricing.BooleanAttribute(override.Boolean)
	}
	return pricing.AttributeValue{}
}

func validName(value string) bool {
	return strings.TrimSpace(value) != "" &&
		utf8.RuneCountInString(value) <= nameMaximumLength &&
		utf8.ValidString(value) &&
		!strings.ContainsFunc(value, unicode.IsControl)
}
