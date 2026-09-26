package plans

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
)

const plansTag = "Plans"

// PlanResponse is the API representation of a plan.
type PlanResponse struct {
	ID            string         `json:"id" doc:"Plan id, such as pln_01jbvagescfn78y0938nkrkayd."`
	Name          string         `json:"name" doc:"Name of the plan, unique in the environment."`
	Mode          Mode           `json:"mode" enum:"margin_target,fixed_allowance" doc:"margin_target allows the period's net revenue times one minus the target margin. fixed_allowance allows a fixed amount per period."`
	TargetMargin  string         `json:"target_margin" doc:"Share of net revenue kept as margin, from 0.0000 to 0.9999. It sets the allowance in margin_target mode and is kept in fixed_allowance mode."`
	Allowance     *string        `json:"allowance" doc:"AI cost allowance per customer period in USD in fixed_allowance mode, null in margin_target mode."`
	HoldTimes     map[string]int `json:"hold_times" doc:"Seconds a check reserves cost, per feature. Other features hold for 10 minutes."`
	Status        Status         `json:"status" enum:"active,archived" doc:"Archived plans keep their customers but take no new ones and cannot be the default plan."`
	CustomerCount int64          `json:"customer_count" doc:"Customers on the plan, counting customers without a plan when it is the default plan."`
	CreatedAt     time.Time      `json:"created_at" doc:"When the plan was created."`
}

type planRoutes struct {
	service *Service
}

type planOutput struct {
	Body PlanResponse
}

type listPlansInput struct {
	Cursor string `query:"cursor" doc:"Cursor from next_cursor of the previous page. Omit it for the first page."`
	Limit  int    `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

type createPlanInput struct {
	Body createPlanRequest
}

type createPlanRequest struct {
	Name         string         `json:"name" doc:"Name of the plan, 1 to 80 characters, unique in the environment."`
	Mode         Mode           `json:"mode" enum:"margin_target,fixed_allowance" doc:"margin_target allows the period's net revenue times one minus the target margin. fixed_allowance allows a fixed amount per period."`
	TargetMargin *string        `json:"target_margin,omitempty" doc:"Share of net revenue kept as margin, from 0 to 0.9999 with at most 4 decimals, such as 0.40. Required in margin_target mode, 0 when omitted in fixed_allowance mode."`
	Allowance    *string        `json:"allowance,omitempty" doc:"AI cost allowance per customer period in USD, such as 2.00. Required in fixed_allowance mode, rejected in margin_target mode."`
	HoldTimes    map[string]int `json:"hold_times,omitempty" doc:"Seconds a check reserves cost, 30 to 86400, per feature. Other features hold for 10 minutes."`
}

type planPathInput struct {
	PlanID string `path:"plan_id" doc:"Plan id, such as pln_01jbvagescfn78y0938nkrkayd."`
}

type updatePlanInput struct {
	PlanID string `path:"plan_id" doc:"Plan id, such as pln_01jbvagescfn78y0938nkrkayd."`
	Body   updatePlanRequest
}

type updatePlanRequest struct {
	Name         *string        `json:"name,omitempty" doc:"New name, 1 to 80 characters, unique in the environment."`
	Mode         *Mode          `json:"mode,omitempty" enum:"margin_target,fixed_allowance" doc:"New mode. Switching to fixed_allowance needs an allowance and keeps the stored target margin."`
	TargetMargin *string        `json:"target_margin,omitempty" doc:"New target margin, from 0 to 0.9999 with at most 4 decimals, stored in either mode."`
	Allowance    *string        `json:"allowance,omitempty" doc:"New allowance in USD for a plan in fixed_allowance mode."`
	HoldTimes    map[string]int `json:"hold_times,omitempty" doc:"Replaces every hold time. An empty object removes them all."`
	Status       *Status        `json:"status,omitempty" enum:"active,archived" doc:"archived stops new assignments and fails with 409 plan_is_default for the default plan. active restores the plan."`
}

// NewPlanResponse returns the API representation of plan.
func NewPlanResponse(plan Plan) PlanResponse {
	response := PlanResponse{
		ID:            identifiers.Encode(identifiers.PrefixPlan, plan.ID),
		Name:          plan.Name,
		Mode:          plan.Mode,
		TargetMargin:  money.FormatRatio(plan.TargetMargin),
		HoldTimes:     plan.HoldTimes,
		Status:        plan.Status,
		CustomerCount: plan.CustomerCount,
		CreatedAt:     plan.CreatedAt,
	}
	if plan.Allowance != nil {
		allowance := money.FormatAmount(*plan.Allowance)
		response.Allowance = &allowance
	}
	return response
}

// RegisterRoutes adds the plan routes of service to api on the admin group:
// GET /api/v1/plans, POST /api/v1/plans, GET /api/v1/plans/{plan_id} and
// PATCH /api/v1/plans/{plan_id}. They act in the environment of the admin key
// or member session. Handlers read service only when they run.
func RegisterRoutes(api *httpapi.API, service *Service) {
	routes := &planRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-plans",
		Method:      http.MethodGet,
		Path:        "/api/v1/plans",
		Summary:     "List plans",
		Description: "Returns the active and archived plans of the environment with their customer counts, newest first.",
		Tags:        []string{plansTag},
	}, routes.list)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID:   "create-plan",
		Method:        http.MethodPost,
		Path:          "/api/v1/plans",
		Summary:       "Create a plan",
		Description:   "Creates an active plan in the environment. A name another plan of the environment has returns 409 plan_name_taken.",
		Tags:          []string{plansTag},
		DefaultStatus: http.StatusCreated,
	}, routes.create)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "get-plan",
		Method:      http.MethodGet,
		Path:        "/api/v1/plans/{plan_id}",
		Summary:     "Get a plan",
		Description: "Returns the plan with its customer count.",
		Tags:        []string{plansTag},
	}, routes.get)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "update-plan",
		Method:      http.MethodPatch,
		Path:        "/api/v1/plans/{plan_id}",
		Summary:     "Update a plan",
		Description: "Changes the fields the request holds. Archiving the default plan returns 409 plan_is_default, and a name another plan has returns 409 plan_name_taken.",
		Tags:        []string{plansTag},
	}, routes.update)
}

func (routes *planRoutes) list(ctx context.Context, input *listPlansInput) (*httpapi.Page[PlanResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	plans, nextCursor, err := routes.service.List(ctx, environment, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	responses := make([]PlanResponse, 0, len(plans))
	for _, plan := range plans {
		responses = append(responses, NewPlanResponse(plan))
	}
	return httpapi.NewPage(responses, nextCursor), nil
}

func (routes *planRoutes) create(ctx context.Context, input *createPlanInput) (*planOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := routes.service.Create(ctx, environment, CreateInput{
		Name:         input.Body.Name,
		Mode:         input.Body.Mode,
		TargetMargin: input.Body.TargetMargin,
		Allowance:    input.Body.Allowance,
		HoldTimes:    input.Body.HoldTimes,
	})
	if err != nil {
		return nil, err
	}
	return &planOutput{Body: NewPlanResponse(plan)}, nil
}

func (routes *planRoutes) get(ctx context.Context, input *planPathInput) (*planOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	planID, err := identifiers.Decode(identifiers.PrefixPlan, input.PlanID)
	if err != nil {
		return nil, httpapi.ErrNotFound
	}
	plan, err := routes.service.Get(ctx, environment, planID)
	if err != nil {
		return nil, err
	}
	return &planOutput{Body: NewPlanResponse(plan)}, nil
}

func (routes *planRoutes) update(ctx context.Context, input *updatePlanInput) (*planOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	planID, err := identifiers.Decode(identifiers.PrefixPlan, input.PlanID)
	if err != nil {
		return nil, httpapi.ErrNotFound
	}
	plan, err := routes.service.Update(ctx, environment, planID, UpdateInput{
		Name:         input.Body.Name,
		Mode:         input.Body.Mode,
		TargetMargin: input.Body.TargetMargin,
		Allowance:    input.Body.Allowance,
		HoldTimes:    input.Body.HoldTimes,
		Status:       input.Body.Status,
	})
	if err != nil {
		return nil, err
	}
	return &planOutput{Body: NewPlanResponse(plan)}, nil
}
