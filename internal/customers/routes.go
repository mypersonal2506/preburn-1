package customers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/plans"
)

const customersTag = "Customers"

// CustomerResponse is the API representation of a customer.
type CustomerResponse struct {
	ID          string                     `json:"id" doc:"Customer id, such as cust_01jbvagescfn78y0938nkrkayd."`
	ExternalID  string                     `json:"external_id" doc:"Id of the customer in your system, unique within the environment."`
	DisplayName *string                    `json:"display_name" doc:"Name the dashboard shows. Null when the customer has none."`
	PlanID      *string                    `json:"plan_id" doc:"Plan id, such as pln_01jbvagescfn78y0938nkrkayd. Null when the default plan of the environment applies."`
	Metadata    map[string]json.RawMessage `json:"metadata" doc:"JSON object attached to the customer."`
	Status      Status                     `json:"status" enum:"active,disabled,archived" doc:"Record status of the customer."`
	CreatedAt   time.Time                  `json:"created_at" doc:"When the customer was created."`
	UpdatedAt   time.Time                  `json:"updated_at" doc:"When the customer was last replaced."`
}

type customerRoutes struct {
	service *Service
}

type upsertCustomerInput struct {
	ExternalID string `path:"external_id" doc:"Id of the customer in your system: 1 to 128 letters, digits or the characters . _ : @ -."`
	Body       upsertCustomerRequest
}

type upsertCustomerRequest struct {
	DisplayName *string                    `json:"display_name,omitempty" nullable:"true" doc:"Name the dashboard shows, 1 to 200 characters. Omitted or null leaves the customer without one."`
	PlanID      *string                    `json:"plan_id,omitempty" nullable:"true" doc:"Id of an active plan of the environment. Omitted or null applies the default plan of the environment."`
	Metadata    map[string]json.RawMessage `json:"metadata,omitempty" doc:"JSON object of at most 50 keys and 4096 bytes. Omitted stores an empty object."`
}

type customerOutput struct {
	Body CustomerResponse
}

// RegisterRoutes adds PUT /api/v1/customers/{external_id} of service to api
// on the runtime group. It acts in the environment of the API key. Handlers
// read service only when they run.
func RegisterRoutes(api *httpapi.API, service *Service) {
	routes := &customerRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID: "upsert-customer",
		Method:      http.MethodPut,
		Path:        "/api/v1/customers/{external_id}",
		Summary:     "Create or replace a customer",
		Description: "Creates the customer with the external id, or replaces the display name, plan and metadata of the existing one. Fields left out are cleared.",
		Tags:        []string{customersTag},
	}, routes.upsert)
}

func (routes *customerRoutes) upsert(ctx context.Context, input *upsertCustomerInput) (*customerOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	planID, err := decodePlanID(input.Body.PlanID)
	if err != nil {
		return nil, err
	}
	customer, err := routes.service.Upsert(ctx, environment, input.ExternalID, UpsertInput{
		DisplayName: input.Body.DisplayName,
		PlanID:      planID,
		Metadata:    input.Body.Metadata,
	})
	if err != nil {
		return nil, err
	}
	return &customerOutput{Body: newCustomerResponse(customer)}, nil
}

func decodePlanID(exposedPlanID *string) (*uuid.UUID, error) {
	if exposedPlanID == nil {
		return nil, nil
	}
	planID, err := identifiers.Decode(identifiers.PrefixPlan, *exposedPlanID)
	if err != nil {
		return nil, plans.ErrPlanNotFound
	}
	return &planID, nil
}

func newCustomerResponse(customer Customer) CustomerResponse {
	response := CustomerResponse{
		ID:          identifiers.Encode(identifiers.PrefixCustomer, customer.ID),
		ExternalID:  customer.ExternalID,
		DisplayName: customer.DisplayName,
		Metadata:    customer.Metadata,
		Status:      customer.Status,
		CreatedAt:   customer.CreatedAt,
		UpdatedAt:   customer.UpdatedAt,
	}
	if customer.PlanID != nil {
		planID := identifiers.Encode(identifiers.PrefixPlan, *customer.PlanID)
		response.PlanID = &planID
	}
	return response
}
