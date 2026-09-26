package dashboard

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/dashboard/queries"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/signals"
)

const (
	// ActiveCustomerMaximum is the most active customers an environment may
	// have for the overview and the customer list, which compute the current
	// signals of every active customer. Both return ErrEnvironmentTooLarge
	// above it.
	ActiveCustomerMaximum = 50_000

	recentDecisionLimit = 10
	searchMaximumLength = 200
	customerListingName = "dashboard_customers"
	searchLocation      = "query.search"
)

// CustomerSort is the value that orders the customer list.
type CustomerSort string

const (
	// CustomerSortMargin orders by the margin of the current period.
	CustomerSortMargin CustomerSort = "margin"
	// CustomerSortPace orders by the pace signal.
	CustomerSortPace CustomerSort = "pace"
	// CustomerSortCost orders by the settled cost of the current period.
	CustomerSortCost CustomerSort = "cost"
	// CustomerSortRevenue orders by the net revenue of the current period.
	CustomerSortRevenue CustomerSort = "revenue"
)

// SortDirection is the direction of the customer list order.
type SortDirection string

const (
	// SortDirectionAscending puts the lowest value first.
	SortDirectionAscending SortDirection = "ascending"
	// SortDirectionDescending puts the highest value first.
	SortDirectionDescending SortDirection = "descending"
)

// RevenueFilter keeps customers by the net revenue of their current period.
type RevenueFilter string

const (
	// RevenueFilterAll keeps every customer.
	RevenueFilterAll RevenueFilter = "all"
	// RevenueFilterPaying keeps customers with net revenue above zero.
	RevenueFilterPaying RevenueFilter = "paying"
	// RevenueFilterFree keeps customers with net revenue of zero or below.
	RevenueFilterFree RevenueFilter = "free"
)

// CustomerListFilter selects and orders the customer list.
type CustomerListFilter struct {
	// Sort is the value the list orders by.
	Sort CustomerSort
	// Direction is the direction of the order.
	Direction SortDirection
	// Revenue keeps paying customers, free customers or both.
	Revenue RevenueFilter
	// PlanID keeps the customers whose effective plan it is, or nil for
	// every plan.
	PlanID *uuid.UUID
	// Search keeps the customers whose external id or display name contains
	// it in any letter case, or empty for every customer.
	Search string
}

// CustomerService reads the customer list and customer details of the
// dashboard. Create one with NewCustomerService. It is safe for concurrent
// use.
type CustomerService struct {
	queries *queries.Queries
	signals signalReader
	clock   clock.Clock
}

type signalReader struct {
	queries  *queries.Queries
	states   *customerstate.Loader
	counters *decisions.Counters
}

type customerSignals struct {
	state   signals.CustomerState
	signals signals.Signals
}

type customerRow struct {
	customerSignals
	externalID  string
	displayName *string
	plan        *queries.ListPlansRow
	position    customerPosition
}

type customerPosition struct {
	paying     bool
	amount     money.Amount
	ratio      float64
	externalID string
}

type customerCursorKey struct {
	Sort       CustomerSort  `json:"sort"`
	Direction  SortDirection `json:"direction"`
	Paying     bool          `json:"paying"`
	Amount     money.Amount  `json:"amount"`
	RatioBits  uint64        `json:"ratio_bits"`
	ExternalID string        `json:"external_id"`
}

// ErrEnvironmentTooLarge is the error for a dashboard read that computes the
// signals of every active customer in an environment with more than
// ActiveCustomerMaximum of them: 422 environment_too_large.
var ErrEnvironmentTooLarge = httpapi.NewCodedError(http.StatusUnprocessableEntity, "environment_too_large", "the environment has more active customers than the dashboard evaluates")

var (
	customerCursor = httpapi.NewCursor[customerCursorKey](customerListingName)
	searchRule     = fmt.Sprintf("expected at most %d characters", searchMaximumLength)
)

// NewCustomerService returns a CustomerService that reads pool, loads
// customer states through states and reads period counters through
// counters. It only stores its arguments, so zero values serve route
// registration for the OpenAPI document.
func NewCustomerService(pool *pgxpool.Pool, states *customerstate.Loader, counters *decisions.Counters, timeSource clock.Clock) *CustomerService {
	store := queries.New(pool)
	return &CustomerService{
		queries: store,
		signals: signalReader{queries: store, states: states, counters: counters},
		clock:   timeSource,
	}
}

// List returns one page of the active customers of environment with the
// revenue, cost, margin and pace of their current period, in the order of
// filter, and the cursor of the next page, which is empty on the last page.
// Revenue is the net revenue attributed to the period, cost the settled cost
// of the period counter, and margin one minus cost divided by revenue, nil
// without revenue above zero. Customers with revenue above zero come first
// in both directions, then the others in the same order, and ties order by
// external id. A search longer than 200 characters returns a 422
// validation_failed problem at query.search. A limit of 0 selects
// httpapi.ListLimitDefault, and a limit outside 1 to
// httpapi.ListLimitMaximum returns a 422 validation_failed problem at
// query.limit. A cursor that List did not return for environment and the
// same sort and direction fails with httpapi.ErrInvalidCursor. It returns
// ErrEnvironmentTooLarge when environment has more than
// ActiveCustomerMaximum active customers.
func (service *CustomerService) List(ctx context.Context, environment httpapi.Environment, filter CustomerListFilter, cursor string, limit int) ([]CustomerMarginResponse, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	if utf8.RuneCountInString(filter.Search) > searchMaximumLength {
		return nil, "", httpapi.NewValidationProblem(httpapi.ProblemError{Location: searchLocation, Message: searchRule})
	}
	var after *customerPosition
	if cursor != "" {
		key, err := customerCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		if key.Sort != filter.Sort || key.Direction != filter.Direction {
			return nil, "", fmt.Errorf("%w: sort=%s direction=%s", httpapi.ErrInvalidCursor, key.Sort, key.Direction)
		}
		position := key.position()
		after = &position
	}
	rows, err := service.customerRows(ctx, environment, filter)
	if err != nil {
		return nil, "", err
	}
	start := 0
	if after != nil {
		start = sort.Search(len(rows), func(index int) bool { return filter.compare(rows[index].position, *after) > 0 })
	}
	end := min(start+pageSize, len(rows))
	page := rows[start:end]
	responses := make([]CustomerMarginResponse, len(page))
	for index, row := range page {
		responses[index] = newCustomerMarginResponse(row)
	}
	if end == len(rows) {
		return responses, "", nil
	}
	nextCursor, err := customerCursor.Encode(environment, filter.cursorKey(page[len(page)-1].position))
	if err != nil {
		return nil, "", err
	}
	return responses, nextCursor, nil
}

// Customer returns the customer with customerID in environment with the
// signals of its current period, its period rollups from newest to oldest,
// the usage of its current period by feature, provider and model from the
// highest cost down, and its 10 latest decisions, newest first. It returns
// httpapi.ErrNotFound when environment has no such customer.
func (service *CustomerService) Customer(ctx context.Context, environment httpapi.Environment, customerID uuid.UUID) (CustomerDetailResponse, error) {
	storeEnvironment := queries.Environment(environment)
	customer, err := service.queries.SelectCustomer(ctx, queries.SelectCustomerParams{Environment: storeEnvironment, CustomerID: customerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return CustomerDetailResponse{}, httpapi.ErrNotFound
	}
	if err != nil {
		return CustomerDetailResponse{}, fmt.Errorf("select customer %s: %w", customerID, err)
	}
	now := service.clock.Now()
	state, err := service.signals.states.Load(ctx, environment, customerID, now)
	if err != nil {
		return CustomerDetailResponse{}, err
	}
	current, err := service.signals.compute(ctx, environment, []signals.CustomerState{state}, now)
	if err != nil {
		return CustomerDetailResponse{}, err
	}
	detail := CustomerDetailResponse{
		ID:          identifiers.Encode(identifiers.PrefixCustomer, customer.CustomerID),
		ExternalID:  customer.ExternalID,
		DisplayName: customer.DisplayName,
		Status:      customers.Status(customer.Status),
		PeriodStart: state.Period.Start,
		PeriodEnd:   state.Period.End,
		Signals:     signals.NewResponse(current[0].signals),
	}
	if state.PlanID != nil {
		plansByID, err := service.plansByID(ctx, environment)
		if err != nil {
			return CustomerDetailResponse{}, err
		}
		plan, err := planOf(plansByID, *state.PlanID)
		if err != nil {
			return CustomerDetailResponse{}, err
		}
		detail.PlanID, detail.PlanName, detail.TargetMargin = planFields(&plan)
	}
	if detail.History, err = service.history(ctx, storeEnvironment, customerID); err != nil {
		return CustomerDetailResponse{}, err
	}
	if detail.Usage, err = service.usage(ctx, storeEnvironment, customerID, state.Period.Start); err != nil {
		return CustomerDetailResponse{}, err
	}
	if detail.RecentDecisions, err = service.recentDecisions(ctx, storeEnvironment, customerID); err != nil {
		return CustomerDetailResponse{}, err
	}
	return detail, nil
}

func (service *CustomerService) customerRows(ctx context.Context, environment httpapi.Environment, filter CustomerListFilter) ([]customerRow, error) {
	current, err := service.signals.activeCustomers(ctx, environment, service.clock.Now())
	if err != nil {
		return nil, err
	}
	activeRows, err := service.queries.ListActiveCustomers(ctx, queries.Environment(environment))
	if err != nil {
		return nil, fmt.Errorf("list active customers of environment %s: %w", environment, err)
	}
	named := make(map[uuid.UUID]queries.ListActiveCustomersRow, len(activeRows))
	for _, row := range activeRows {
		named[row.CustomerID] = row
	}
	plansByID, err := service.plansByID(ctx, environment)
	if err != nil {
		return nil, err
	}
	search := strings.ToLower(filter.Search)
	rows := make([]customerRow, 0, len(current))
	for _, customer := range current {
		// A customer that stopped being active after LoadAll read its state has no active row.
		active, found := named[customer.state.CustomerID]
		if !found {
			continue
		}
		row := customerRow{customerSignals: customer, externalID: active.ExternalID, displayName: active.DisplayName}
		if customer.state.PlanID != nil {
			plan, err := planOf(plansByID, *customer.state.PlanID)
			if err != nil {
				return nil, err
			}
			row.plan = &plan
		}
		if !filter.keeps(row, search) {
			continue
		}
		row.position = filter.positionOf(row)
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(left, right customerRow) int { return filter.compare(left.position, right.position) })
	return rows, nil
}

func (service *CustomerService) plansByID(ctx context.Context, environment httpapi.Environment) (map[uuid.UUID]queries.ListPlansRow, error) {
	planRows, err := service.queries.ListPlans(ctx, queries.Environment(environment))
	if err != nil {
		return nil, fmt.Errorf("list plans of environment %s: %w", environment, err)
	}
	plansByID := make(map[uuid.UUID]queries.ListPlansRow, len(planRows))
	for _, plan := range planRows {
		plansByID[plan.PlanID] = plan
	}
	return plansByID, nil
}

func (service *CustomerService) history(ctx context.Context, environment queries.Environment, customerID uuid.UUID) ([]CustomerPeriodResponse, error) {
	rows, err := service.queries.ListCustomerRollups(ctx, queries.ListCustomerRollupsParams{Environment: environment, CustomerID: customerID})
	if err != nil {
		return nil, fmt.Errorf("list rollups of customer %s: %w", customerID, err)
	}
	history := make([]CustomerPeriodResponse, len(rows))
	for index, row := range rows {
		var counts ledger.DecisionCounts
		if err := json.Unmarshal(row.DecisionCounts, &counts); err != nil {
			return nil, fmt.Errorf("decode decision counts of customer %s: %w", customerID, err)
		}
		history[index] = CustomerPeriodResponse{
			PeriodStart:    row.PeriodStart.UTC(),
			PeriodEnd:      row.PeriodEnd.UTC(),
			Revenue:        money.FormatAmount(row.RevenueNetNanos),
			Cost:           money.FormatAmount(row.CostNanos),
			Margin:         formatMargin(row.RevenueNetNanos, row.CostNanos),
			UncostedCount:  int64(row.UncostedCount),
			DecisionCounts: DecisionCountsResponse(counts),
		}
	}
	return history, nil
}

func (service *CustomerService) usage(ctx context.Context, environment queries.Environment, customerID uuid.UUID, periodStart time.Time) ([]CustomerUsageResponse, error) {
	rows, err := service.queries.ListCustomerPeriodUsage(ctx, queries.ListCustomerPeriodUsageParams{
		Environment: environment,
		CustomerID:  customerID,
		PeriodStart: periodStart,
	})
	if err != nil {
		return nil, fmt.Errorf("list period usage of customer %s: %w", customerID, err)
	}
	usage := make([]CustomerUsageResponse, len(rows))
	for index, row := range rows {
		usage[index] = CustomerUsageResponse{
			Feature:       row.Feature,
			Provider:      row.Provider,
			Model:         row.Model,
			RequestCount:  row.RequestCount,
			Cost:          money.FormatAmount(money.Amount(row.CostNanos)),
			UncostedCount: row.UncostedCount,
		}
	}
	return usage, nil
}

func (service *CustomerService) recentDecisions(ctx context.Context, environment queries.Environment, customerID uuid.UUID) ([]CustomerDecisionResponse, error) {
	rows, err := service.queries.ListRecentCustomerDecisions(ctx, queries.ListRecentCustomerDecisionsParams{
		Environment: environment,
		CustomerID:  customerID,
		RowLimit:    recentDecisionLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list recent decisions of customer %s: %w", customerID, err)
	}
	recent := make([]CustomerDecisionResponse, len(rows))
	for index, row := range rows {
		recent[index] = CustomerDecisionResponse{
			ID:                identifiers.Encode(identifiers.PrefixDecision, row.DecisionID),
			Feature:           row.Feature,
			RequestedProvider: row.RequestedProvider,
			RequestedModel:    row.RequestedModel,
			Provider:          row.Provider,
			Model:             row.Model,
			Outcome:           policies.Outcome(row.Outcome),
			Reason:            policies.Reason(row.Reason),
			Status:            row.Status,
			CreatedAt:         row.CreatedAt.UTC(),
		}
		if row.MatchedPolicyID != nil {
			policyID := identifiers.Encode(identifiers.PrefixPolicy, *row.MatchedPolicyID)
			recent[index].MatchedPolicyID = &policyID
		}
		if row.EstimatedCostNanos != nil {
			estimatedCost := money.FormatAmount(*row.EstimatedCostNanos)
			recent[index].EstimatedCost = &estimatedCost
		}
	}
	return recent, nil
}

func (reader signalReader) activeCustomers(ctx context.Context, environment httpapi.Environment, now time.Time) ([]customerSignals, error) {
	activeCount, err := reader.queries.CountActiveCustomers(ctx, queries.Environment(environment))
	if err != nil {
		return nil, fmt.Errorf("count active customers of environment %s: %w", environment, err)
	}
	if activeCount > ActiveCustomerMaximum {
		return nil, ErrEnvironmentTooLarge
	}
	states, err := reader.states.LoadAll(ctx, environment, now)
	if err != nil {
		return nil, err
	}
	return reader.compute(ctx, environment, states, now)
}

func (reader signalReader) compute(ctx context.Context, environment httpapi.Environment, states []signals.CustomerState, now time.Time) ([]customerSignals, error) {
	periodStarts := make(map[uuid.UUID]time.Time, len(states))
	for _, state := range states {
		periodStarts[state.CustomerID] = state.Period.Start
	}
	snapshots, err := reader.counters.SnapshotMany(ctx, environment, periodStarts)
	if err != nil {
		return nil, err
	}
	computed := make([]customerSignals, len(states))
	for index, state := range states {
		values, err := signals.Compute(state, snapshots[state.CustomerID], nil, now)
		if err != nil {
			return nil, err
		}
		computed[index] = customerSignals{state: state, signals: values}
	}
	return computed, nil
}

func (filter CustomerListFilter) keeps(row customerRow, lowerSearch string) bool {
	paying := row.state.NetRevenue > 0
	if (filter.Revenue == RevenueFilterPaying && !paying) || (filter.Revenue == RevenueFilterFree && paying) {
		return false
	}
	if filter.PlanID != nil && (row.state.PlanID == nil || *row.state.PlanID != *filter.PlanID) {
		return false
	}
	if strings.Contains(strings.ToLower(row.externalID), lowerSearch) {
		return true
	}
	return row.displayName != nil && strings.Contains(strings.ToLower(*row.displayName), lowerSearch)
}

func (filter CustomerListFilter) positionOf(row customerRow) customerPosition {
	position := customerPosition{paying: row.state.NetRevenue > 0, externalID: row.externalID}
	switch filter.Sort {
	case CustomerSortMargin:
		position.ratio, _ = marginOf(row.state.NetRevenue, row.signals.CostToDate)
	case CustomerSortPace:
		position.ratio = row.signals.Pace
	case CustomerSortCost:
		position.amount = row.signals.CostToDate
	case CustomerSortRevenue:
		position.amount = row.state.NetRevenue
	}
	return position
}

func (filter CustomerListFilter) compare(left customerPosition, right customerPosition) int {
	if left.paying != right.paying {
		if left.paying {
			return -1
		}
		return 1
	}
	order := cmp.Or(cmp.Compare(left.amount, right.amount), cmp.Compare(left.ratio, right.ratio))
	if filter.Direction == SortDirectionDescending {
		order = -order
	}
	return cmp.Or(order, strings.Compare(left.externalID, right.externalID))
}

func (filter CustomerListFilter) cursorKey(position customerPosition) customerCursorKey {
	return customerCursorKey{
		Sort:       filter.Sort,
		Direction:  filter.Direction,
		Paying:     position.paying,
		Amount:     position.amount,
		RatioBits:  math.Float64bits(position.ratio),
		ExternalID: position.externalID,
	}
}

func (key customerCursorKey) position() customerPosition {
	return customerPosition{
		paying:     key.Paying,
		amount:     key.Amount,
		ratio:      math.Float64frombits(key.RatioBits),
		externalID: key.ExternalID,
	}
}

func planOf(plansByID map[uuid.UUID]queries.ListPlansRow, planID uuid.UUID) (queries.ListPlansRow, error) {
	plan, found := plansByID[planID]
	if !found {
		return queries.ListPlansRow{}, fmt.Errorf("plan %s is not a plan of the environment", planID)
	}
	return plan, nil
}

func planFields(plan *queries.ListPlansRow) (*string, *string, *string) {
	planID := identifiers.Encode(identifiers.PrefixPlan, plan.PlanID)
	if planMode(plan) == plans.ModeFixedAllowance {
		return &planID, &plan.Name, nil
	}
	targetMargin := money.FormatRatio(plan.TargetMarginBasisPoints)
	return &planID, &plan.Name, &targetMargin
}

func planMode(plan *queries.ListPlansRow) plans.Mode {
	if plan.AllowanceNanos == nil {
		return plans.ModeMarginTarget
	}
	return plans.ModeFixedAllowance
}

func newCustomerMarginResponse(row customerRow) CustomerMarginResponse {
	response := CustomerMarginResponse{
		ID:          identifiers.Encode(identifiers.PrefixCustomer, row.state.CustomerID),
		ExternalID:  row.externalID,
		DisplayName: row.displayName,
		Revenue:     money.FormatAmount(row.state.NetRevenue),
		Cost:        money.FormatAmount(row.signals.CostToDate),
		Margin:      formatMargin(row.state.NetRevenue, row.signals.CostToDate),
		Pace:        money.FormatSignal(row.signals.Pace),
		PeriodStart: row.state.Period.Start,
		PeriodEnd:   row.state.Period.End,
	}
	if row.plan != nil {
		response.PlanID, response.PlanName, response.TargetMargin = planFields(row.plan)
	}
	return response
}
