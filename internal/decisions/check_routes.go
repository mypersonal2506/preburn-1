package decisions

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
)

const decisionsTag = "Decisions"

type checkRoutes struct {
	service *CheckService
}

type checkInput struct {
	Body CheckRequest
}

type checkOutput struct {
	Body CheckResponse
}

// RegisterCheckRoutes adds POST /api/v1/check of service to api on the
// runtime group. It acts in the environment of the API key. The handler
// reads service only when it runs.
func RegisterCheckRoutes(api *httpapi.API, service *CheckService) {
	routes := &checkRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID: "check",
		Method:      http.MethodPost,
		Path:        "/api/v1/check",
		Summary:     "Check a request",
		Description: "Decides whether the customer's provider request runs as asked, on another model, with capped parameters, or not at all, and reserves its estimated cost against the customer's allowance. Report the usage or release the decision afterwards. Returns 503 counters_unavailable or database_unavailable when Preburn cannot decide, and the SDK falls back.",
		Tags:        []string{decisionsTag},
	}, routes.check)
}

func (routes *checkRoutes) check(ctx context.Context, input *checkInput) (*checkOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	response, err := routes.service.Check(ctx, environment, input.Body)
	if err != nil {
		return nil, err
	}
	return &checkOutput{Body: response}, nil
}
