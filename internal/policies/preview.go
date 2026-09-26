package policies

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies/queries"
	"github.com/preburn/preburn/internal/signals"
)

// PreviewCustomerMaximum is the most active customers an environment may
// have for Preview to evaluate a draft. Preview returns ErrPreviewTooLarge
// above it.
const PreviewCustomerMaximum = 50_000

// PreviewResult is how many customers a draft policy matches now.
type PreviewResult struct {
	EvaluatedCustomerCount int  `json:"evaluated_customer_count" doc:"Active customers the draft's level, plan or customer applies to."`
	MatchedCustomerCount   int  `json:"matched_customer_count" doc:"Evaluated customers whose signals meet the draft's conditions now. A condition on request_estimated_cost counts as met."`
	RequestDependent       bool `json:"request_dependent" doc:"True when the draft has a condition on request_estimated_cost, so whether it matches a customer also depends on each request."`
}

// ErrPreviewTooLarge is the error for a preview in an environment with more
// than PreviewCustomerMaximum active customers: 422 preview_too_large.
var ErrPreviewTooLarge = httpapi.NewCodedError(http.StatusUnprocessableEntity, "preview_too_large", "the environment has more active customers than a preview evaluates")

// SchemaName returns PolicyPreviewResult, the name of the schema of
// PreviewResult in the OpenAPI document.
func (PreviewResult) SchemaName() string {
	return "PolicyPreviewResult"
}

// Preview checks draft like a document for Create, without storing it, and
// counts the active customers of environment that it would match now, as
// if it were active. Each customer's signals come from its current period:
// the net revenue of package customerstate, and the cost to date and
// decision count summed from the ledger entries and decisions of that
// period, with nothing reserved. Conditions on request_estimated_cost count
// as met, and RequestDependent reports that the draft has one. It returns a
// 422 policy_invalid problem for an invalid draft, plans.ErrPlanNotFound for
// a draft on a plan that is missing from environment or archived, and
// ErrPreviewTooLarge when environment has more than PreviewCustomerMaximum
// active customers.
func (service *Service) Preview(ctx context.Context, environment httpapi.Environment, draft []byte) (PreviewResult, error) {
	document, err := service.checkedDraft(ctx, environment, draft)
	if err != nil {
		return PreviewResult{}, err
	}
	customerCount, err := service.queries.CountActiveCustomers(ctx, queries.Environment(environment))
	if err != nil {
		return PreviewResult{}, fmt.Errorf("count active customers of environment %s: %w", environment, err)
	}
	if customerCount > PreviewCustomerMaximum {
		return PreviewResult{}, ErrPreviewTooLarge
	}
	now := service.clock.Now()
	states, err := service.customerStates.LoadAll(ctx, environment, now)
	if err != nil {
		return PreviewResult{}, err
	}
	counters, err := service.periodCounters(ctx, environment, states)
	if err != nil {
		return PreviewResult{}, err
	}
	when, requestDependent := withoutRequestConditions(document.When)
	draftPolicy := Policy{Document: document}
	draftPolicy.Status = StatusActive
	draftPolicy.When = when
	request := ResolutionRequest{}
	if document.Feature != nil {
		request.Feature = *document.Feature
	}
	result := PreviewResult{RequestDependent: requestDependent}
	for _, state := range states {
		request.CustomerID = state.CustomerID
		request.PlanID = state.PlanID
		if !draftPolicy.appliesTo(request) {
			continue
		}
		result.EvaluatedCustomerCount++
		signalValues, err := signals.Compute(state, counters[state.CustomerID], nil, now)
		if err != nil {
			return PreviewResult{}, err
		}
		if draftPolicy.When.evaluate(signalValues).matched {
			result.MatchedCustomerCount++
		}
	}
	return result, nil
}

func (service *Service) periodCounters(ctx context.Context, environment httpapi.Environment, states []signals.CustomerState) (map[uuid.UUID]signals.CounterSnapshot, error) {
	customerIDs := make([]uuid.UUID, len(states))
	periodStarts := make([]time.Time, len(states))
	for index, state := range states {
		customerIDs[index] = state.CustomerID
		periodStarts[index] = state.Period.Start
	}
	rows, err := service.queries.ListPeriodCounters(ctx, queries.ListPeriodCountersParams{
		Environment:  queries.Environment(environment),
		CustomerIds:  customerIDs,
		PeriodStarts: periodStarts,
	})
	if err != nil {
		return nil, fmt.Errorf("list period counters of environment %s: %w", environment, err)
	}
	counters := make(map[uuid.UUID]signals.CounterSnapshot, len(rows))
	for _, row := range rows {
		counters[row.CustomerID] = signals.CounterSnapshot{Settled: money.Amount(row.SettledNanos), Count: row.DecisionCount}
	}
	return counters, nil
}

func withoutRequestConditions(group ConditionGroup) (ConditionGroup, bool) {
	members := make([]GroupMember, 0, len(group.Members))
	requestDependent := false
	for _, member := range group.Members {
		switch typed := member.(type) {
		case Condition:
			if typed.Signal == signals.NameRequestEstimatedCost {
				members = append(members, ConditionGroup{Quantifier: QuantifierAll})
				requestDependent = true
				continue
			}
			members = append(members, typed)
		case ConditionGroup:
			nested, nestedRequestDependent := withoutRequestConditions(typed)
			members = append(members, nested)
			requestDependent = requestDependent || nestedRequestDependent
		}
	}
	return ConditionGroup{Quantifier: group.Quantifier, Members: members}, requestDependent
}
