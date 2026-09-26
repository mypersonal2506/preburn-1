package pricing

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
)

type uncostedRoutes struct {
	service *Service
}

type listUncostedInput struct {
	Cursor string `query:"cursor" doc:"Cursor from next_cursor of the previous page. Omit it for the first page."`
	Limit  int    `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

// RegisterUncostedRoutes adds GET /api/v1/pricing/uncosted of service to api
// on the admin group. It acts in the environment of the admin key or member
// session. The handler reads service only when it runs.
func RegisterUncostedRoutes(api *httpapi.API, service *Service) {
	routes := &uncostedRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-pricing-uncosted",
		Method:      http.MethodGet,
		Path:        "/api/v1/pricing/uncosted",
		Summary:     "List uncosted usage",
		Description: "Returns the usage of the last 90 days that no rule or override prices, grouped by provider, model and missing meter, ordered by provider, model and meter. A request with several missing meters counts once for each. Usage that a later price corrected is left out.",
		Tags:        []string{pricingTag},
	}, routes.listUncosted)
}

func (routes *uncostedRoutes) listUncosted(ctx context.Context, input *listUncostedInput) (*httpapi.Page[UncostedUsage], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	usage, nextCursor, err := routes.service.ListUncosted(ctx, environment, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	return httpapi.NewPage(usage, nextCursor), nil
}
