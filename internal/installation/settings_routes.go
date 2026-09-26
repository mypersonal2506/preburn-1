package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/plans"
)

const settingsTag = "Settings"

// SettingsResponse is the API representation of the installation settings as
// one environment sees them.
type SettingsResponse struct {
	InstallationName          string  `json:"installation_name" doc:"Name of the installation, shared by both environments."`
	DefaultPlanID             *string `json:"default_plan_id" doc:"Plan of this environment's customers that have no plan, or null for none."`
	StripeCustomerMetadataKey string  `json:"stripe_customer_metadata_key" doc:"Stripe customer metadata key that holds the customer's external id in this environment."`
}

type settingsRoutes struct {
	service *SettingsService
}

type settingsOutput struct {
	Body SettingsResponse
}

type updateSettingsInput struct {
	Body updateSettingsRequest
}

type updateSettingsRequest struct {
	InstallationName          *string          `json:"installation_name,omitempty" doc:"New installation name, 1 to 80 characters."`
	DefaultPlanID             defaultPlanField `json:"default_plan_id,omitzero" doc:"Active plan of this environment for customers that have no plan, or null for none. Any other plan id returns 422 plan_not_found."`
	StripeCustomerMetadataKey *string          `json:"stripe_customer_metadata_key,omitempty" doc:"New Stripe customer metadata key of this environment, matching ^[a-z][a-z0-9_]{0,39}$."`
}

type defaultPlanField struct {
	present bool
	planID  *string
}

// RegisterSettingsRoutes adds the settings routes of service to api on the
// dashboard group: GET /api/v1/settings and PATCH /api/v1/settings. They act
// in the environment of the member session. Handlers read service only when
// they run.
func RegisterSettingsRoutes(api *httpapi.API, service *SettingsService) {
	routes := &settingsRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "get-settings",
		Method:      http.MethodGet,
		Path:        "/api/v1/settings",
		Summary:     "Get the settings",
		Description: "Returns the installation name and the default plan and Stripe metadata key of the environment.",
		Tags:        []string{settingsTag},
	}, routes.get)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "update-settings",
		Method:      http.MethodPatch,
		Path:        "/api/v1/settings",
		Summary:     "Update the settings",
		Description: "Changes the fields the request holds. The default plan and the Stripe metadata key belong to the environment, the installation name to both environments.",
		Tags:        []string{settingsTag},
	}, routes.update)
}

func (routes *settingsRoutes) get(ctx context.Context, _ *struct{}) (*settingsOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := routes.service.Get(ctx, environment)
	if err != nil {
		return nil, err
	}
	return &settingsOutput{Body: newSettingsResponse(settings)}, nil
}

func (routes *settingsRoutes) update(ctx context.Context, input *updateSettingsInput) (*settingsOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	update := SettingsUpdate{
		InstallationName:          input.Body.InstallationName,
		ReplaceDefaultPlan:        input.Body.DefaultPlanID.present,
		StripeCustomerMetadataKey: input.Body.StripeCustomerMetadataKey,
	}
	if input.Body.DefaultPlanID.planID != nil {
		planID, err := identifiers.Decode(identifiers.PrefixPlan, *input.Body.DefaultPlanID.planID)
		if err != nil {
			return nil, plans.ErrPlanNotFound
		}
		update.DefaultPlanID = &planID
	}
	settings, err := routes.service.Update(ctx, environment, update)
	if err != nil {
		return nil, err
	}
	return &settingsOutput{Body: newSettingsResponse(settings)}, nil
}

// UnmarshalJSON records that the request holds the field, and its plan id
// unless the value is null.
func (field *defaultPlanField) UnmarshalJSON(data []byte) error {
	field.present = true
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	return json.Unmarshal(data, &field.planID)
}

// Schema documents the field as a string or null.
func (defaultPlanField) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: huma.TypeString, Nullable: true}
}

func newSettingsResponse(settings Settings) SettingsResponse {
	response := SettingsResponse{
		InstallationName:          settings.InstallationName,
		StripeCustomerMetadataKey: settings.StripeCustomerMetadataKey,
	}
	if settings.DefaultPlanID != nil {
		defaultPlanID := identifiers.Encode(identifiers.PrefixPlan, *settings.DefaultPlanID)
		response.DefaultPlanID = &defaultPlanID
	}
	return response
}
