package policies

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
)

const (
	policiesTag        = "Policies"
	planFilterLocation = "query.plan_id"
	planFilterRule     = "expected a plan id, such as pln_01jbvagescfn78y0938nkrkayd"
)

// DocumentResponse is the API representation of a policy document, the
// fields a client writes.
type DocumentResponse struct {
	Name          string         `json:"name" doc:"Name of the policy, 1 to 120 characters."`
	Level         Level          `json:"level" enum:"everyone,plan,customer" doc:"Who the policy applies to: every customer, the customers of plan_id, or the customer customer_id."`
	PlanID        *string        `json:"plan_id" doc:"Plan id, such as pln_01jbvagescfn78y0938nkrkayd, for level plan and null otherwise. Customers without a plan follow the default plan."`
	CustomerID    *string        `json:"customer_id" doc:"Customer id, such as cust_01jbvagescfn78y0938nkrkayd, for level customer and null otherwise."`
	Feature       *string        `json:"feature" doc:"Feature the policy applies to, matching ^[a-z][a-z0-9_]{0,63}$, or null for every feature."`
	When          ConditionGroup `json:"when" doc:"Conditions on the customer's signals under which the policy matches."`
	Action        Action         `json:"action" doc:"What the policy decides when it matches."`
	Enforcement   Enforcement    `json:"enforcement" enum:"soft,hard" doc:"soft compares the signals once per check. hard reserves the ceiling estimate and re-checks allowance_remaining and limits atomically."`
	OnUnreachable Outcome        `json:"on_unreachable" enum:"allow,deny" doc:"Outcome the SDK uses when it cannot reach Preburn, returned in every check response as fallback_outcome."`
	OnUncosted    Outcome        `json:"on_uncosted" enum:"allow,deny" doc:"Outcome for a request that cannot be priced."`
	Status        Status         `json:"status" enum:"active,disabled,archived" doc:"Only active policies take part in checks. Creating a policy without a status makes it active."`
}

// PolicyResponse is the API representation of a policy.
type PolicyResponse struct {
	ID string `json:"id" doc:"Policy id, such as pol_01jbvagescfn78y0938nkrkayd."`
	DocumentResponse
	Version   int32     `json:"version" doc:"Starts at 1 and increments on every update."`
	CreatedAt time.Time `json:"created_at" doc:"When the policy was created."`
	UpdatedAt time.Time `json:"updated_at" doc:"When the policy last changed. The latest change wins a tie between policies of one level and outcome."`
}

// ParameterMappingsResponse is the API representation of the parameter
// mappings of the catalog.
type ParameterMappingsResponse struct {
	Models []ModelParametersResponse `json:"models" nullable:"false" doc:"Every provider model with overridable parameters, ordered by provider and model."`
}

// ModelParametersResponse is the API representation of the overridable
// parameters of one provider model.
type ModelParametersResponse struct {
	Provider   string                       `json:"provider" doc:"Provider name, such as fal_ai."`
	Model      string                       `json:"model" doc:"Model name, such as fal-ai/veo3.1/fast."`
	Parameters map[string]ParameterResponse `json:"parameters" doc:"Overridable parameters by the name policy overrides use. Empty when the model has none."`
}

// ParameterResponse is the API representation of one overridable parameter.
type ParameterResponse struct {
	ProviderParameter string                 `json:"provider_parameter" doc:"Name of the parameter in the provider's API."`
	ValueType         catalogfiles.ValueType `json:"value_type" enum:"string,integer,boolean" doc:"Type of the parameter's value."`
	AllowedValues     []OverrideValue        `json:"allowed_values" nullable:"false" doc:"Values the parameter takes. Empty for a boolean and for an integer with a minimum and maximum."`
	Minimum           *int64                 `json:"minimum" doc:"Lowest value of an integer parameter with a range, null otherwise."`
	Maximum           *int64                 `json:"maximum" doc:"Highest value of an integer parameter with a range, null otherwise."`
	Effect            catalogfiles.Effect    `json:"effect" enum:"sets,bounds,prices" doc:"sets sets the meter's quantity, bounds caps it and prices selects the rule that prices it."`
	Meter             string                 `json:"meter" doc:"Meter whose quantity or price the parameter changes."`
	Quantities        map[string]string      `json:"quantities" doc:"Quantity of the meter each value of a string parameter sets, such as 4 for 4s. Null for other parameters."`
}

type policyRoutes struct {
	service *Service
}

type policyOutput struct {
	Body PolicyResponse
}

type previewOutput struct {
	Body PreviewResult
}

type parameterMappingsOutput struct {
	Body ParameterMappingsResponse
}

type listPoliciesInput struct {
	Status Status `query:"status" enum:"active,disabled,archived" doc:"Lists only policies with this status. Omit it for every status."`
	PlanID string `query:"plan_id" doc:"Lists only the plan level policies of this plan, such as pln_01jbvagescfn78y0938nkrkayd. An id that is not a plan of the environment lists none. Omit it for every level."`
	Cursor string `query:"cursor" doc:"Cursor from next_cursor of the previous page. Omit it for the first page."`
	Limit  int    `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

type documentInput struct {
	Body documentRequest
}

type policyPathInput struct {
	PolicyID string `path:"policy_id" doc:"Policy id, such as pol_01jbvagescfn78y0938nkrkayd."`
}

type updatePolicyInput struct {
	PolicyID string `path:"policy_id" doc:"Policy id, such as pol_01jbvagescfn78y0938nkrkayd."`
	Body     documentChangesRequest
}

// NewDocumentResponse returns the API representation of document.
func NewDocumentResponse(document Document) DocumentResponse {
	response := DocumentResponse{
		Name:          document.Name,
		Level:         document.Scope.Level,
		Feature:       document.Feature,
		When:          document.When,
		Action:        document.Action,
		Enforcement:   document.Enforcement,
		OnUnreachable: document.OnUnreachable,
		OnUncosted:    document.OnUncosted,
		Status:        document.Status,
	}
	if document.Scope.PlanID != nil {
		planID := identifiers.Encode(identifiers.PrefixPlan, *document.Scope.PlanID)
		response.PlanID = &planID
	}
	if document.Scope.CustomerID != nil {
		customerID := identifiers.Encode(identifiers.PrefixCustomer, *document.Scope.CustomerID)
		response.CustomerID = &customerID
	}
	return response
}

// NewPolicyResponse returns the API representation of policy.
func NewPolicyResponse(policy Policy) PolicyResponse {
	return PolicyResponse{
		ID:               identifiers.Encode(identifiers.PrefixPolicy, policy.ID),
		DocumentResponse: NewDocumentResponse(policy.Document),
		Version:          policy.Version,
		CreatedAt:        policy.CreatedAt,
		UpdatedAt:        policy.UpdatedAt,
	}
}

// RegisterRoutes adds the policy routes of service to api on the admin
// group: GET /api/v1/policies, POST /api/v1/policies, GET and PATCH
// /api/v1/policies/{policy_id}, POST /api/v1/policies/preview and GET
// /api/v1/policies/parameter-mappings. They act in the environment of the
// admin key or member session. Handlers read service only when they run.
func RegisterRoutes(api *httpapi.API, service *Service) {
	routes := &policyRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-policies",
		Method:      http.MethodGet,
		Path:        "/api/v1/policies",
		Summary:     "List policies",
		Description: "Returns the policies of the environment, newest first. status and plan_id narrow the list.",
		Tags:        []string{policiesTag},
	}, routes.list)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID:      "create-policy",
		Method:           http.MethodPost,
		Path:             "/api/v1/policies",
		Summary:          "Create a policy",
		Description:      "Creates version 1 of a policy in the environment. An invalid document returns 422 policy_invalid with one error per failing field.",
		Tags:             []string{policiesTag},
		DefaultStatus:    http.StatusCreated,
		SkipValidateBody: true,
	}, routes.create)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "get-policy",
		Method:      http.MethodGet,
		Path:        "/api/v1/policies/{policy_id}",
		Summary:     "Get a policy",
		Description: "Returns the current version of the policy.",
		Tags:        []string{policiesTag},
	}, routes.get)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID:      "update-policy",
		Method:           http.MethodPatch,
		Path:             "/api/v1/policies/{policy_id}",
		Summary:          "Update a policy",
		Description:      "Replaces each field the request holds, when and action as a whole, and increments the version. null clears plan_id, customer_id and feature. An invalid result returns 422 policy_invalid.",
		Tags:             []string{policiesTag},
		SkipValidateBody: true,
	}, routes.update)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID:      "preview-policy",
		Method:           http.MethodPost,
		Path:             "/api/v1/policies/preview",
		Summary:          "Preview a policy",
		Description:      fmt.Sprintf("Counts the active customers a draft policy would match now, without storing it. An environment with more than %d active customers returns 422 preview_too_large.", PreviewCustomerMaximum),
		Tags:             []string{policiesTag},
		SkipValidateBody: true,
	}, routes.preview)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "get-parameter-mappings",
		Method:      http.MethodGet,
		Path:        "/api/v1/policies/parameter-mappings",
		Summary:     "Get the parameter mappings",
		Description: "Returns the parameters that route and cap overrides can set for each provider model.",
		Tags:        []string{policiesTag},
	}, routes.parameterMappings)
}

func (routes *policyRoutes) list(ctx context.Context, input *listPoliciesInput) (*httpapi.Page[PolicyResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	var filter ListFilter
	if input.Status != "" {
		filter.Status = &input.Status
	}
	if input.PlanID != "" {
		planID, err := identifiers.Decode(identifiers.PrefixPlan, input.PlanID)
		if err != nil {
			return nil, httpapi.NewValidationProblem(httpapi.ProblemError{Location: planFilterLocation, Message: planFilterRule})
		}
		filter.PlanID = &planID
	}
	policies, nextCursor, err := routes.service.List(ctx, environment, filter, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	responses := make([]PolicyResponse, 0, len(policies))
	for _, policy := range policies {
		responses = append(responses, NewPolicyResponse(policy))
	}
	return httpapi.NewPage(responses, nextCursor), nil
}

func (routes *policyRoutes) create(ctx context.Context, input *documentInput) (*policyOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	policy, err := routes.service.Create(ctx, environment, input.Body.document)
	if err != nil {
		return nil, err
	}
	return &policyOutput{Body: NewPolicyResponse(policy)}, nil
}

func (routes *policyRoutes) get(ctx context.Context, input *policyPathInput) (*policyOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	policyID, err := identifiers.Decode(identifiers.PrefixPolicy, input.PolicyID)
	if err != nil {
		return nil, httpapi.ErrNotFound
	}
	policy, err := routes.service.Get(ctx, environment, policyID)
	if err != nil {
		return nil, err
	}
	return &policyOutput{Body: NewPolicyResponse(policy)}, nil
}

func (routes *policyRoutes) update(ctx context.Context, input *updatePolicyInput) (*policyOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	policyID, err := identifiers.Decode(identifiers.PrefixPolicy, input.PolicyID)
	if err != nil {
		return nil, httpapi.ErrNotFound
	}
	policy, err := routes.service.Update(ctx, environment, policyID, input.Body.changes)
	if err != nil {
		return nil, err
	}
	return &policyOutput{Body: NewPolicyResponse(policy)}, nil
}

func (routes *policyRoutes) preview(ctx context.Context, input *documentInput) (*previewOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := routes.service.Preview(ctx, environment, input.Body.document)
	if err != nil {
		return nil, err
	}
	return &previewOutput{Body: result}, nil
}

func (routes *policyRoutes) parameterMappings(context.Context, *struct{}) (*parameterMappingsOutput, error) {
	return &parameterMappingsOutput{Body: newParameterMappingsResponse(routes.service.ParameterMappings())}, nil
}

func newParameterMappingsResponse(parameterMappings map[catalogfiles.ModelKey]map[string]catalogfiles.Parameter) ParameterMappingsResponse {
	modelKeys := slices.SortedFunc(maps.Keys(parameterMappings), func(first catalogfiles.ModelKey, second catalogfiles.ModelKey) int {
		return cmp.Or(cmp.Compare(first.Provider, second.Provider), cmp.Compare(first.Model, second.Model))
	})
	models := make([]ModelParametersResponse, 0, len(modelKeys))
	for _, modelKey := range modelKeys {
		parameters := make(map[string]ParameterResponse, len(parameterMappings[modelKey]))
		for name, parameter := range parameterMappings[modelKey] {
			parameters[name] = newParameterResponse(parameter)
		}
		models = append(models, ModelParametersResponse{Provider: modelKey.Provider, Model: modelKey.Model, Parameters: parameters})
	}
	return ParameterMappingsResponse{Models: models}
}

func newParameterResponse(parameter catalogfiles.Parameter) ParameterResponse {
	response := ParameterResponse{
		ProviderParameter: parameter.ProviderParameter,
		ValueType:         parameter.ValueType,
		AllowedValues:     []OverrideValue{},
		Minimum:           parameter.Minimum,
		Maximum:           parameter.Maximum,
		Effect:            parameter.Effect,
		Meter:             parameter.Meter,
	}
	for _, value := range parameter.AllowedStrings {
		response.AllowedValues = append(response.AllowedValues, OverrideValue{Type: catalogfiles.ValueTypeString, String: value})
	}
	for _, value := range parameter.AllowedIntegers {
		response.AllowedValues = append(response.AllowedValues, OverrideValue{Type: catalogfiles.ValueTypeInteger, Integer: value})
	}
	if parameter.Quantities != nil {
		response.Quantities = make(map[string]string, len(parameter.Quantities))
		for value, quantity := range parameter.Quantities {
			response.Quantities[value] = money.FormatQuantity(quantity)
		}
	}
	return response
}
