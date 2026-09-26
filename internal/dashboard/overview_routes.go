package dashboard

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/policies"
)

const schemaTypeNull = "null"

// OverviewResponse is the dashboard overview of one period option.
type OverviewResponse struct {
	Period         OverviewPeriod         `json:"period" enum:"current,previous,last_30_days" doc:"Period option the overview sums."`
	Revenue        string                 `json:"revenue" doc:"Net revenue in USD."`
	Cost           string                 `json:"cost" doc:"AI cost in USD."`
	Margin         *string                `json:"margin" doc:"Gross margin, one minus cost divided by revenue, with 4 decimals. Null without revenue above zero."`
	TargetMargin   *string                `json:"target_margin" doc:"Target margin of the margin target plans weighted by their revenue, with 4 decimals. Null when no margin target plan has revenue above zero."`
	Daily          []DailyTotalsResponse  `json:"daily" nullable:"false" doc:"Revenue and AI cost per UTC day of the period, oldest first, days without either included."`
	PlanMargins    []PlanMarginResponse   `json:"plan_margins" nullable:"false" doc:"Totals per effective plan of the customers with totals in the period, by plan name, then customers without a plan."`
	LossCustomers  []LossCustomerResponse `json:"loss_customers" nullable:"false" doc:"The 5 customers with revenue above zero and the lowest margin below zero, lowest first."`
	CostAvoided    CostAvoidedResponse    `json:"cost_avoided" doc:"AI cost that decisions kept from running, estimated at check time."`
	DecisionCounts DecisionCountsResponse `json:"decision_counts" doc:"Decisions by outcome."`
	UncostedCount  int64                  `json:"uncosted_count" doc:"Ledger entries that could not be priced and have no correction."`
	Attention      AttentionResponse      `json:"attention" doc:"Counts behind the attention banner, strongest first in field order."`
	PolicyChanges  []PolicyChangeResponse `json:"policy_changes" nullable:"false" doc:"The 5 most recently changed policies, newest change first, whatever the period."`
}

// DailyTotalsResponse is the revenue and AI cost of one UTC day.
type DailyTotalsResponse struct {
	Date    string `json:"date" format:"date" doc:"UTC day, such as 2026-09-16."`
	Revenue string `json:"revenue" doc:"Net revenue recognized on the day in USD."`
	Cost    string `json:"cost" doc:"AI cost of the requests that ran on the day in USD."`
}

// PlanMarginResponse is the totals of the customers of one effective plan.
type PlanMarginResponse struct {
	PlanID        *string     `json:"plan_id" doc:"Plan id, such as pln_01jbvagescfn78y0938nkrkayd. Null for customers without a plan."`
	Name          *string     `json:"name" doc:"Plan name. Null for customers without a plan."`
	Mode          *plans.Mode `json:"mode" enum:"margin_target,fixed_allowance" doc:"How the plan sets the allowance. Null for customers without a plan."`
	TargetMargin  *string     `json:"target_margin" doc:"Target margin of a margin target plan with 4 decimals. Null for a fixed allowance plan and for customers without a plan."`
	CustomerCount int64       `json:"customer_count" doc:"Customers of the plan with totals in the period."`
	Revenue       string      `json:"revenue" doc:"Net revenue in USD."`
	Cost          string      `json:"cost" doc:"AI cost in USD."`
	Margin        *string     `json:"margin" doc:"One minus cost divided by revenue, with 4 decimals. Null without revenue above zero."`
	BelowTarget   bool        `json:"below_target" doc:"True when the plan has revenue above zero and a margin below its target margin, or below zero for a fixed allowance plan."`
}

// LossCustomerResponse is a customer to watch: a customer with revenue
// above zero and a margin below zero in the period, one of the lowest.
type LossCustomerResponse struct {
	ID                  string                 `json:"id" doc:"Customer id, such as cust_01jbvagescfn78y0938nkrkayd."`
	ExternalID          string                 `json:"external_id" doc:"Id of the customer in your system."`
	DisplayName         *string                `json:"display_name" doc:"Name the dashboard shows. Null when the customer has none."`
	PlanID              *string                `json:"plan_id" doc:"Effective plan of the customer. Null when there is none."`
	Revenue             string                 `json:"revenue" doc:"Net revenue in USD."`
	Cost                string                 `json:"cost" doc:"AI cost in USD."`
	Margin              string                 `json:"margin" doc:"One minus cost divided by revenue, with 4 decimals."`
	LatestPolicyOutcome *PolicyOutcomeResponse `json:"latest_policy_outcome" doc:"Latest decision of the customer that a policy decided, at any time. Null when there is none."`
}

// PolicyOutcomeResponse is the outcome of a decision that a policy decided.
type PolicyOutcomeResponse struct {
	Outcome   policies.Outcome `json:"outcome" enum:"allow,route,cap,deny" doc:"Outcome of the decision."`
	PolicyID  string           `json:"policy_id" doc:"Policy that decided it, such as pol_01jbvagescfn78y0938nkrkayd."`
	DecidedAt time.Time        `json:"decided_at" doc:"When the check decided."`
}

// CostAvoidedResponse is the AI cost that decisions kept from running,
// estimated at check time.
type CostAvoidedResponse struct {
	Total                string `json:"total" doc:"Sum of denied, routed and capped in USD."`
	Denied               string `json:"denied" doc:"Requested estimated cost of denied decisions in USD."`
	Routed               string `json:"routed" doc:"Requested minus served estimated cost of routed decisions, at least zero per decision, in USD."`
	Capped               string `json:"capped" doc:"Requested minus served estimated cost of capped decisions, at least zero per decision, in USD."`
	ChangedDecisionCount int64  `json:"changed_decision_count" doc:"Decisions whose outcome was route, cap or deny."`
}

// DecisionCountsResponse counts decisions by outcome.
type DecisionCountsResponse struct {
	Allow int64 `json:"allow" doc:"Decisions that allowed the request."`
	Route int64 `json:"route" doc:"Decisions that routed the request."`
	Cap   int64 `json:"cap" doc:"Decisions that capped the request."`
	Deny  int64 `json:"deny" doc:"Decisions that denied the request."`
}

// AttentionResponse is the counts behind the overview's attention banner.
type AttentionResponse struct {
	PlansBelowTarget   int64 `json:"plans_below_target" doc:"Plans in plan_margins that are below target."`
	CustomersAbovePace int64 `json:"customers_above_pace" doc:"Active customers whose current pace is above 2.0, whatever the period."`
	DroppedReports     int64 `json:"dropped_reports" doc:"Reports the SDK dropped today and in the 6 UTC days before, whatever the period."`
	UncostedRequests   int64 `json:"uncosted_requests" doc:"Ledger entries of the period that could not be priced and have no correction."`
}

// PolicyChangeResponse is the latest change of a policy.
type PolicyChangeResponse struct {
	ID        string          `json:"id" doc:"Policy id, such as pol_01jbvagescfn78y0938nkrkayd."`
	Name      string          `json:"name" doc:"Policy name."`
	Status    policies.Status `json:"status" enum:"active,disabled,archived" doc:"Status of the policy."`
	Version   int64           `json:"version" doc:"Version of the policy, 1 for a policy never updated."`
	Change    PolicyChange    `json:"change" enum:"created,updated" doc:"created for a policy never updated, updated otherwise."`
	CreatedAt time.Time       `json:"created_at" doc:"When the policy was created."`
	UpdatedAt time.Time       `json:"updated_at" doc:"When the policy last changed."`
}

type overviewRoutes struct {
	service *OverviewService
}

type overviewInput struct {
	Period OverviewPeriod `query:"period" enum:"current,previous,last_30_days" default:"current" doc:"current sums each customer's current period, previous the period before it, last_30_days today and the 29 UTC days before."`
}

type overviewOutput struct {
	Body OverviewResponse
}

// RegisterOverviewRoutes adds GET /api/v1/dashboard/overview of service to
// api on the dashboard group. It acts in the environment of the member
// session. The handler reads service only when it runs.
func RegisterOverviewRoutes(api *httpapi.API, service *OverviewService) {
	routes := &overviewRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "get-dashboard-overview",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/overview",
		Summary:     "Get the overview",
		Description: "Returns revenue, AI cost, margins, cost avoided, decision counts and the attention counts of the environment for one period option.",
		Tags:        []string{dashboardTag},
	}, routes.get)
}

// TransformSchema lists null among the values of mode, because Huma leaves
// null out of the enum of a nullable field.
func (PlanMarginResponse) TransformSchema(_ huma.Registry, schema *huma.Schema) *huma.Schema {
	mode := schema.Properties["mode"]
	mode.Enum = append(mode.Enum, nil)
	return schema
}

// TransformSchema documents latest_policy_outcome as a PolicyOutcomeResponse
// or null, because Huma's nullable tag panics on a field whose schema is a
// reference to an object.
func (LossCustomerResponse) TransformSchema(_ huma.Registry, schema *huma.Schema) *huma.Schema {
	outcome := schema.Properties["latest_policy_outcome"]
	schema.Properties["latest_policy_outcome"] = &huma.Schema{
		Description: outcome.Description,
		OneOf:       []*huma.Schema{{Ref: outcome.Ref}, {Type: schemaTypeNull}},
	}
	return schema
}

func (routes *overviewRoutes) get(ctx context.Context, input *overviewInput) (*overviewOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	overview, err := routes.service.Overview(ctx, environment, input.Period)
	if err != nil {
		return nil, err
	}
	return &overviewOutput{Body: overview}, nil
}
