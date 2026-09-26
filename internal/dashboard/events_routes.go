package dashboard

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/policies"
)

// ActivityEventResponse is one event of the dashboard event list: a
// decision or a ledger entry.
type ActivityEventResponse struct {
	Kind                EventKind         `json:"kind" enum:"decision,ledger_entry" doc:"Record behind the event."`
	ID                  string            `json:"id" doc:"Decision id, such as dec_01jbvagescfn78y0938nkrkayd, or ledger entry id, such as led_01jbvagescfn78y0938nkrkayd."`
	OccurredAt          time.Time         `json:"occurred_at" doc:"When the check decided, or when the reported request ran."`
	CustomerID          string            `json:"customer_id" doc:"Customer id, such as cust_01jbvagescfn78y0938nkrkayd."`
	CustomerExternalID  string            `json:"customer_external_id" doc:"Id of the customer in your system."`
	CustomerDisplayName *string           `json:"customer_display_name" doc:"Name the dashboard shows. Null when the customer has none."`
	Feature             string            `json:"feature" doc:"Feature the request served."`
	Provider            string            `json:"provider" doc:"Provider the request runs or ran on."`
	Model               string            `json:"model" doc:"Model the request runs or ran on."`
	Outcome             *policies.Outcome `json:"outcome" enum:"allow,route,cap,deny" doc:"Outcome of a decision. Null for a ledger entry."`
	Cost                *string           `json:"cost" doc:"Estimated cost of a decision, or AI cost of a ledger entry, in USD. Null when it could not be priced."`
	DecisionID          *string           `json:"decision_id" doc:"Decision whose usage a ledger entry records. Null for a decision and for usage reported without one."`
}

type eventRoutes struct {
	service *EventService
}

type listEventsInput struct {
	Cursor string `query:"cursor" doc:"Cursor from next_cursor of the previous page. Omit it for the first page."`
	Limit  int    `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

// RegisterEventRoutes adds GET /api/v1/dashboard/events of service to api on
// the dashboard group. It acts in the environment of the member session. The
// handler reads service only when it runs.
func RegisterEventRoutes(api *httpapi.API, service *EventService) {
	routes := &eventRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "list-dashboard-events",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/events",
		Summary:     "List recent events",
		Description: "Returns the decisions and ledger entries of the environment merged by time, newest first.",
		Tags:        []string{dashboardTag},
	}, routes.list)
}

// TransformSchema lists null among the values of outcome, because Huma
// leaves null out of the enum of a nullable field.
func (ActivityEventResponse) TransformSchema(_ huma.Registry, schema *huma.Schema) *huma.Schema {
	outcome := schema.Properties["outcome"]
	outcome.Enum = append(outcome.Enum, nil)
	return schema
}

func (routes *eventRoutes) list(ctx context.Context, input *listEventsInput) (*httpapi.Page[ActivityEventResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	events, nextCursor, err := routes.service.List(ctx, environment, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	return httpapi.NewPage(events, nextCursor), nil
}
