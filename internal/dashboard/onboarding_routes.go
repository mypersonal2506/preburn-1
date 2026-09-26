package dashboard

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
)

// OnboardingResponse is the state of the get started checklist of an
// environment.
type OnboardingResponse struct {
	HasAPIKey    bool       `json:"has_api_key" doc:"True when the environment has an active API key."`
	FirstCheckAt *time.Time `json:"first_check_at" doc:"When the earliest stored decision of the environment was checked. Null before the first check."`
	HasPlan      bool       `json:"has_plan" doc:"True when the environment has an active plan."`
	HasPolicy    bool       `json:"has_policy" doc:"True when the environment has an active policy."`
	HasRevenue   bool       `json:"has_revenue" doc:"True when the environment has recorded revenue."`
}

type onboardingRoutes struct {
	service *OnboardingService
}

type onboardingOutput struct {
	Body OnboardingResponse
}

// RegisterOnboardingRoutes adds GET /api/v1/dashboard/onboarding of service
// to api on the dashboard group. It acts in the environment of the member
// session. The handler reads service only when it runs.
func RegisterOnboardingRoutes(api *httpapi.API, service *OnboardingService) {
	routes := &onboardingRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "get-dashboard-onboarding",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/onboarding",
		Summary:     "Get the checklist state",
		Description: "Returns which get started steps the environment has done: an API key, a first check, a plan, a policy and revenue.",
		Tags:        []string{dashboardTag},
	}, routes.get)
}

func (routes *onboardingRoutes) get(ctx context.Context, _ *struct{}) (*onboardingOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	onboarding, err := routes.service.Onboarding(ctx, environment)
	if err != nil {
		return nil, err
	}
	return &onboardingOutput{Body: onboarding}, nil
}
