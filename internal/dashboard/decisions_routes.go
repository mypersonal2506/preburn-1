package dashboard

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
	"github.com/preburn/preburn/internal/signals"
)

const (
	customerIDLocation = "query.customer_id"
	customerIDRule     = "expected a customer id, such as cust_01jbvagescfn78y0938nkrkayd"
	policyIDLocation   = "query.policy_id"
	policyIDRule       = "expected a policy id, such as pol_01jbvagescfn78y0938nkrkayd"
)

// DecisionResponse is one decision of the dashboard decision list, with the
// customer it was made for.
type DecisionResponse struct {
	CustomerDecisionResponse
	CustomerID          string  `json:"customer_id" doc:"Customer id, such as cust_01jbvagescfn78y0938nkrkayd."`
	CustomerExternalID  string  `json:"customer_external_id" doc:"Id of the customer in your system."`
	CustomerDisplayName *string `json:"customer_display_name" doc:"Name the dashboard shows. Null when the customer has none."`
}

// DecisionDetailResponse is the dashboard view of one decision: the request,
// the signals at check time, the lifecycle times and the ledger entries of
// its usage.
type DecisionDetailResponse struct {
	DecisionResponse
	CustomerUserExternalID *string                       `json:"customer_user_external_id" doc:"Id of the customer's user in your system. Null when the check named none."`
	Attributes             pricing.Attributes            `json:"attributes" doc:"Attributes of the request as the check received them."`
	Overrides              pricing.Attributes            `json:"overrides" doc:"Attributes the decision set on the request, such as duration or audio, by Preburn attribute name. GET /api/v1/policies/parameter-mappings names the provider parameter of each attribute, such as generate_audio for audio. Empty unless the outcome is route or cap."`
	MatchedPolicyVersion   *int64                        `json:"matched_policy_version" doc:"Version of the matched policy at the check. Null when none matched."`
	RequestedEstimatedCost *string                       `json:"requested_estimated_cost" doc:"Estimated cost of the request as asked in USD. Null when it could not be priced."`
	ReservedAmount         string                        `json:"reserved_amount" doc:"USD amount the decision held against the customer's allowance."`
	EstimateBasis          decisions.EstimateBasis       `json:"estimate_basis" enum:"p95,ceiling,request_estimate,none" doc:"Usage whose cost the decision reserved."`
	Signals                signals.Response              `json:"signals" doc:"Margin signals of the customer's period at the check."`
	PeriodStart            time.Time                     `json:"period_start" doc:"Start of the customer period the decision counts in."`
	PeriodEnd              time.Time                     `json:"period_end" doc:"End of the customer period the decision counts in."`
	ExpiresAt              time.Time                     `json:"expires_at" doc:"When the reservation is released unless the request is reported first. The check time for a deny."`
	SettledAt              *time.Time                    `json:"settled_at" doc:"When a report settled the decision. Null until then."`
	LedgerEntries          []DecisionLedgerEntryResponse `json:"ledger_entries" nullable:"false" doc:"Usage reported for the decision and the corrections that price it, oldest first."`
}

// DecisionLedgerEntryResponse is a ledger entry of a decision: the usage its
// report recorded, or a correction that prices that usage.
type DecisionLedgerEntryResponse struct {
	ID           string             `json:"id" doc:"Ledger entry id, such as led_01jbvagescfn78y0938nkrkayd."`
	Provider     string             `json:"provider" doc:"Provider the request ran on."`
	Model        string             `json:"model" doc:"Model the request ran on."`
	Usage        map[string]string  `json:"usage" doc:"Quantity per meter, such as 8 for output_seconds."`
	Cost         *string            `json:"cost" doc:"AI cost in USD. Null when the usage could not be priced."`
	CostStatus   pricing.CostStatus `json:"cost_status" enum:"costed,uncosted" doc:"uncosted when a meter of the usage has no price."`
	CorrectionOf *string            `json:"correction_of" doc:"Uncosted entry this entry prices, such as led_01jbvagescfn78y0938nkrkayd. Null for reported usage."`
	OccurredAt   time.Time          `json:"occurred_at" doc:"When the request ran."`
	CreatedAt    time.Time          `json:"created_at" doc:"When the entry was recorded."`
}

type decisionRoutes struct {
	service *DecisionService
}

type listDecisionsInput struct {
	Outcome    policies.Outcome `query:"outcome" enum:"allow,route,cap,deny" doc:"Keeps the decisions with this outcome."`
	CustomerID string           `query:"customer_id" doc:"Keeps the decisions of this customer, such as cust_01jbvagescfn78y0938nkrkayd."`
	Feature    string           `query:"feature" doc:"Keeps the decisions for this feature."`
	PolicyID   string           `query:"policy_id" doc:"Keeps the decisions this policy decided, such as pol_01jbvagescfn78y0938nkrkayd."`
	Cursor     string           `query:"cursor" doc:"Cursor from next_cursor of the previous page. Omit it for the first page."`
	Limit      int              `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

type getDecisionInput struct {
	DecisionID string `path:"decision_id" doc:"Decision id, such as dec_01jbvagescfn78y0938nkrkayd."`
}

type decisionDetailOutput struct {
	Body DecisionDetailResponse
}

// RegisterDecisionRoutes adds GET /api/v1/dashboard/decisions and GET
// /api/v1/dashboard/decisions/{decision_id} of service to api on the
// dashboard group. They act in the environment of the member session.
// Handlers read service only when they run.
func RegisterDecisionRoutes(api *httpapi.API, service *DecisionService) {
	routes := &decisionRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "list-dashboard-decisions",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/decisions",
		Summary:     "List decisions",
		Description: "Returns the decisions of the environment, newest first, filtered by outcome, customer, feature and policy.",
		Tags:        []string{dashboardTag},
	}, routes.list)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "get-dashboard-decision",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/decisions/{decision_id}",
		Summary:     "Get a decision",
		Description: "Returns the decision with the request, the signals at check time, its lifecycle times and the ledger entries of its usage.",
		Tags:        []string{dashboardTag},
	}, routes.get)
}

func (routes *decisionRoutes) list(ctx context.Context, input *listDecisionsInput) (*httpapi.Page[DecisionResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	filter := DecisionFilter{Feature: input.Feature}
	if input.Outcome != "" {
		filter.Outcome = &input.Outcome
	}
	if input.CustomerID != "" {
		customerID, err := identifiers.Decode(identifiers.PrefixCustomer, input.CustomerID)
		if err != nil {
			return nil, httpapi.NewValidationProblem(httpapi.ProblemError{Location: customerIDLocation, Message: customerIDRule})
		}
		filter.CustomerID = &customerID
	}
	if input.PolicyID != "" {
		policyID, err := identifiers.Decode(identifiers.PrefixPolicy, input.PolicyID)
		if err != nil {
			return nil, httpapi.NewValidationProblem(httpapi.ProblemError{Location: policyIDLocation, Message: policyIDRule})
		}
		filter.PolicyID = &policyID
	}
	listed, nextCursor, err := routes.service.List(ctx, environment, filter, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	return httpapi.NewPage(listed, nextCursor), nil
}

func (routes *decisionRoutes) get(ctx context.Context, input *getDecisionInput) (*decisionDetailOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	decisionID, err := identifiers.Decode(identifiers.PrefixDecision, input.DecisionID)
	if err != nil {
		return nil, httpapi.ErrNotFound
	}
	detail, err := routes.service.Decision(ctx, environment, decisionID)
	if err != nil {
		return nil, err
	}
	return &decisionDetailOutput{Body: detail}, nil
}
