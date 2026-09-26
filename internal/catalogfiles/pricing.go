package catalogfiles

import (
	"fmt"
	"net/url"
	"time"

	"github.com/preburn/preburn/internal/money"
)

const sourceURLScheme = "https"

// CuratedModel is one model entry of a curated pricing file.
type CuratedModel struct {
	// Provider is the base name of the pricing file, such as fal_ai for
	// pricing/fal_ai.yaml.
	Provider string
	// Model is the model name the provider's API uses.
	Model string
	// DisplayName is the model's name for people.
	DisplayName string
	// SourceURL is the https page the prices were read from.
	SourceURL string
	// VerifiedOn is midnight UTC of the day the prices were last checked
	// against SourceURL.
	VerifiedOn time.Time
	// Rules holds at least one rule, and no two rules share a meter and
	// conditions.
	Rules []Rule
}

// Rule prices one meter of a curated model for requests whose attributes
// include every condition.
type Rule struct {
	// Meter is the meter the rule prices.
	Meter string
	// Conditions holds the attributes a request must carry for the rule to
	// match, empty for a rule that matches every request.
	Conditions Attributes
	// UnitPrice is the price per unit quantity.
	UnitPrice money.UnitPrice
	// MinimumCharge is the lowest cost of a line priced by the rule, nil when
	// there is none.
	MinimumCharge *money.Amount
	// BillingIncrement is the quantity that usage is rounded up to a multiple
	// of before rating, nil when usage is not rounded.
	BillingIncrement *money.Quantity
}

type pricingDocument struct {
	Models []modelDocument `yaml:"models"`
}

type modelDocument struct {
	Model       string         `yaml:"model"`
	DisplayName string         `yaml:"display_name"`
	SourceURL   string         `yaml:"source_url"`
	VerifiedOn  string         `yaml:"verified_on"`
	Rules       []ruleDocument `yaml:"rules"`
}

type ruleDocument struct {
	Meter            string     `yaml:"meter"`
	Conditions       Attributes `yaml:"conditions"`
	UnitPrice        string     `yaml:"unit_price"`
	UnitQuantity     int64      `yaml:"unit_quantity"`
	MinimumCharge    string     `yaml:"minimum_charge"`
	BillingIncrement string     `yaml:"billing_increment"`
}

func (loader *catalogLoader) loadPricing(provider string, data []byte) error {
	var document pricingDocument
	if err := decodeStrict(data, &document); err != nil {
		return err
	}
	if len(document.Models) == 0 {
		return fmt.Errorf("%w: models", ErrMissingField)
	}
	for _, entry := range document.Models {
		if entry.Model == "" {
			return fmt.Errorf("%w: model", ErrMissingField)
		}
		model, err := parseCuratedModel(provider, entry)
		if err != nil {
			return fmt.Errorf("model %q: %w", entry.Model, err)
		}
		key := ModelKey{Provider: provider, Model: model.Model}
		if loader.curatedModels[key] {
			return fmt.Errorf("model %q: %w model entry", model.Model, ErrDuplicate)
		}
		loader.curatedModels[key] = true
		loader.catalog.CuratedModels = append(loader.catalog.CuratedModels, model)
		loader.catalog.DisplayNames[key] = model.DisplayName
	}
	return nil
}

func parseCuratedModel(provider string, entry modelDocument) (CuratedModel, error) {
	if entry.DisplayName == "" {
		return CuratedModel{}, fmt.Errorf("%w: display_name", ErrMissingField)
	}
	sourceURL, err := url.Parse(entry.SourceURL)
	if err != nil || sourceURL.Scheme != sourceURLScheme || sourceURL.Host == "" {
		return CuratedModel{}, fmt.Errorf("%w %q", ErrInvalidSourceURL, entry.SourceURL)
	}
	verifiedOn, err := time.Parse(time.DateOnly, entry.VerifiedOn)
	if err != nil {
		return CuratedModel{}, fmt.Errorf("%w %q", ErrInvalidVerifiedOn, entry.VerifiedOn)
	}
	if len(entry.Rules) == 0 {
		return CuratedModel{}, fmt.Errorf("%w: rules", ErrMissingField)
	}
	model := CuratedModel{
		Provider:    provider,
		Model:       entry.Model,
		DisplayName: entry.DisplayName,
		SourceURL:   entry.SourceURL,
		VerifiedOn:  verifiedOn,
		Rules:       make([]Rule, 0, len(entry.Rules)),
	}
	ruleKeys := map[string]bool{}
	for index, ruleEntry := range entry.Rules {
		rule, err := parseRule(ruleEntry)
		if err != nil {
			return CuratedModel{}, fmt.Errorf("rule %d: %w", index+1, err)
		}
		ruleKey := rule.Meter + " " + rule.Conditions.Canonical()
		if ruleKeys[ruleKey] {
			return CuratedModel{}, fmt.Errorf("rule %d: %w rule for meter %s with conditions %q", index+1, ErrDuplicate, rule.Meter, rule.Conditions.Canonical())
		}
		ruleKeys[ruleKey] = true
		model.Rules = append(model.Rules, rule)
	}
	return model, nil
}

func parseRule(entry ruleDocument) (Rule, error) {
	if err := validateMeter(entry.Meter); err != nil {
		return Rule{}, err
	}
	conditions, err := validateAttributes(entry.Conditions)
	if err != nil {
		return Rule{}, err
	}
	unitPrice, err := money.ParseUnitPrice(entry.UnitPrice, entry.UnitQuantity)
	if err != nil {
		return Rule{}, fmt.Errorf("%w: %w", ErrInvalidPrice, err)
	}
	minimumCharge, err := parseMinimumCharge(entry.MinimumCharge)
	if err != nil {
		return Rule{}, err
	}
	billingIncrement, err := parseBillingIncrement(entry.BillingIncrement)
	if err != nil {
		return Rule{}, err
	}
	return Rule{
		Meter:            entry.Meter,
		Conditions:       conditions,
		UnitPrice:        unitPrice,
		MinimumCharge:    minimumCharge,
		BillingIncrement: billingIncrement,
	}, nil
}

func parseMinimumCharge(value string) (*money.Amount, error) {
	if value == "" {
		return nil, nil
	}
	minimumCharge, err := money.ParseNonNegativeAmount(value)
	if err != nil {
		return nil, fmt.Errorf("%w: minimum_charge: %w", ErrInvalidPrice, err)
	}
	if minimumCharge == 0 {
		return nil, fmt.Errorf("%w: minimum_charge must be above zero", ErrInvalidPrice)
	}
	return &minimumCharge, nil
}

func parseBillingIncrement(value string) (*money.Quantity, error) {
	if value == "" {
		return nil, nil
	}
	billingIncrement, err := money.ParseQuantity(value)
	if err != nil {
		return nil, fmt.Errorf("%w: billing_increment: %w", ErrInvalidPrice, err)
	}
	if billingIncrement == 0 {
		return nil, fmt.Errorf("%w: billing_increment must be above zero", ErrInvalidPrice)
	}
	return &billingIncrement, nil
}
