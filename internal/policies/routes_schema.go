package policies

import (
	"reflect"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/signals"
)

const (
	conditionValueDescription = "Decimal string compared with the signal: an amount in USD for money signals, such as 2.50, a ratio with at most 4 decimals for ratio signals, such as 0.40, and a whole number for period_decision_count."
	draftDescription          = "Policy document. The server checks every field and answers 422 policy_invalid with one error per failing field."
	changesDescription        = "Policy document fields to replace. Omitted fields keep their stored values."
)

type documentRequest struct {
	document []byte
}

type documentChangesRequest struct {
	changes []byte
}

type policyConditionGroup struct{}

type policyCondition struct{}

type policyAction struct{}

var draftRequiredFields = []string{namePath, levelPath, whenPath, actionPath, enforcementPath, onUnreachablePath, onUncostedPath}

func (request *documentRequest) UnmarshalJSON(data []byte) error {
	request.document = slices.Clone(data)
	return nil
}

func (documentRequest) Schema(registry huma.Registry) *huma.Schema {
	schema := huma.SchemaFromType(registry, reflect.TypeFor[DocumentResponse]())
	schema.Description = draftDescription
	schema.Required = draftRequiredFields
	return schema
}

func (request *documentChangesRequest) UnmarshalJSON(data []byte) error {
	request.changes = slices.Clone(data)
	return nil
}

func (documentChangesRequest) Schema(registry huma.Registry) *huma.Schema {
	schema := huma.SchemaFromType(registry, reflect.TypeFor[DocumentResponse]())
	schema.Description = changesDescription
	schema.Required = nil
	return schema
}

// Schema documents a condition group as a reference to the
// PolicyConditionGroup component: {"all": [...]} or {"any": [...]} with
// conditions and nested groups as members.
func (ConditionGroup) Schema(registry huma.Registry) *huma.Schema {
	return registry.Schema(reflect.TypeFor[policyConditionGroup](), true, "")
}

// Schema documents an action as a reference to the PolicyAction component:
// the outcome with the route chain, overrides and limit, each null when the
// action has none.
func (Action) Schema(registry huma.Registry) *huma.Schema {
	return registry.Schema(reflect.TypeFor[policyAction](), true, "")
}

// Schema documents an override value as a JSON string, integer or boolean.
func (OverrideValue) Schema(huma.Registry) *huma.Schema {
	return overrideValueSchema()
}

func (policyConditionGroup) TransformSchema(registry huma.Registry, _ *huma.Schema) *huma.Schema {
	member := &huma.Schema{OneOf: []*huma.Schema{
		registry.Schema(reflect.TypeFor[policyCondition](), true, ""),
		registry.Schema(reflect.TypeFor[policyConditionGroup](), true, ""),
	}}
	return &huma.Schema{
		Type:        huma.TypeObject,
		Description: "Condition group holding exactly one of all and any. Members are conditions or nested groups, at most 3 groups deep counting this one.",
		Properties: map[string]*huma.Schema{
			string(QuantifierAll): {Type: huma.TypeArray, Items: member, Description: "Holds when every member holds. An empty all always holds."},
			string(QuantifierAny): {Type: huma.TypeArray, Items: member, MinItems: new(1), Description: "Holds when at least one member holds."},
		},
		MinProperties:        new(1),
		MaxProperties:        new(1),
		AdditionalProperties: false,
	}
}

func (policyCondition) TransformSchema(huma.Registry, *huma.Schema) *huma.Schema {
	names := make([]any, 0, len(signals.Names()))
	for _, name := range signals.Names() {
		names = append(names, string(name))
	}
	return &huma.Schema{
		Type:        huma.TypeObject,
		Description: "Compares one signal of the customer's period with a value, signal first.",
		Properties: map[string]*huma.Schema{
			"signal":   {Type: huma.TypeString, Enum: names, Description: "Signal to compare."},
			"operator": {Type: huma.TypeString, Enum: []any{"lt", "lte", "gt", "gte", "eq", "ne"}, Description: "Comparison of the signal with the value."},
			"value":    {Type: huma.TypeString, Description: conditionValueDescription},
		},
		Required:             []string{"signal", "operator", "value"},
		AdditionalProperties: false,
	}
}

func (policyAction) TransformSchema(huma.Registry, *huma.Schema) *huma.Schema {
	return &huma.Schema{
		Type:        huma.TypeObject,
		Description: "What the policy decides when it matches.",
		Properties: map[string]*huma.Schema{
			"outcome": {
				Type:        huma.TypeString,
				Enum:        []any{string(OutcomeAllow), string(OutcomeRoute), string(OutcomeCap), string(OutcomeDeny)},
				Description: "allow runs the request, route runs it on the first route chain target that can be priced, cap applies overrides or a limit, deny rejects it.",
			},
			"route_chain": {
				Type:        huma.TypeArray,
				Nullable:    true,
				Items:       routeTargetSchema(),
				MinItems:    new(1),
				MaxItems:    new(routeChainMaximumLength),
				Description: "Provider models in order of preference for outcome route, null otherwise. A model alias is stored as the model it names.",
			},
			"overrides": {
				Type:                 huma.TypeObject,
				Nullable:             true,
				AdditionalProperties: overrideValueSchema(),
				Description:          "Parameter values to apply for outcomes route and cap, keyed by the parameter names of GET /api/v1/policies/parameter-mappings. A route override must suit every target, a cap override at least one model.",
			},
			"limit": {
				Type:     huma.TypeObject,
				Nullable: true,
				Properties: map[string]*huma.Schema{
					"kind":  {Type: huma.TypeString, Enum: []any{string(LimitKindCount), string(LimitKindAmount)}, Description: "count limits decisions, amount limits cost."},
					"value": {Type: huma.TypeString, Description: "Most decisions per period as a whole number, or most cost per period in USD, such as 25.00."},
				},
				Required:             []string{"kind", "value"},
				AdditionalProperties: false,
				Description:          "Per period limit of outcome cap for the policy's feature, or for every feature when it has none. Null otherwise.",
			},
		},
		Required:             []string{"outcome"},
		AdditionalProperties: false,
	}
}

func routeTargetSchema() *huma.Schema {
	return &huma.Schema{
		Type: huma.TypeObject,
		Properties: map[string]*huma.Schema{
			"provider": {Type: huma.TypeString, MinLength: new(1), MaxLength: new(routeTargetNameMaximumLength), Description: "Provider name, such as fal_ai."},
			"model":    {Type: huma.TypeString, MinLength: new(1), MaxLength: new(routeTargetNameMaximumLength), Description: "Model name or alias, such as fal-ai/veo3.1/lite."},
		},
		Required:             []string{"provider", "model"},
		AdditionalProperties: false,
	}
}

func overrideValueSchema() *huma.Schema {
	return &huma.Schema{OneOf: []*huma.Schema{
		{Type: huma.TypeString},
		{Type: huma.TypeInteger, Format: "int64"},
		{Type: huma.TypeBoolean},
	}}
}
