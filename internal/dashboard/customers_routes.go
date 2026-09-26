package dashboard

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/signals"
)

const (
	dashboardTag   = "Dashboard"
	planIDLocation = "query.plan_id"
	planIDRule     = "expected a plan id, such as pln_01jbvagescfn78y0938nkrkayd"
)

// CustomerMarginResponse is one row of the dashboard customer list: an
// active customer with the revenue, cost, margin and pace of its current
// period.
type CustomerMarginResponse struct {
	ID           string    `json:"id" doc:"Customer id, such as cust_01jbvagescfn78y0938nkrkayd."`
	ExternalID   string    `json:"external_id" doc:"Id of the customer in your system."`
	DisplayName  *string   `json:"display_name" doc:"Name the dashboard shows. Null when the customer has none."`
	PlanID       *string   `json:"plan_id" doc:"Effective plan: the customer's plan, else the default plan of the environment. Null when there is neither."`
	PlanName     *string   `json:"plan_name" doc:"Name of the effective plan. Null without one."`
	TargetMargin *string   `json:"target_margin" doc:"Target margin of the effective plan with 4 decimals. Null without a plan and for a fixed allowance plan."`
	Revenue      string    `json:"revenue" doc:"Net revenue attributed to the current period in USD."`
	Cost         string    `json:"cost" doc:"Settled AI cost of the current period in USD."`
	Margin       *string   `json:"margin" doc:"One minus cost divided by revenue, with 4 decimals. Null without revenue above zero."`
	Pace         string    `json:"pace" doc:"Pace signal with 4 decimals, or inf for cost against a zero allowance."`
	PeriodStart  time.Time `json:"period_start" doc:"Start of the current period."`
	PeriodEnd    time.Time `json:"period_end" doc:"End of the current period."`
}

// CustomerDetailResponse is the dashboard view of one customer: the signals
// of its current period, its period history, this period's usage and its
// latest decisions.
type CustomerDetailResponse struct {
	ID              string                     `json:"id" doc:"Customer id, such as cust_01jbvagescfn78y0938nkrkayd."`
	ExternalID      string                     `json:"external_id" doc:"Id of the customer in your system."`
	DisplayName     *string                    `json:"display_name" doc:"Name the dashboard shows. Null when the customer has none."`
	Status          customers.Status           `json:"status" enum:"active,disabled,archived" doc:"Record status of the customer."`
	PlanID          *string                    `json:"plan_id" doc:"Effective plan: the customer's plan, else the default plan of the environment. Null when there is neither."`
	PlanName        *string                    `json:"plan_name" doc:"Name of the effective plan. Null without one."`
	TargetMargin    *string                    `json:"target_margin" doc:"Target margin of the effective plan with 4 decimals. Null without a plan and for a fixed allowance plan."`
	PeriodStart     time.Time                  `json:"period_start" doc:"Start of the current period."`
	PeriodEnd       time.Time                  `json:"period_end" doc:"End of the current period."`
	Signals         signals.Response           `json:"signals" doc:"Signals of the current period, with nothing requested."`
	History         []CustomerPeriodResponse   `json:"history" nullable:"false" doc:"Period rollups of the customer, newest first."`
	Usage           []CustomerUsageResponse    `json:"usage" nullable:"false" doc:"Usage of the current period by feature, provider and model, highest cost first."`
	RecentDecisions []CustomerDecisionResponse `json:"recent_decisions" nullable:"false" doc:"The 10 latest decisions of the customer, newest first."`
}

// CustomerPeriodResponse is one period rollup of a customer.
type CustomerPeriodResponse struct {
	PeriodStart    time.Time              `json:"period_start" doc:"Start of the period."`
	PeriodEnd      time.Time              `json:"period_end" doc:"End of the period."`
	Revenue        string                 `json:"revenue" doc:"Net revenue attributed to the period in USD."`
	Cost           string                 `json:"cost" doc:"AI cost of the period's ledger entries in USD."`
	Margin         *string                `json:"margin" doc:"One minus cost divided by revenue, with 4 decimals. Null without revenue above zero."`
	UncostedCount  int64                  `json:"uncosted_count" doc:"Ledger entries of the period that could not be priced."`
	DecisionCounts DecisionCountsResponse `json:"decision_counts" doc:"Decisions of the period by outcome."`
}

// CustomerUsageResponse is the usage of one feature, provider and model in a
// customer's current period.
type CustomerUsageResponse struct {
	Feature       string `json:"feature" doc:"Feature the requests served."`
	Provider      string `json:"provider" doc:"Provider the requests ran on."`
	Model         string `json:"model" doc:"Model the requests ran on."`
	RequestCount  int64  `json:"request_count" doc:"Requests reported, corrections left out."`
	Cost          string `json:"cost" doc:"AI cost of the requests in USD."`
	UncostedCount int64  `json:"uncosted_count" doc:"Requests that could not be priced and have no correction."`
}

// CustomerDecisionResponse is a decision in the recent decisions of a
// customer.
type CustomerDecisionResponse struct {
	ID                string           `json:"id" doc:"Decision id, such as dec_01jbvagescfn78y0938nkrkayd."`
	Feature           string           `json:"feature" doc:"Feature the request served."`
	RequestedProvider string           `json:"requested_provider" doc:"Provider the check asked for."`
	RequestedModel    string           `json:"requested_model" doc:"Model the check asked for."`
	Provider          string           `json:"provider" doc:"Provider the decision runs the request on."`
	Model             string           `json:"model" doc:"Model the decision runs the request on, another model than requested_model when routed."`
	Outcome           policies.Outcome `json:"outcome" enum:"allow,route,cap,deny" doc:"Outcome of the decision."`
	Reason            policies.Reason  `json:"reason" enum:"no_policy_matched,policy_matched,hard_limit_reached,route_chain_exhausted,uncosted_allowed,uncosted_denied,cap_not_applicable" doc:"Why the check decided the outcome."`
	MatchedPolicyID   *string          `json:"matched_policy_id" doc:"Policy that decided the outcome, such as pol_01jbvagescfn78y0938nkrkayd. Null when none matched."`
	EstimatedCost     *string          `json:"estimated_cost" doc:"Estimated cost of the request the decision runs in USD. Null when it could not be priced."`
	Status            string           `json:"status" enum:"reserved,settled,released,expired,unreserved" doc:"Lifecycle status of the decision."`
	CreatedAt         time.Time        `json:"created_at" doc:"When the check decided."`
}

type customerRoutes struct {
	service *CustomerService
}

type listCustomersInput struct {
	Sort          CustomerSort  `query:"sort" enum:"margin,pace,cost,revenue" default:"margin" doc:"Value the list orders by. Customers without revenue above zero come last in both directions."`
	Direction     SortDirection `query:"direction" enum:"ascending,descending" default:"ascending" doc:"Direction of the order."`
	RevenueFilter RevenueFilter `query:"revenue_filter" enum:"all,paying,free" default:"all" doc:"paying keeps customers with revenue above zero, free the others."`
	PlanID        string        `query:"plan_id" doc:"Keeps the customers whose effective plan is this plan."`
	Search        string        `query:"search" doc:"Keeps the customers whose external id or display name contains this text in any letter case, at most 200 characters."`
	Cursor        string        `query:"cursor" doc:"Cursor from next_cursor of the previous page, with the same sort and direction. Omit it for the first page."`
	Limit         int           `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

type getCustomerInput struct {
	CustomerID string `path:"customer_id" doc:"Customer id, such as cust_01jbvagescfn78y0938nkrkayd."`
}

type customerDetailOutput struct {
	Body CustomerDetailResponse
}

// RegisterCustomerRoutes adds GET /api/v1/dashboard/customers and GET
// /api/v1/dashboard/customers/{customer_id} of service to api on the
// dashboard group. They act in the environment of the member session.
// Handlers read service only when they run.
func RegisterCustomerRoutes(api *httpapi.API, service *CustomerService) {
	routes := &customerRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "list-dashboard-customers",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/customers",
		Summary:     "List customer margins",
		Description: "Returns the active customers of the environment with the revenue, AI cost, margin and pace of their current period.",
		Tags:        []string{dashboardTag},
	}, routes.list)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "get-dashboard-customer",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/customers/{customer_id}",
		Summary:     "Get a customer's margins",
		Description: "Returns the customer with the signals of its current period, its period history, this period's usage by feature and model, and its 10 latest decisions.",
		Tags:        []string{dashboardTag},
	}, routes.get)
}

func (routes *customerRoutes) list(ctx context.Context, input *listCustomersInput) (*httpapi.Page[CustomerMarginResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	filter := CustomerListFilter{Sort: input.Sort, Direction: input.Direction, Revenue: input.RevenueFilter, Search: input.Search}
	if input.PlanID != "" {
		planID, err := identifiers.Decode(identifiers.PrefixPlan, input.PlanID)
		if err != nil {
			return nil, httpapi.NewValidationProblem(httpapi.ProblemError{Location: planIDLocation, Message: planIDRule})
		}
		filter.PlanID = &planID
	}
	margins, nextCursor, err := routes.service.List(ctx, environment, filter, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	return httpapi.NewPage(margins, nextCursor), nil
}

func (routes *customerRoutes) get(ctx context.Context, input *getCustomerInput) (*customerDetailOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	customerID, err := identifiers.Decode(identifiers.PrefixCustomer, input.CustomerID)
	if err != nil {
		return nil, httpapi.ErrNotFound
	}
	detail, err := routes.service.Customer(ctx, environment, customerID)
	if err != nil {
		return nil, err
	}
	return &customerDetailOutput{Body: detail}, nil
}
