package pricing

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
)

const pricingTag = "Pricing"

// KeyPriceResponse is the API representation of the headline price of one
// meter of a model.
type KeyPriceResponse struct {
	Meter        Meter  `json:"meter" doc:"Meter the price applies to."`
	UnitPrice    string `json:"unit_price" doc:"USD price of unit_quantity units, with 9 decimals."`
	UnitQuantity int64  `json:"unit_quantity" doc:"Whole units unit_price pays for, such as 1000000 for token meters."`
}

// ModelResponse is the API representation of a catalog model.
type ModelResponse struct {
	Provider    string             `json:"provider" doc:"Provider name, such as openai or fal_ai."`
	Model       string             `json:"model" doc:"Model name the rules use."`
	DisplayName *string            `json:"display_name" doc:"Name for people, null when the catalog has none."`
	Status      RuleStatus         `json:"status" enum:"active,deprecated" doc:"deprecated when neither the catalog nor an override in effect prices the model."`
	KeyPrices   []KeyPriceResponse `json:"key_prices" nullable:"false" doc:"Headline price of each meter for the model's default attributes, with the overrides of the environment applied."`
}

// RatedLineResponse is the API representation of the price of one meter of
// a quote.
type RatedLineResponse struct {
	Meter             Meter   `json:"meter" doc:"Meter of the line."`
	Quantity          string  `json:"quantity" doc:"Billed quantity, the usage rounded up to the billing increment when one applies."`
	UnitPrice         *string `json:"unit_price" doc:"USD price of unit_quantity units, null when the line is missing."`
	UnitQuantity      *int64  `json:"unit_quantity" doc:"Whole units unit_price pays for, null when the line is missing."`
	Cost              *string `json:"cost" doc:"USD cost of the line, null when the line is missing."`
	PricingRuleID     *string `json:"pricing_rule_id" doc:"Catalog rule that priced the line, such as prc_01jbvagescfn78y0938nkrkayd."`
	PricingOverrideID *string `json:"pricing_override_id" doc:"Override that priced the line, such as pro_01jbvagescfn78y0938nkrkayd."`
	Missing           bool    `json:"missing" doc:"True when no rule or override prices the meter."`
}

// RatedRequestResponse is the API representation of a priced request.
type RatedRequestResponse struct {
	CostStatus CostStatus          `json:"cost_status" enum:"costed,uncosted" doc:"costed when every line is priced."`
	Cost       *string             `json:"cost" doc:"USD cost of the request, null when it is uncosted."`
	Lines      []RatedLineResponse `json:"lines" nullable:"false" doc:"One line per meter of the usage, ordered by meter. A meter with a zero quantity that no rule or override prices has no line."`
}

// OverrideResponse is the API representation of a price override.
type OverrideResponse struct {
	ID                 string       `json:"id" doc:"Override id, such as pro_01jbvagescfn78y0938nkrkayd. A price change to an override that has started returns a successor with a new id."`
	Type               OverrideType `json:"type" enum:"adjustment,standalone" doc:"adjustment when a catalog rule has the same provider, model, meter and conditions and the override replaces its price, standalone otherwise."`
	Provider           string       `json:"provider" doc:"Provider name."`
	Model              string       `json:"model" doc:"Model name the rules use."`
	Meter              Meter        `json:"meter" doc:"Meter the override prices."`
	Conditions         Attributes   `json:"conditions" doc:"Attributes a request must contain for the override to apply."`
	UnitPrice          string       `json:"unit_price" doc:"Price of unit_quantity units in USD, or in the native unit when native_unit is set."`
	UnitQuantity       int64        `json:"unit_quantity" doc:"Whole units unit_price pays for."`
	NativeUnit         *string      `json:"native_unit" doc:"Unit unit_price is expressed in, such as credits, or null for USD."`
	NativeUnitPrice    *string      `json:"native_unit_price" doc:"USD price of one native unit, or null for USD."`
	EffectiveUnitPrice string       `json:"effective_unit_price" doc:"USD price of unit_quantity units, unit_price times native_unit_price rounded half up to 9 decimals."`
	MinimumCharge      *string      `json:"minimum_charge" doc:"Lowest USD cost of a line, or null. An adjustment without one keeps the minimum charge of its rule."`
	BillingIncrement   *string      `json:"billing_increment" doc:"Quantity usage is rounded up to a multiple of, or null. An adjustment without one keeps the increment of its rule."`
	EffectiveFrom      time.Time    `json:"effective_from" doc:"First instant the override applies."`
	EffectiveTo        *time.Time   `json:"effective_to" doc:"First instant the override no longer applies, or null when it has no end."`
	CreatedAt          time.Time    `json:"created_at" doc:"When the override was created."`
	UpdatedAt          time.Time    `json:"updated_at" doc:"When the override last changed."`
}

type pricingRoutes struct {
	service *Service
}

type listModelsInput struct {
	Search            string `query:"search" doc:"Keeps the models whose model name or display name contains this text in any letter case."`
	Provider          string `query:"provider" doc:"Keeps the models of this provider."`
	Meter             string `query:"meter" doc:"Keeps the models with a rule or override for this meter."`
	IncludeDeprecated bool   `query:"include_deprecated" doc:"Also lists the models that neither the catalog nor an override in effect prices."`
	Cursor            string `query:"cursor" doc:"Cursor from next_cursor of the previous page. Omit it for the first page."`
	Limit             int    `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

type modelAttributesInput struct {
	Provider string `query:"provider" required:"true" minLength:"1" doc:"Provider name."`
	Model    string `query:"model" required:"true" minLength:"1" doc:"Model name or one of its aliases."`
}

type modelAttributesOutput struct {
	Body ModelAttributes
}

type quoteInput struct {
	Body quoteRequest
}

type quoteRequest struct {
	Provider   string            `json:"provider" doc:"Provider name, such as openai or fal_ai."`
	Model      string            `json:"model" doc:"Model name or one of its aliases."`
	Attributes Attributes        `json:"attributes,omitempty" doc:"Attributes of the request, such as resolution or audio. Known keys take the values of the attribute vocabulary, other keys take any value."`
	Usage      map[string]string `json:"usage" doc:"Quantity used per meter, a non-negative decimal with at most 6 decimals, such as 8 for output_seconds."`
}

type quoteOutput struct {
	Body RatedRequestResponse
}

type listOverridesInput struct {
	IncludeEnded bool   `query:"include_ended" doc:"Also lists the overrides that have ended."`
	Cursor       string `query:"cursor" doc:"Cursor from next_cursor of the previous page. Omit it for the first page."`
	Limit        int    `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

type createOverrideInput struct {
	Body createOverrideRequest
}

type createOverrideRequest struct {
	Type             *OverrideType `json:"type,omitempty" enum:"adjustment,standalone" doc:"Expected type. standalone where a catalog rule has the same provider, model, meter and conditions returns 409 pricing_rule_exists_use_adjustment. Omit it to derive the type."`
	Provider         string        `json:"provider" doc:"Provider name, 1 to 200 characters."`
	Model            string        `json:"model" doc:"Model name or one of its aliases, 1 to 200 characters. An alias is stored as the model it names."`
	Meter            string        `json:"meter" doc:"Meter the override prices, one of GET /api/v1/pricing/meters."`
	Conditions       Attributes    `json:"conditions,omitempty" doc:"Attributes a request must contain for the override to apply. Known keys take the values of the attribute vocabulary."`
	UnitPrice        string        `json:"unit_price" doc:"Non-negative price of unit_quantity units in USD, or in the native unit when native_unit is set, such as 0.12."`
	UnitQuantity     int64         `json:"unit_quantity" doc:"Whole units unit_price pays for, 1 or more, such as 1000000 for token meters."`
	NativeUnit       *string       `json:"native_unit,omitempty" doc:"Unit unit_price is expressed in, such as credits, 1 to 40 characters. Requires native_unit_price."`
	NativeUnitPrice  *string       `json:"native_unit_price,omitempty" doc:"Non-negative USD price of one native unit, such as 0.0083. Requires native_unit."`
	MinimumCharge    *string       `json:"minimum_charge,omitempty" doc:"Non-negative lowest USD cost of a line."`
	BillingIncrement *string       `json:"billing_increment,omitempty" doc:"Positive quantity usage is rounded up to a multiple of, such as 1."`
}

type overridePathInput struct {
	PricingOverrideID string `path:"pricing_override_id" doc:"Override id, such as pro_01jbvagescfn78y0938nkrkayd."`
}

type updateOverrideInput struct {
	PricingOverrideID string `path:"pricing_override_id" doc:"Override id, such as pro_01jbvagescfn78y0938nkrkayd."`
	Body              updateOverrideRequest
}

type updateOverrideRequest struct {
	UnitPrice        *string                   `json:"unit_price,omitempty" doc:"New non-negative price of unit_quantity units, in the native unit when one is set."`
	UnitQuantity     *int64                    `json:"unit_quantity,omitempty" doc:"New unit quantity, 1 or more."`
	NativeUnit       NullableChange[string]    `json:"native_unit,omitzero" doc:"New native unit label, or null for USD. Set or clear it together with native_unit_price."`
	NativeUnitPrice  NullableChange[string]    `json:"native_unit_price,omitzero" doc:"New USD price of one native unit, or null for USD."`
	MinimumCharge    NullableChange[string]    `json:"minimum_charge,omitzero" doc:"New non-negative minimum charge in USD, or null for none."`
	BillingIncrement NullableChange[string]    `json:"billing_increment,omitzero" doc:"New positive billing increment, or null for none."`
	EffectiveTo      NullableChange[time.Time] `json:"effective_to,omitzero" doc:"New end, no earlier than now, or null for no end."`
}

type overrideOutput struct {
	Body OverrideResponse
}

// RegisterRoutes adds the pricing routes of service to api on the admin
// group: GET /api/v1/pricing/meters, GET /api/v1/pricing/models,
// GET /api/v1/pricing/model-attributes, GET and POST
// /api/v1/pricing/overrides, GET, PATCH and DELETE
// /api/v1/pricing/overrides/{pricing_override_id} and POST
// /api/v1/pricing/quote. They act in the environment of the admin key or
// member session. Handlers read service only when they run.
func RegisterRoutes(api *httpapi.API, service *Service) {
	routes := &pricingRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-pricing-meters",
		Method:      http.MethodGet,
		Path:        "/api/v1/pricing/meters",
		Summary:     "List meters",
		Description: "Returns every meter with its unit, ordered by name.",
		Tags:        []string{pricingTag},
	}, routes.listMeters)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-pricing-models",
		Method:      http.MethodGet,
		Path:        "/api/v1/pricing/models",
		Summary:     "List models",
		Description: "Returns the models of the pricing catalog and the models the environment prices with overrides, ordered by provider and model, with display names and key prices. Deprecated models are left out unless include_deprecated is true.",
		Tags:        []string{pricingTag},
	}, routes.listModels)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "get-pricing-model-attributes",
		Method:      http.MethodGet,
		Path:        "/api/v1/pricing/model-attributes",
		Summary:     "Get model attributes",
		Description: "Returns the attribute keys and values that apply to the model, from the vocabulary, its parameter mappings, its catalog rules and the overrides of the environment, each with its sources.",
		Tags:        []string{pricingTag},
	}, routes.modelAttributes)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-pricing-overrides",
		Method:      http.MethodGet,
		Path:        "/api/v1/pricing/overrides",
		Summary:     "List overrides",
		Description: "Returns the price overrides of the environment, newest first. Ended overrides are left out unless include_ended is true.",
		Tags:        []string{pricingTag},
	}, routes.listOverrides)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID:   "create-pricing-override",
		Method:        http.MethodPost,
		Path:          "/api/v1/pricing/overrides",
		Summary:       "Create an override",
		Description:   "Creates a price override in the environment, in effect from now. It adjusts the catalog rule with the same provider, model, meter and conditions, or stands alone when there is none.",
		Tags:          []string{pricingTag},
		DefaultStatus: http.StatusCreated,
	}, routes.createOverride)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "get-pricing-override",
		Method:      http.MethodGet,
		Path:        "/api/v1/pricing/overrides/{pricing_override_id}",
		Summary:     "Get an override",
		Description: "Returns the price override.",
		Tags:        []string{pricingTag},
	}, routes.getOverride)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "update-pricing-override",
		Method:      http.MethodPatch,
		Path:        "/api/v1/pricing/overrides/{pricing_override_id}",
		Summary:     "Update an override",
		Description: "Changes the price fields and the end the request holds. Null clears an optional field. A price change to an override that has started ends it now and returns its successor, which has a new id and the new prices from now, so earlier usage keeps the old prices. A change to effective_to alone, or any change before the override starts, keeps the id. An override that has ended, including one a successor replaced, returns 409 pricing_override_ended.",
		Tags:        []string{pricingTag},
	}, routes.updateOverride)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID:   "end-pricing-override",
		Method:        http.MethodDelete,
		Path:          "/api/v1/pricing/overrides/{pricing_override_id}",
		Summary:       "End an override",
		Description:   "Ends the price override now. The override is kept, and one that has already ended keeps its end.",
		Tags:          []string{pricingTag},
		DefaultStatus: http.StatusNoContent,
	}, routes.endOverride)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "create-pricing-quote",
		Method:      http.MethodPost,
		Path:        "/api/v1/pricing/quote",
		Summary:     "Quote a request",
		Description: "Prices the usage of one provider request with the catalog and the overrides of the environment at the current time.",
		Tags:        []string{pricingTag},
	}, routes.quote)
}

func (routes *pricingRoutes) listMeters(ctx context.Context, _ *struct{}) (*httpapi.Page[MeterDescription], error) {
	meters, err := routes.service.Meters(ctx)
	if err != nil {
		return nil, err
	}
	return httpapi.NewPage(meters, ""), nil
}

func (routes *pricingRoutes) listModels(ctx context.Context, input *listModelsInput) (*httpapi.Page[ModelResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	filter := ModelFilter{Search: input.Search, Provider: input.Provider, Meter: input.Meter, IncludeDeprecated: input.IncludeDeprecated}
	models, nextCursor, err := routes.service.ListModels(ctx, environment, filter, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	responses := make([]ModelResponse, 0, len(models))
	for _, model := range models {
		responses = append(responses, newModelResponse(model))
	}
	return httpapi.NewPage(responses, nextCursor), nil
}

func (routes *pricingRoutes) modelAttributes(ctx context.Context, input *modelAttributesInput) (*modelAttributesOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	attributes, err := routes.service.ModelAttributes(ctx, environment, input.Provider, input.Model)
	if err != nil {
		return nil, err
	}
	return &modelAttributesOutput{Body: attributes}, nil
}

func (routes *pricingRoutes) listOverrides(ctx context.Context, input *listOverridesInput) (*httpapi.Page[OverrideResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	records, nextCursor, err := routes.service.ListOverrides(ctx, environment, input.IncludeEnded, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	responses := make([]OverrideResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, newOverrideResponse(record))
	}
	return httpapi.NewPage(responses, nextCursor), nil
}

func (routes *pricingRoutes) createOverride(ctx context.Context, input *createOverrideInput) (*overrideOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	record, err := routes.service.CreateOverride(ctx, environment, CreateOverrideInput{
		Type:             input.Body.Type,
		Provider:         input.Body.Provider,
		Model:            input.Body.Model,
		Meter:            input.Body.Meter,
		Conditions:       input.Body.Conditions,
		UnitPrice:        input.Body.UnitPrice,
		UnitQuantity:     input.Body.UnitQuantity,
		NativeUnit:       input.Body.NativeUnit,
		NativeUnitPrice:  input.Body.NativeUnitPrice,
		MinimumCharge:    input.Body.MinimumCharge,
		BillingIncrement: input.Body.BillingIncrement,
	})
	if err != nil {
		return nil, err
	}
	return &overrideOutput{Body: newOverrideResponse(record)}, nil
}

func (routes *pricingRoutes) getOverride(ctx context.Context, input *overridePathInput) (*overrideOutput, error) {
	environment, overrideID, err := overrideTarget(ctx, input.PricingOverrideID)
	if err != nil {
		return nil, err
	}
	record, err := routes.service.GetOverride(ctx, environment, overrideID)
	if err != nil {
		return nil, err
	}
	return &overrideOutput{Body: newOverrideResponse(record)}, nil
}

func (routes *pricingRoutes) updateOverride(ctx context.Context, input *updateOverrideInput) (*overrideOutput, error) {
	environment, overrideID, err := overrideTarget(ctx, input.PricingOverrideID)
	if err != nil {
		return nil, err
	}
	record, err := routes.service.UpdateOverride(ctx, environment, overrideID, UpdateOverrideInput{
		UnitPrice:        input.Body.UnitPrice,
		UnitQuantity:     input.Body.UnitQuantity,
		NativeUnit:       input.Body.NativeUnit,
		NativeUnitPrice:  input.Body.NativeUnitPrice,
		MinimumCharge:    input.Body.MinimumCharge,
		BillingIncrement: input.Body.BillingIncrement,
		EffectiveTo:      input.Body.EffectiveTo,
	})
	if err != nil {
		return nil, err
	}
	return &overrideOutput{Body: newOverrideResponse(record)}, nil
}

func (routes *pricingRoutes) endOverride(ctx context.Context, input *overridePathInput) (*struct{}, error) {
	environment, overrideID, err := overrideTarget(ctx, input.PricingOverrideID)
	if err != nil {
		return nil, err
	}
	return nil, routes.service.EndOverride(ctx, environment, overrideID)
}

func (routes *pricingRoutes) quote(ctx context.Context, input *quoteInput) (*quoteOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	rated, err := routes.service.Quote(ctx, environment, QuoteRequest{
		Provider:   input.Body.Provider,
		Model:      input.Body.Model,
		Attributes: input.Body.Attributes,
		Usage:      input.Body.Usage,
	})
	if err != nil {
		return nil, err
	}
	return &quoteOutput{Body: newRatedRequestResponse(rated)}, nil
}

func overrideTarget(ctx context.Context, exposedID string) (httpapi.Environment, uuid.UUID, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return "", uuid.UUID{}, err
	}
	overrideID, err := identifiers.Decode(identifiers.PrefixPricingOverride, exposedID)
	if err != nil {
		return "", uuid.UUID{}, httpapi.ErrNotFound
	}
	return environment, overrideID, nil
}

func newModelResponse(model CatalogModel) ModelResponse {
	keyPrices := make([]KeyPriceResponse, 0, len(model.KeyPrices))
	for _, keyPrice := range model.KeyPrices {
		keyPrices = append(keyPrices, KeyPriceResponse{
			Meter:        keyPrice.Meter,
			UnitPrice:    money.FormatAmount(keyPrice.UnitPrice.Nanos),
			UnitQuantity: keyPrice.UnitPrice.UnitQuantity,
		})
	}
	return ModelResponse{
		Provider:    model.Provider,
		Model:       model.Model,
		DisplayName: model.DisplayName,
		Status:      model.Status,
		KeyPrices:   keyPrices,
	}
}

func newRatedRequestResponse(rated RatedRequest) RatedRequestResponse {
	response := RatedRequestResponse{CostStatus: rated.CostStatus, Lines: make([]RatedLineResponse, 0, len(rated.Lines))}
	if rated.Cost != nil {
		cost := money.FormatAmount(*rated.Cost)
		response.Cost = &cost
	}
	for _, line := range rated.Lines {
		lineResponse := RatedLineResponse{Meter: line.Meter, Quantity: money.FormatQuantity(line.Quantity), Missing: line.Missing}
		if !line.Missing {
			unitPrice := money.FormatAmount(line.UnitPrice.Nanos)
			cost := money.FormatAmount(line.Cost)
			lineResponse.UnitPrice = &unitPrice
			lineResponse.UnitQuantity = &line.UnitPrice.UnitQuantity
			lineResponse.Cost = &cost
		}
		if line.RuleID != nil {
			ruleID := identifiers.Encode(identifiers.PrefixPricingRule, *line.RuleID)
			lineResponse.PricingRuleID = &ruleID
		}
		if line.OverrideID != nil {
			overrideID := identifiers.Encode(identifiers.PrefixPricingOverride, *line.OverrideID)
			lineResponse.PricingOverrideID = &overrideID
		}
		response.Lines = append(response.Lines, lineResponse)
	}
	return response
}

func newOverrideResponse(record OverrideRecord) OverrideResponse {
	response := OverrideResponse{
		ID:                 identifiers.Encode(identifiers.PrefixPricingOverride, record.ID),
		Type:               record.Type,
		Provider:           record.Provider,
		Model:              record.Model,
		Meter:              record.Meter,
		Conditions:         record.Conditions,
		UnitPrice:          money.FormatAmount(record.UnitPrice.Nanos),
		UnitQuantity:       record.UnitPrice.UnitQuantity,
		EffectiveUnitPrice: money.FormatAmount(record.EffectiveUnitPrice.Nanos),
		EffectiveFrom:      record.EffectiveFrom,
		EffectiveTo:        record.EffectiveTo,
		CreatedAt:          record.CreatedAt,
		UpdatedAt:          record.UpdatedAt,
	}
	if record.NativeUnit != nil {
		nativeUnitPrice := money.FormatAmount(record.NativeUnit.Price)
		response.NativeUnit = &record.NativeUnit.Label
		response.NativeUnitPrice = &nativeUnitPrice
	}
	if record.MinimumCharge != nil {
		minimumCharge := money.FormatAmount(*record.MinimumCharge)
		response.MinimumCharge = &minimumCharge
	}
	if record.BillingIncrement != nil {
		billingIncrement := money.FormatQuantity(*record.BillingIncrement)
		response.BillingIncrement = &billingIncrement
	}
	return response
}
