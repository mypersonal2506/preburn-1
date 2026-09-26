package dashboard

import (
	"context"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/policies"
)

const (
	lastEventIDHeaderLocation = "header.Last-Event-ID"
	lastEventIDQueryLocation  = "query.last_event_id"
	streamIDRule              = "expected a stream id from an earlier event, such as 1726488000000-0"
)

// DecisionEventResponse is the data of a decision event of the decision
// stream: the summary of a decision a check just made.
type DecisionEventResponse struct {
	ID                  string           `json:"id" doc:"Decision id, such as dec_01jbvagescfn78y0938nkrkayd."`
	CreatedAt           time.Time        `json:"created_at" doc:"When the check decided."`
	CustomerID          string           `json:"customer_id" doc:"Customer id, such as cust_01jbvagescfn78y0938nkrkayd."`
	CustomerExternalID  string           `json:"customer_external_id" doc:"Id of the customer in your system."`
	CustomerDisplayName *string          `json:"customer_display_name" doc:"Name the dashboard shows. Null when the customer has none."`
	Feature             string           `json:"feature" doc:"Feature the request serves."`
	RequestedModel      string           `json:"requested_model" doc:"Model the check asked for."`
	Model               string           `json:"model" doc:"Model the decision runs the request on, another model than requested_model when routed."`
	Outcome             policies.Outcome `json:"outcome" enum:"allow,route,cap,deny" doc:"Outcome of the decision."`
	Reason              policies.Reason  `json:"reason" enum:"no_policy_matched,policy_matched,hard_limit_reached,route_chain_exhausted,uncosted_allowed,uncosted_denied,cap_not_applicable" doc:"Why the check decided the outcome."`
	EstimatedCost       *string          `json:"estimated_cost" doc:"Estimated cost of the request the decision runs in USD. Null when it could not be priced."`
	MatchedPolicyID     *string          `json:"matched_policy_id" doc:"Policy that decided the outcome, such as pol_01jbvagescfn78y0938nkrkayd. Null when none matched."`
}

type streamRoutes struct {
	service *StreamService
}

type decisionStreamInput struct {
	Environment       httpapi.Environment `query:"environment" required:"true" enum:"test,live" doc:"Environment to stream, test or live. EventSource cannot send the X-Preburn-Environment header."`
	LastEventID       string              `query:"last_event_id" doc:"Stream id of the last event received. The stream resumes after it. Omit it to start with the next decision."`
	LastEventIDHeader string              `header:"Last-Event-ID" doc:"Stream id EventSource sends when it reconnects. It takes precedence over last_event_id."`
}

// RegisterStreamRoutes adds GET /api/v1/dashboard/decisions/stream of service
// to api on the dashboard group. It relays the decision stream of the
// environment its environment query parameter names as server-sent events:
// one decision event per decision, with the stream id as the event id and a
// DecisionEventResponse as the data, and a comment heartbeat every heartbeat
// interval of service. It resumes after the Last-Event-ID header or else the
// last_event_id query parameter, and starts with the next decision without
// either. It registers with httpapi.RegisterEventStream, so the member
// session acts in the environment of the query. The stream ends at the first
// heartbeat after the member session ends, and its heartbeats never extend
// the session. A missing or
// unknown environment and a malformed stream id return a 422
// validation_failed problem. The handler reads service only when it runs.
func RegisterStreamRoutes(api *httpapi.API, service *StreamService) {
	routes := &streamRoutes{service: service}
	httpapi.RegisterEventStream(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "stream-dashboard-decisions",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/decisions/stream",
		Summary:     "Stream decisions",
		Description: "Sends each new decision of the environment as a server-sent event named decision, with the stream id as the event id, and a comment heartbeat every 15 seconds. The stream ends at the first heartbeat after the session ends, such as by a logout or a removal of the member.",
		Tags:        []string{dashboardTag},
		Responses: map[string]*huma.Response{
			strconv.Itoa(http.StatusOK): {
				Description: "Server-sent events until the client disconnects or the session ends.",
				Content: map[string]*huma.MediaType{
					eventStreamContentType: {Schema: decisionEventSchema()},
				},
			},
		},
	}, routes.stream)
}

func (routes *streamRoutes) stream(ctx context.Context, input *decisionStreamInput) (*huma.StreamResponse, error) {
	lastEventID, err := input.resumeID()
	if err != nil {
		return nil, err
	}
	startID, err := routes.service.startID(ctx, input.Environment, lastEventID)
	if err != nil {
		return nil, err
	}
	return &huma.StreamResponse{Body: func(streamContext huma.Context) {
		request, writer := humago.Unwrap(streamContext)
		routes.service.relay(ctx, request, input.Environment, startID, writer)
	}}, nil
}

func (input *decisionStreamInput) resumeID() (string, error) {
	switch {
	case input.LastEventIDHeader != "":
		return input.LastEventIDHeader, requireStreamID(input.LastEventIDHeader, lastEventIDHeaderLocation)
	case input.LastEventID != "":
		return input.LastEventID, requireStreamID(input.LastEventID, lastEventIDQueryLocation)
	}
	return "", nil
}

func requireStreamID(value string, location string) error {
	milliseconds, sequence, found := strings.Cut(value, "-")
	if found && isStreamIDPart(milliseconds) && isStreamIDPart(sequence) {
		return nil
	}
	return httpapi.NewValidationProblem(httpapi.ProblemError{Location: location, Message: streamIDRule})
}

func isStreamIDPart(part string) bool {
	_, err := strconv.ParseUint(part, 10, 64)
	return err == nil
}

func decisionEventSchema() *huma.Schema {
	registry := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	return &huma.Schema{
		Type:        huma.TypeObject,
		Description: "One server-sent event. Comment lines are heartbeats.",
		Properties: map[string]*huma.Schema{
			"id":    {Type: huma.TypeString, Description: "Stream id of the decision, such as 1726488000000-0. Send it as Last-Event-ID or last_event_id to resume after it."},
			"event": {Type: huma.TypeString, Enum: []any{decisionEventName}, Description: "Event name."},
			"data":  registry.Schema(reflect.TypeFor[DecisionEventResponse](), false, ""),
		},
		Required: []string{"id", "event", "data"},
	}
}
