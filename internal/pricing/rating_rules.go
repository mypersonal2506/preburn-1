package pricing

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/money"
)

const (
	// RuleSourceCurated marks a rule from the curated catalog files.
	RuleSourceCurated RuleSource = "curated"
	// RuleSourceLiteLLM marks a rule imported from the LiteLLM price snapshot.
	RuleSourceLiteLLM RuleSource = "litellm"
)

const (
	// RuleStatusActive marks a rule the catalog still lists.
	RuleStatusActive RuleStatus = "active"
	// RuleStatusDeprecated marks a rule the catalog no longer lists. It
	// still prices requests inside its effective window.
	RuleStatusDeprecated RuleStatus = "deprecated"
)

const nanosPerUSD = 1_000_000_000

// RuleSource is where a catalog rule comes from. Between matching rules with
// the same number of condition keys, a curated rule wins over a litellm rule.
type RuleSource string

// RuleStatus is the catalog state of a rule.
type RuleStatus string

// Rule is a catalog pricing rule: the price of one meter of one provider
// model for the requests whose attributes contain its conditions, in effect
// from EffectiveFrom up to, but not including, EffectiveTo.
type Rule struct {
	// ID is the rule's id.
	ID uuid.UUID
	// Provider is the provider name, such as openai.
	Provider string
	// Model is the provider's model name, such as gpt-4o-mini.
	Model string
	// Meter is the usage dimension the rule prices.
	Meter Meter
	// Conditions are the attributes a request must contain for the rule to
	// apply. Empty conditions match every request.
	Conditions Attributes
	// UnitPrice is the USD price.
	UnitPrice money.UnitPrice
	// MinimumCharge, when set, is the lowest cost of a meter line priced by
	// the rule.
	MinimumCharge *money.Amount
	// BillingIncrement, when set, rounds the usage quantity up to a multiple
	// of itself before pricing.
	BillingIncrement *money.Quantity
	// EffectiveFrom is the first instant the rule applies.
	EffectiveFrom time.Time
	// EffectiveTo, when set, is the first instant the rule no longer applies.
	EffectiveTo *time.Time
	// Status is the catalog state of the rule.
	Status RuleStatus
	// Source is where the rule comes from.
	Source RuleSource
}

// NativeUnit is the unit a provider bills in when it is not USD, such as
// credits, with its USD price.
type NativeUnit struct {
	// Label names the unit, such as credits.
	Label string
	// Price is the USD price of one native unit.
	Price money.Amount
}

// Override is an installation price override of one environment. At a
// request time when its provider, model, meter and conditions equal a catalog
// rule in effect, it is an adjustment that replaces that rule's price.
// Otherwise it is standalone and is matched before the catalog as a rule of
// its own.
type Override struct {
	// ID is the override's id.
	ID uuid.UUID
	// Environment is the environment the override prices.
	Environment httpapi.Environment
	// Provider is the provider name, such as openai.
	Provider string
	// Model is the provider's model name, such as gpt-4o-mini.
	Model string
	// Meter is the usage dimension the override prices.
	Meter Meter
	// Conditions are the attributes a request must contain for the override
	// to apply.
	Conditions Attributes
	// UnitPrice is the price in USD, or in billionths of the native unit when
	// NativeUnit is set, so 20 credits is Nanos 20,000,000,000.
	UnitPrice money.UnitPrice
	// MinimumCharge, when set, is the lowest USD cost of a meter line. An
	// adjustment without one keeps the minimum charge of its rule.
	MinimumCharge *money.Amount
	// BillingIncrement, when set, rounds the usage quantity up to a multiple
	// of itself. An adjustment without one keeps the increment of its rule.
	BillingIncrement *money.Quantity
	// NativeUnit, when set, is the unit UnitPrice is expressed in.
	NativeUnit *NativeUnit
	// EffectiveFrom is the first instant the override applies.
	EffectiveFrom time.Time
	// EffectiveTo, when set, is the first instant the override no longer
	// applies.
	EffectiveTo *time.Time
}

// ModelAlias maps another name of a provider model, such as a dated
// snapshot name, to the model name the rules use.
type ModelAlias struct {
	// Provider is the provider name, such as openai.
	Provider string
	// Alias is the other name, such as gpt-4o-mini-2024-07-18.
	Alias string
	// Model is the model name the rules use, such as gpt-4o-mini.
	Model string
}

// RuleSet holds catalog rules, the overrides of one environment and model
// aliases, indexed by provider, model and meter and ordered for matching.
// Build it with NewRuleSet. It never changes after that and is safe for
// concurrent use.
type RuleSet struct {
	aliases map[modelKey]string
	models  map[modelKey]modelRules
}

type modelKey struct {
	provider string
	model    string
}

type modelRules map[Meter]*meterRules

type meterRules struct {
	overrides []pricedOverride
	rules     []Rule
}

type pricedOverride struct {
	override  Override
	unitPrice money.UnitPrice
}

type selectedPrice struct {
	unitPrice        money.UnitPrice
	minimumCharge    *money.Amount
	billingIncrement *money.Quantity
	ruleID           *uuid.UUID
	overrideID       *uuid.UUID
}

// ErrInvalidRuleSet reports input NewRuleSet rejects: a rule with an unknown
// source or status, overrides of more than one environment, or an alias
// defined twice.
var ErrInvalidRuleSet = errors.New("invalid rule set")

var sourcePrecedence = map[RuleSource]int{
	RuleSourceCurated: 0,
	RuleSourceLiteLLM: 1,
}

// NewRuleSet indexes rules, overrides and aliases for Rate and KeyPrices.
// Every override must belong to the same environment. The RuleSet keeps the
// conditions maps and pointer fields of its input, so callers do not modify
// them afterwards. It returns ErrInvalidRuleSet for invalid input and
// money.ErrOverflow when an override's USD price does not fit int64.
func NewRuleSet(rules []Rule, overrides []Override, aliases []ModelAlias) (*RuleSet, error) {
	ruleSet := &RuleSet{aliases: make(map[modelKey]string, len(aliases)), models: map[modelKey]modelRules{}}
	for _, alias := range aliases {
		key := modelKey{provider: alias.Provider, model: alias.Alias}
		if _, defined := ruleSet.aliases[key]; defined {
			return nil, fmt.Errorf("%w: alias %s of provider %s defined twice", ErrInvalidRuleSet, alias.Alias, alias.Provider)
		}
		ruleSet.aliases[key] = alias.Model
	}
	for _, rule := range rules {
		if _, known := sourcePrecedence[rule.Source]; !known {
			return nil, fmt.Errorf("%w: rule %s has unknown source %q", ErrInvalidRuleSet, rule.ID, rule.Source)
		}
		if rule.Status != RuleStatusActive && rule.Status != RuleStatusDeprecated {
			return nil, fmt.Errorf("%w: rule %s has unknown status %q", ErrInvalidRuleSet, rule.ID, rule.Status)
		}
		entry := ruleSet.entry(rule.Provider, rule.Model, rule.Meter)
		entry.rules = append(entry.rules, rule)
	}
	for _, override := range overrides {
		if override.Environment != overrides[0].Environment {
			return nil, fmt.Errorf("%w: overrides of environments %s and %s", ErrInvalidRuleSet, overrides[0].Environment, override.Environment)
		}
		unitPrice, err := override.EffectiveUnitPrice()
		if err != nil {
			return nil, fmt.Errorf("override %s: %w", override.ID, err)
		}
		entry := ruleSet.entry(override.Provider, override.Model, override.Meter)
		entry.overrides = append(entry.overrides, pricedOverride{override: override, unitPrice: unitPrice})
	}
	for _, meters := range ruleSet.models {
		for _, entry := range meters {
			slices.SortFunc(entry.rules, compareRules)
			slices.SortFunc(entry.overrides, compareOverrides)
		}
	}
	return ruleSet, nil
}

// EffectiveUnitPrice returns the USD price of override. Without a native
// unit it is UnitPrice. With one, its Nanos are UnitPrice.Nanos times the
// native unit's Price divided by 1,000,000,000, rounded half up, for the
// same UnitQuantity. It returns money.ErrOverflow when the result does not
// fit int64.
func (override Override) EffectiveUnitPrice() (money.UnitPrice, error) {
	if override.NativeUnit == nil {
		return override.UnitPrice, nil
	}
	product := new(big.Int).Mul(big.NewInt(int64(override.UnitPrice.Nanos)), big.NewInt(int64(override.NativeUnit.Price)))
	divisor := big.NewInt(nanosPerUSD)
	nanos, remainder := new(big.Int).DivMod(product, divisor, new(big.Int))
	if doubledRemainder := new(big.Int).Lsh(remainder, 1); doubledRemainder.Cmp(divisor) >= 0 {
		nanos.Add(nanos, big.NewInt(1))
	}
	if !nanos.IsInt64() {
		return money.UnitPrice{}, fmt.Errorf("%w: %s %s at %s each", money.ErrOverflow, money.FormatAmount(override.UnitPrice.Nanos), override.NativeUnit.Label, money.FormatAmount(override.NativeUnit.Price))
	}
	return money.UnitPrice{Nanos: money.Amount(nanos.Int64()), UnitQuantity: override.UnitPrice.UnitQuantity}, nil
}

func (ruleSet *RuleSet) entry(provider, model string, meter Meter) *meterRules {
	key := modelKey{provider: provider, model: model}
	meters, found := ruleSet.models[key]
	if !found {
		meters = modelRules{}
		ruleSet.models[key] = meters
	}
	entry, found := meters[meter]
	if !found {
		entry = &meterRules{}
		meters[meter] = entry
	}
	return entry
}

func (ruleSet *RuleSet) lookupModel(provider, model string) modelRules {
	if target, isAlias := ruleSet.aliases[modelKey{provider: provider, model: model}]; isAlias {
		model = target
	}
	return ruleSet.models[modelKey{provider: provider, model: model}]
}

func (meters modelRules) selectPrice(meter Meter, attributes Attributes, at time.Time) (selectedPrice, bool) {
	entry, found := meters[meter]
	if !found {
		return selectedPrice{}, false
	}
	for _, standalone := range entry.overrides {
		if standalone.inEffect(at) && attributes.contain(standalone.override.Conditions) && !entry.hasRuleInEffect(standalone.override.Conditions, at) {
			return selectedPrice{
				unitPrice:        standalone.unitPrice,
				minimumCharge:    standalone.override.MinimumCharge,
				billingIncrement: standalone.override.BillingIncrement,
				overrideID:       &standalone.override.ID,
			}, true
		}
	}
	for _, rule := range entry.rules {
		if !inEffect(rule.EffectiveFrom, rule.EffectiveTo, at) || !attributes.contain(rule.Conditions) {
			continue
		}
		for _, adjustment := range entry.overrides {
			if adjustment.inEffect(at) && maps.Equal(adjustment.override.Conditions, rule.Conditions) {
				return adjustment.adjust(rule), true
			}
		}
		return selectedPrice{
			unitPrice:        rule.UnitPrice,
			minimumCharge:    rule.MinimumCharge,
			billingIncrement: rule.BillingIncrement,
			ruleID:           &rule.ID,
		}, true
	}
	return selectedPrice{}, false
}

func (entry *meterRules) hasRuleInEffect(conditions Attributes, at time.Time) bool {
	return slices.ContainsFunc(entry.rules, func(rule Rule) bool {
		return inEffect(rule.EffectiveFrom, rule.EffectiveTo, at) && maps.Equal(rule.Conditions, conditions)
	})
}

func (priced pricedOverride) inEffect(at time.Time) bool {
	return inEffect(priced.override.EffectiveFrom, priced.override.EffectiveTo, at)
}

func (priced pricedOverride) adjust(rule Rule) selectedPrice {
	price := selectedPrice{
		unitPrice:        priced.unitPrice,
		minimumCharge:    rule.MinimumCharge,
		billingIncrement: rule.BillingIncrement,
		overrideID:       &priced.override.ID,
	}
	if priced.override.MinimumCharge != nil {
		price.minimumCharge = priced.override.MinimumCharge
	}
	if priced.override.BillingIncrement != nil {
		price.billingIncrement = priced.override.BillingIncrement
	}
	return price
}

func inEffect(from time.Time, to *time.Time, at time.Time) bool {
	return !at.Before(from) && (to == nil || at.Before(*to))
}

func compareRules(first, second Rule) int {
	return cmp.Or(
		cmp.Compare(len(second.Conditions), len(first.Conditions)),
		cmp.Compare(sourcePrecedence[first.Source], sourcePrecedence[second.Source]),
		second.EffectiveFrom.Compare(first.EffectiveFrom),
		bytes.Compare(first.ID[:], second.ID[:]),
	)
}

func compareOverrides(first, second pricedOverride) int {
	return cmp.Or(
		cmp.Compare(len(second.override.Conditions), len(first.override.Conditions)),
		second.override.EffectiveFrom.Compare(first.override.EffectiveFrom),
		bytes.Compare(first.override.ID[:], second.override.ID[:]),
	)
}
