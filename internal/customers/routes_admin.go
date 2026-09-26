package customers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/signals"
)

// CounterReader reads the Redis period counters of customers.
// decisions.Counters implements it.
type CounterReader interface {
	// SnapshotMany returns a snapshot for every customer in periodStarts,
	// which maps each customer id to the start of its current period. A
	// counter that does not exist reads as zero.
	SnapshotMany(ctx context.Context, environment httpapi.Environment, periodStarts map[uuid.UUID]time.Time) (map[uuid.UUID]signals.CounterSnapshot, error)
}

// CustomerSignalsResponse is the API representation of a customer with the
// signals of its current period.
type CustomerSignalsResponse struct {
	CustomerResponse
	EffectivePlanID *string          `json:"effective_plan_id" doc:"Plan the signals follow: the customer's plan, else the default plan of the environment. Null when there is neither."`
	PeriodStart     time.Time        `json:"period_start" doc:"Start of the customer's current period."`
	PeriodEnd       time.Time        `json:"period_end" doc:"End of the customer's current period."`
	Signals         signals.Response `json:"signals" doc:"Signals of the current period, with nothing requested."`
}

type adminCustomerRoutes struct {
	service  *Service
	states   *customerstate.Loader
	counters CounterReader
}

type listCustomersInput struct {
	Search string `query:"search" doc:"Keeps the customers whose external id starts with this text in any letter case."`
	Cursor string `query:"cursor" doc:"Cursor from next_cursor of the previous page. Omit it for the first page."`
	Limit  int    `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

type getCustomerInput struct {
	ExternalID string `path:"external_id" doc:"Id of the customer in your system."`
}

type customerSignalsOutput struct {
	Body CustomerSignalsResponse
}

// RegisterAdminRoutes adds GET /api/v1/customers and GET
// /api/v1/customers/{external_id} of service to api on the admin group. Both
// return customers with the signals of their current period, from the
// customer state that states loads and the period counter that counters
// reads, with nothing requested. A page of the list loads the states of its
// customers, whatever their status, in one batch with Loader.LoadMany. They
// act in the environment of the API key or member session. Handlers read
// their dependencies only when they run.
func RegisterAdminRoutes(api *httpapi.API, service *Service, states *customerstate.Loader, counters CounterReader) {
	routes := &adminCustomerRoutes{service: service, states: states, counters: counters}
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-customers",
		Method:      http.MethodGet,
		Path:        "/api/v1/customers",
		Summary:     "List customers",
		Description: "Returns the customers of the environment with the signals of their current period, newest first.",
		Tags:        []string{customersTag},
	}, routes.list)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "get-customer",
		Method:      http.MethodGet,
		Path:        "/api/v1/customers/{external_id}",
		Summary:     "Get a customer",
		Description: "Returns the customer with the external id and the signals of its current period.",
		Tags:        []string{customersTag},
	}, routes.get)
}

func (routes *adminCustomerRoutes) list(ctx context.Context, input *listCustomersInput) (*httpapi.Page[CustomerSignalsResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	listed, nextCursor, err := routes.service.List(ctx, environment, input.Search, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	now := routes.service.clock.Now()
	states, err := routes.pageStates(ctx, environment, listed, now)
	if err != nil {
		return nil, err
	}
	responses, err := routes.withSignals(ctx, environment, listed, states, now)
	if err != nil {
		return nil, err
	}
	return httpapi.NewPage(responses, nextCursor), nil
}

func (routes *adminCustomerRoutes) get(ctx context.Context, input *getCustomerInput) (*customerSignalsOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	customer, err := routes.service.CustomerByExternalID(ctx, environment, input.ExternalID)
	if err != nil {
		return nil, err
	}
	now := routes.service.clock.Now()
	state, err := routes.states.Load(ctx, environment, customer.ID, now)
	if err != nil {
		return nil, err
	}
	responses, err := routes.withSignals(ctx, environment, []Customer{customer}, map[uuid.UUID]signals.CustomerState{customer.ID: state}, now)
	if err != nil {
		return nil, err
	}
	return &customerSignalsOutput{Body: responses[0]}, nil
}

func (routes *adminCustomerRoutes) pageStates(ctx context.Context, environment httpapi.Environment, listed []Customer, now time.Time) (map[uuid.UUID]signals.CustomerState, error) {
	customerIDs := make([]uuid.UUID, len(listed))
	for index, customer := range listed {
		customerIDs[index] = customer.ID
	}
	loaded, err := routes.states.LoadMany(ctx, environment, customerIDs, now)
	if err != nil {
		return nil, err
	}
	states := make(map[uuid.UUID]signals.CustomerState, len(loaded))
	for _, state := range loaded {
		states[state.CustomerID] = state
	}
	return states, nil
}

func (routes *adminCustomerRoutes) withSignals(ctx context.Context, environment httpapi.Environment, listed []Customer, states map[uuid.UUID]signals.CustomerState, now time.Time) ([]CustomerSignalsResponse, error) {
	periodStarts := make(map[uuid.UUID]time.Time, len(listed))
	for _, customer := range listed {
		periodStarts[customer.ID] = states[customer.ID].Period.Start
	}
	snapshots, err := routes.counters.SnapshotMany(ctx, environment, periodStarts)
	if err != nil {
		return nil, err
	}
	responses := make([]CustomerSignalsResponse, len(listed))
	for index, customer := range listed {
		state := states[customer.ID]
		computed, err := signals.Compute(state, snapshots[customer.ID], nil, now)
		if err != nil {
			return nil, err
		}
		responses[index] = CustomerSignalsResponse{
			CustomerResponse: newCustomerResponse(customer),
			PeriodStart:      state.Period.Start,
			PeriodEnd:        state.Period.End,
			Signals:          signals.NewResponse(computed),
		}
		if state.PlanID != nil {
			effectivePlanID := identifiers.Encode(identifiers.PrefixPlan, *state.PlanID)
			responses[index].EffectivePlanID = &effectivePlanID
		}
	}
	return responses, nil
}
