package dashboard

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
)

// KnownFeatureResponse is a feature the environment knows and where it
// appears.
type KnownFeatureResponse struct {
	Feature string          `json:"feature" doc:"Feature name, such as text_to_video."`
	Sources []FeatureSource `json:"sources" nullable:"false" enum:"decisions,plan_hold_times,policies,usage_estimates" doc:"Where the feature appears: decisions of the last 30 days, hold times of an active plan, an active policy or a usage estimate."`
}

type featureRoutes struct {
	service *FeatureService
}

// RegisterFeatureRoutes adds GET /api/v1/dashboard/features of service to
// api on the dashboard group. It acts in the environment of the member
// session. The handler reads service only when it runs.
func RegisterFeatureRoutes(api *httpapi.API, service *FeatureService) {
	routes := &featureRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "list-dashboard-features",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/features",
		Summary:     "List known features",
		Description: "Returns every feature that the usage estimates, active policies, active plan hold times or the decisions of the last 30 days of the environment name, in one page.",
		Tags:        []string{dashboardTag},
	}, routes.list)
}

func (routes *featureRoutes) list(ctx context.Context, _ *struct{}) (*httpapi.Page[KnownFeatureResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	features, err := routes.service.Features(ctx, environment)
	if err != nil {
		return nil, err
	}
	return httpapi.NewPage(features, ""), nil
}
