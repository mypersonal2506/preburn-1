package pricing

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/pricing/queries"
)

const (
	modelListingName   = "pricing_models"
	meterQueryLocation = "query.meter"
)

const (
	// AttributeSourceVocabulary marks a key or value of the attribute
	// vocabulary.
	AttributeSourceVocabulary AttributeSource = "vocabulary"
	// AttributeSourceParameterMappings marks a value that a parameter mapping
	// of the model with the prices effect allows.
	AttributeSourceParameterMappings AttributeSource = "parameter_mappings"
	// AttributeSourceRules marks a value that a condition of an open catalog
	// rule of the model holds.
	AttributeSourceRules AttributeSource = "rules"
	// AttributeSourceOverrides marks a value that a condition of an override
	// of the model in the environment holds, while the override has not
	// ended.
	AttributeSourceOverrides AttributeSource = "overrides"
)

// AttributeSource names where Service.ModelAttributes found an attribute key
// or value.
type AttributeSource string

// MeterDescription is one meter of the meter vocabulary with its unit and
// description.
type MeterDescription struct {
	Meter       Meter  `json:"meter" doc:"Meter name."`
	Unit        string `json:"unit" doc:"Unit the meter counts, such as token or second."`
	Description string `json:"description" doc:"What the meter counts."`
}

// ModelFilter narrows Service.ListModels. Empty fields do not filter.
type ModelFilter struct {
	// Search keeps the models whose model name or display name contains it in
	// any letter case.
	Search string
	// Provider keeps the models of this provider.
	Provider string
	// Meter keeps the models with a rule or override for this meter.
	Meter string
	// IncludeDeprecated keeps the models with neither an open rule nor an
	// override in effect, which the listing leaves out otherwise.
	IncludeDeprecated bool
}

// CatalogModel is one provider model of the catalog rules or of the
// overrides of an environment.
type CatalogModel struct {
	// Provider is the provider name, such as openai.
	Provider string
	// Model is the model name the rules use.
	Model string
	// DisplayName is the name for people from the catalog files, or nil when
	// they have none.
	DisplayName *string
	// Status is active when the model has an open rule or an override in
	// effect, and deprecated otherwise.
	Status RuleStatus
	// KeyPrices holds the headline price of each meter, as KeyPrices returns
	// it for the model's default attributes and the RuleSet of the
	// environment.
	KeyPrices []KeyPrice
}

// ModelAttributes lists the attribute keys and values that apply to one
// provider model in one environment.
type ModelAttributes struct {
	Provider   string           `json:"provider" doc:"Provider name."`
	Model      string           `json:"model" doc:"Model name the rules use, the target when the request named an alias."`
	Attributes []ModelAttribute `json:"attributes" nullable:"false" doc:"Every key of the attribute vocabulary and every other key a rule or override of the model names, ordered by key."`
}

// ModelAttribute is one attribute key of a model with its known values.
type ModelAttribute struct {
	Key     string                `json:"key" doc:"Attribute key, such as resolution."`
	Sources []AttributeSource     `json:"sources" nullable:"false" enum:"vocabulary,parameter_mappings,rules,overrides" doc:"Where the key was found."`
	Values  []ModelAttributeValue `json:"values" nullable:"false" doc:"Known values, vocabulary values first in vocabulary order, then values from the other sources in the order found. Empty for a key that takes any string."`
}

// ModelAttributeValue is one known value of an attribute key.
type ModelAttributeValue struct {
	Value   AttributeValue    `json:"value" doc:"Attribute value, a string, boolean or integer."`
	Sources []AttributeSource `json:"sources" nullable:"false" enum:"vocabulary,parameter_mappings,rules,overrides" doc:"Where the value was found."`
}

type modelPosition struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type attributeCollection map[string]*ModelAttribute

var modelCursor = httpapi.NewCursor[modelPosition](modelListingName)

// Meters returns every meter of the meter vocabulary ordered by name.
func (service *Service) Meters(ctx context.Context) ([]MeterDescription, error) {
	rows, err := service.queries.ListMeters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list meters: %w", err)
	}
	meters := make([]MeterDescription, 0, len(rows))
	for _, row := range rows {
		meters = append(meters, MeterDescription{Meter: Meter(row.Meter), Unit: row.Unit, Description: row.Description})
	}
	return meters, nil
}

// ListModels returns one page of the provider models that filter keeps,
// ordered by provider and model, and the cursor of the next page, which is
// empty on the last page. The models are those of the catalog rules and those
// the overrides of environment price, so a model priced only by standalone
// overrides is listed. Key prices come from the RuleSet of environment, so
// its overrides apply. A limit of 0 selects
// httpapi.ListLimitDefault, and a limit outside 1 to
// httpapi.ListLimitMaximum returns a 422 validation_failed problem at
// query.limit. A filter meter outside the vocabulary returns the problem at
// query.meter. A cursor that ListModels did not return for environment fails
// with httpapi.ErrInvalidCursor.
func (service *Service) ListModels(ctx context.Context, environment httpapi.Environment, filter ModelFilter, cursor string, limit int) ([]CatalogModel, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	now := service.clock.Now()
	parameters := queries.ListModelsParams{
		Environment:       queries.Environment(environment),
		At:                now,
		IncludeDeprecated: filter.IncludeDeprecated,
		RowLimit:          int64(pageSize) + 1,
	}
	if filter.Meter != "" {
		if _, err := ParseMeter(filter.Meter); err != nil {
			return nil, "", httpapi.NewValidationProblem(httpapi.ProblemError{Location: meterQueryLocation, Message: meterRule})
		}
		parameters.Meter = &filter.Meter
	}
	if filter.Provider != "" {
		parameters.Provider = &filter.Provider
	}
	if filter.Search != "" {
		parameters.Search = &filter.Search
		parameters.DisplayNameProviders, parameters.DisplayNameModels = service.displayNameMatches(filter.Search)
	}
	if cursor != "" {
		position, err := modelCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		parameters.AfterProvider = &position.Provider
		parameters.AfterModel = &position.Model
	}
	rows, err := service.queries.ListModels(ctx, parameters)
	if err != nil {
		return nil, "", fmt.Errorf("list pricing models: %w", err)
	}
	ruleSet, err := service.ruleSets.Get(ctx, environment)
	if err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		last := rows[pageSize-1]
		nextCursor, err = modelCursor.Encode(environment, modelPosition{Provider: last.Provider, Model: last.Model})
		if err != nil {
			return nil, "", err
		}
	}
	models := make([]CatalogModel, 0, len(rows))
	for _, row := range rows {
		model, err := service.catalogModel(row, ruleSet, now)
		if err != nil {
			return nil, "", err
		}
		models = append(models, model)
	}
	return models, nextCursor, nil
}

// ModelAttributes returns the attribute keys and values that apply to the
// provider model, resolved through the model aliases, in environment. Every
// key of the attribute vocabulary is listed with its fixed values, joined by
// the values the model's price parameter mappings allow, the conditions of
// its open catalog rules and the conditions of its overrides in environment
// that have not ended. Each key and value carries the sources it was found
// in. A model without rules gets the vocabulary alone.
func (service *Service) ModelAttributes(ctx context.Context, environment httpapi.Environment, provider, model string) (ModelAttributes, error) {
	model, err := service.canonicalModel(ctx, provider, model)
	if err != nil {
		return ModelAttributes{}, err
	}
	ruleConditions, err := service.queries.ListOpenRuleConditions(ctx, queries.ListOpenRuleConditionsParams{Provider: provider, Model: model})
	if err != nil {
		return ModelAttributes{}, fmt.Errorf("list rule conditions of %s/%s: %w", provider, model, err)
	}
	overrideConditions, err := service.queries.ListOpenOverrideConditions(ctx, queries.ListOpenOverrideConditionsParams{
		Environment: queries.Environment(environment),
		Provider:    provider,
		Model:       model,
		At:          service.clock.Now(),
	})
	if err != nil {
		return ModelAttributes{}, fmt.Errorf("list override conditions of %s/%s: %w", provider, model, err)
	}
	collection := attributeCollection{}
	collection.addVocabulary()
	collection.addParameterMappings(service.files.ParameterMappings[catalogfiles.ModelKey{Provider: provider, Model: model}])
	if err := collection.addConditions(ruleConditions, AttributeSourceRules); err != nil {
		return ModelAttributes{}, err
	}
	if err := collection.addConditions(overrideConditions, AttributeSourceOverrides); err != nil {
		return ModelAttributes{}, err
	}
	attributes := make([]ModelAttribute, 0, len(collection))
	for _, key := range slices.Sorted(maps.Keys(collection)) {
		attributes = append(attributes, *collection[key])
	}
	return ModelAttributes{Provider: provider, Model: model, Attributes: attributes}, nil
}

func (service *Service) catalogModel(row queries.ListModelsRow, ruleSet *RuleSet, at time.Time) (CatalogModel, error) {
	key := catalogfiles.ModelKey{Provider: row.Provider, Model: row.Model}
	defaults, err := attributesFromCatalog(service.files.DefaultAttributes[key])
	if err != nil {
		return CatalogModel{}, fmt.Errorf("default attributes of %s: %w", key, err)
	}
	model := CatalogModel{
		Provider:  row.Provider,
		Model:     row.Model,
		Status:    RuleStatusDeprecated,
		KeyPrices: KeyPrices(row.Provider, row.Model, defaults, ruleSet, at),
	}
	if row.HasPriceInEffect {
		model.Status = RuleStatusActive
	}
	if displayName, named := service.files.DisplayNames[key]; named {
		model.DisplayName = &displayName
	}
	return model, nil
}

func (service *Service) displayNameMatches(search string) ([]string, []string) {
	lowered := strings.ToLower(search)
	providers := []string{}
	models := []string{}
	for key, displayName := range service.files.DisplayNames {
		if strings.Contains(strings.ToLower(displayName), lowered) {
			providers = append(providers, key.Provider)
			models = append(models, key.Model)
		}
	}
	return providers, models
}

func (collection attributeCollection) addVocabulary() {
	for _, requirement := range attributeVocabulary {
		collection.addKey(requirement.key, AttributeSourceVocabulary)
		if requirement.kind == attributeKindBoolean {
			collection.addValue(requirement.key, BooleanAttribute(false), AttributeSourceVocabulary)
			collection.addValue(requirement.key, BooleanAttribute(true), AttributeSourceVocabulary)
		}
		for _, value := range requirement.values {
			collection.addValue(requirement.key, StringAttribute(value), AttributeSourceVocabulary)
		}
	}
}

func (collection attributeCollection) addParameterMappings(parameters map[string]catalogfiles.Parameter) {
	for _, name := range slices.Sorted(maps.Keys(parameters)) {
		parameter := parameters[name]
		if parameter.Effect != catalogfiles.EffectPrices {
			continue
		}
		if parameter.ValueType == catalogfiles.ValueTypeBoolean {
			collection.addValue(name, BooleanAttribute(false), AttributeSourceParameterMappings)
			collection.addValue(name, BooleanAttribute(true), AttributeSourceParameterMappings)
		}
		for _, value := range parameter.AllowedStrings {
			collection.addValue(name, StringAttribute(value), AttributeSourceParameterMappings)
		}
	}
}

func (collection attributeCollection) addConditions(encodedConditions [][]byte, source AttributeSource) error {
	for _, encoded := range encodedConditions {
		conditions, err := decodeConditions(encoded)
		if err != nil {
			return fmt.Errorf("decode %s conditions: %w", source, err)
		}
		for _, key := range slices.Sorted(maps.Keys(conditions)) {
			collection.addValue(key, conditions[key], source)
		}
	}
	return nil
}

func (collection attributeCollection) addKey(key string, source AttributeSource) *ModelAttribute {
	attribute, found := collection[key]
	if !found {
		attribute = &ModelAttribute{Key: key, Sources: []AttributeSource{}, Values: []ModelAttributeValue{}}
		collection[key] = attribute
	}
	if !slices.Contains(attribute.Sources, source) {
		attribute.Sources = append(attribute.Sources, source)
	}
	return attribute
}

func (collection attributeCollection) addValue(key string, value AttributeValue, source AttributeSource) {
	attribute := collection.addKey(key, source)
	index := slices.IndexFunc(attribute.Values, func(known ModelAttributeValue) bool { return known.Value == value })
	if index < 0 {
		attribute.Values = append(attribute.Values, ModelAttributeValue{Value: value, Sources: []AttributeSource{source}})
		return
	}
	if !slices.Contains(attribute.Values[index].Sources, source) {
		attribute.Values[index].Sources = append(attribute.Values[index].Sources, source)
	}
}
