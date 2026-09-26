package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/plans/queries"
)

const (
	nameMaximumLength      = 80
	targetMarginMaximum    = money.BasisPoints(9_999)
	holdTimeMinimumSeconds = 30
	holdTimeMaximumSeconds = 86_400
	listingName            = "plans"
	uniqueViolationCode    = "23505"
	nameConstraint         = "plans_environment_name_key"

	nameLocation         = "body.name"
	modeLocation         = "body.mode"
	targetMarginLocation = "body.target_margin"
	allowanceLocation    = "body.allowance"
	holdTimesLocation    = "body.hold_times"
	statusLocation       = "body.status"
)

// Mode is how a plan sets the AI cost allowance of its customers.
type Mode string

const (
	// ModeMarginTarget plans allow the period's net revenue times one minus
	// the target margin, floored at 0.
	ModeMarginTarget Mode = "margin_target"
	// ModeFixedAllowance plans allow a fixed amount per period.
	ModeFixedAllowance Mode = "fixed_allowance"
)

// Status is the state of a plan.
type Status string

const (
	// StatusActive plans can be assigned to customers and made the default
	// plan.
	StatusActive Status = "active"
	// StatusArchived plans keep their customers, but new assignments and the
	// default plan setting reject them.
	StatusArchived Status = "archived"
)

// Plan is a pricing plan of one environment.
type Plan struct {
	// ID is the plan's UUID, exposed with the prefix pln.
	ID uuid.UUID
	// Environment is the environment the plan belongs to.
	Environment httpapi.Environment
	// Name is unique among the plans of the environment.
	Name string
	// Mode tells whether TargetMargin or Allowance sets the allowance.
	Mode Mode
	// TargetMargin is the share of net revenue kept as margin in
	// ModeMarginTarget, from 0 to 0.9999. ModeFixedAllowance keeps it for a
	// later switch back.
	TargetMargin money.BasisPoints
	// Allowance is the AI cost allowance per customer period in
	// ModeFixedAllowance, and nil in ModeMarginTarget.
	Allowance *money.Amount
	// HoldTimes maps a feature to the seconds a check reserves cost for it,
	// from 30 to 86,400. It is empty, never nil, when the plan has none.
	HoldTimes map[string]int
	// Status tells whether the plan takes new customers.
	Status Status
	// CustomerCount is the number of customers on the plan, counting the
	// customers without a plan when it is the default plan.
	CustomerCount int64
	// CreatedAt is when the plan was created, in UTC.
	CreatedAt time.Time
}

// CreateInput is a new plan for Service.Create. TargetMargin and Allowance
// hold API decimal strings.
type CreateInput struct {
	// Name is 1 to 80 characters without control characters, unique in the
	// environment.
	Name string
	// Mode is ModeMarginTarget or ModeFixedAllowance.
	Mode Mode
	// TargetMargin is a ratio from 0 to 0.9999 with at most 4 decimals, such
	// as "0.40". ModeMarginTarget requires it. ModeFixedAllowance stores 0
	// when it is nil.
	TargetMargin *string
	// Allowance is a non-negative USD amount, such as "2.00".
	// ModeFixedAllowance requires it and ModeMarginTarget rejects it.
	Allowance *string
	// HoldTimes maps features matching ^[a-z][a-z0-9_]{0,63}$ to 30 to
	// 86,400 seconds. Nil means none.
	HoldTimes map[string]int
}

// UpdateInput is a change to a plan for Service.Update. A nil field keeps
// the stored value. TargetMargin and Allowance hold API decimal strings with
// the rules of CreateInput.
type UpdateInput struct {
	// Name replaces the name, with the rules of CreateInput.Name.
	Name *string
	// Mode switches the mode. Switching to ModeFixedAllowance needs an
	// Allowance and keeps the stored target margin. Switching to
	// ModeMarginTarget drops the allowance.
	Mode *Mode
	// TargetMargin replaces the stored target margin in either mode.
	TargetMargin *string
	// Allowance replaces the allowance of a plan that is or becomes
	// ModeFixedAllowance. ModeMarginTarget rejects it.
	Allowance *string
	// HoldTimes replaces every hold time when it is not nil. An empty map
	// removes them all.
	HoldTimes map[string]int
	// Status archives or restores the plan.
	Status *Status
}

// Service creates, updates, reads and lists plans. Create one with
// NewService. It is safe for concurrent use.
type Service struct {
	pool    *pgxpool.Pool
	queries *queries.Queries
	cache   *cache.Client
	clock   clock.Clock
}

type listPosition struct {
	CreatedAt time.Time `json:"created_at"`
	PlanID    uuid.UUID `json:"plan_id"`
}

type planValues struct {
	name         string
	targetMargin money.BasisPoints
	allowance    *money.Amount
	holdTimes    map[string]int
	status       Status
}

type problemList []httpapi.ProblemError

// ErrNameTaken is the error for a plan name that another plan of the
// environment has: 409 plan_name_taken.
var ErrNameTaken = httpapi.NewCodedError(http.StatusConflict, "plan_name_taken", "a plan with this name exists in the environment")

// ErrIsDefault is the error for archiving the default plan of the
// environment: 409 plan_is_default.
var ErrIsDefault = httpapi.NewCodedError(http.StatusConflict, "plan_is_default", "the default plan of the environment cannot be archived")

// ErrPlanNotFound is the error for a plan id that another resource refers
// to, such as a customer's plan or the default plan setting, when no active
// plan of the request environment has it: 422 plan_not_found.
var ErrPlanNotFound = httpapi.NewCodedError(http.StatusUnprocessableEntity, "plan_not_found", "no active plan with this id in the environment")

// FeaturePattern matches a feature name: a lowercase letter followed by at
// most 63 lowercase letters, digits or underscores. Hold time keys must
// match it.
var FeaturePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

var (
	listCursor               = httpapi.NewCursor[listPosition](listingName)
	nameRule                 = fmt.Sprintf("expected 1 to %d characters without control characters", nameMaximumLength)
	modeRule                 = fmt.Sprintf("expected %s or %s", ModeMarginTarget, ModeFixedAllowance)
	targetMarginRule         = fmt.Sprintf("expected a ratio from 0 to %s with at most 4 decimals, such as 0.40", money.FormatRatio(targetMarginMaximum))
	targetMarginRequiredRule = fmt.Sprintf("expected a target margin in %s mode", ModeMarginTarget)
	allowanceRule            = "expected a non-negative USD amount with at most 9 decimals, such as 2.00"
	allowanceRequiredRule    = fmt.Sprintf("expected an allowance in %s mode", ModeFixedAllowance)
	allowanceRejectedRule    = fmt.Sprintf("expected no allowance in %s mode", ModeMarginTarget)
	holdTimeFeatureRule      = "expected feature names matching " + FeaturePattern.String()
	holdTimeSecondsRule      = fmt.Sprintf("expected %d to %d seconds", holdTimeMinimumSeconds, holdTimeMaximumSeconds)
	statusRule               = fmt.Sprintf("expected %s or %s", StatusActive, StatusArchived)
)

// NewService returns a Service that stores plans in pool, publishes the plan
// invalidation through cacheClient and reads time from timeSource. It only
// stores its arguments, so zero values serve route registration for the
// OpenAPI document.
func NewService(pool *pgxpool.Pool, cacheClient *cache.Client, timeSource clock.Clock) *Service {
	return &Service{pool: pool, queries: queries.New(pool), cache: cacheClient, clock: timeSource}
}

// Create adds an active plan to environment and publishes the plan
// invalidation. An invalid input returns a 422 validation_failed problem that
// names every invalid field, at body.name, body.mode, body.target_margin,
// body.allowance, body.hold_times for a feature outside the pattern and
// body.hold_times.<feature> for its seconds. A name another plan of
// environment has returns ErrNameTaken.
func (service *Service) Create(ctx context.Context, environment httpapi.Environment, input CreateInput) (Plan, error) {
	values, problems := createValues(input)
	if len(problems) > 0 {
		return Plan{}, httpapi.NewValidationProblem(problems...)
	}
	holdTimes, err := json.Marshal(values.holdTimes)
	if err != nil {
		return Plan{}, fmt.Errorf("encode hold times: %w", err)
	}
	row, err := service.queries.InsertPlan(ctx, queries.InsertPlanParams{
		PlanID:                  identifiers.New(),
		Environment:             queries.Environment(environment),
		Name:                    values.name,
		TargetMarginBasisPoints: values.targetMargin,
		AllowanceNanos:          values.allowance,
		HoldTimes:               holdTimes,
		CreatedAt:               service.clock.Now(),
	})
	if isNameTaken(err) {
		return Plan{}, ErrNameTaken
	}
	if err != nil {
		return Plan{}, fmt.Errorf("insert plan: %w", err)
	}
	plan, err := planFromRow(row, 0)
	if err != nil {
		return Plan{}, err
	}
	if err := service.publishInvalidation(ctx, plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// Update applies input to the plan with planID in environment under its row
// lock and publishes the plan invalidation. It returns httpapi.ErrNotFound
// when environment has no such plan, a 422 validation_failed problem at the
// locations of Create plus body.status, ErrNameTaken for a name another plan
// has, and ErrIsDefault when the plan would be archived while it is the
// default plan of environment.
func (service *Service) Update(ctx context.Context, environment httpapi.Environment, planID uuid.UUID, input UpdateInput) (Plan, error) {
	var plan Plan
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		var err error
		plan, err = service.updateLocked(ctx, service.queries.WithTx(transaction), environment, planID, input)
		return err
	})
	if err != nil {
		return Plan{}, err
	}
	if err := service.publishInvalidation(ctx, plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// Get returns the plan with planID in environment, or httpapi.ErrNotFound
// when environment has no such plan.
func (service *Service) Get(ctx context.Context, environment httpapi.Environment, planID uuid.UUID) (Plan, error) {
	row, err := service.queries.SelectPlan(ctx, queries.SelectPlanParams{Environment: queries.Environment(environment), PlanID: planID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, httpapi.ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("select plan %s: %w", planID, err)
	}
	counted, err := withCustomerCounts(ctx, service.queries, environment, []queries.Plan{row})
	if err != nil {
		return Plan{}, err
	}
	return counted[0], nil
}

// List returns one page of the active and archived plans of environment,
// newest first, and the cursor of the next page, which is empty on the last
// page. An empty cursor starts at the newest plan. A limit of 0 selects
// httpapi.ListLimitDefault, and a limit outside 1 to httpapi.ListLimitMaximum
// returns a 422 validation_failed problem at query.limit. A cursor that List
// did not return for environment fails with httpapi.ErrInvalidCursor.
func (service *Service) List(ctx context.Context, environment httpapi.Environment, cursor string, limit int) ([]Plan, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	parameters := queries.ListPlansParams{Environment: queries.Environment(environment), RowLimit: int64(pageSize) + 1}
	if cursor != "" {
		position, err := listCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		parameters.AfterCreatedAt = &position.CreatedAt
		parameters.AfterPlanID = &position.PlanID
	}
	rows, err := service.queries.ListPlans(ctx, parameters)
	if err != nil {
		return nil, "", fmt.Errorf("list plans of environment %s: %w", environment, err)
	}
	if len(rows) <= pageSize {
		page, err := withCustomerCounts(ctx, service.queries, environment, rows)
		return page, "", err
	}
	page, err := withCustomerCounts(ctx, service.queries, environment, rows[:pageSize])
	if err != nil {
		return nil, "", err
	}
	last := page[pageSize-1]
	nextCursor, err := listCursor.Encode(environment, listPosition{CreatedAt: last.CreatedAt, PlanID: last.ID})
	if err != nil {
		return nil, "", err
	}
	return page, nextCursor, nil
}

func (service *Service) updateLocked(ctx context.Context, transactionQueries *queries.Queries, environment httpapi.Environment, planID uuid.UUID, input UpdateInput) (Plan, error) {
	row, err := transactionQueries.SelectPlanForUpdate(ctx, queries.SelectPlanForUpdateParams{
		Environment: queries.Environment(environment),
		PlanID:      planID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, httpapi.ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("select plan %s for update: %w", planID, err)
	}
	current, err := planFromRow(row, 0)
	if err != nil {
		return Plan{}, err
	}
	values, problems := updateValues(current, input)
	if len(problems) > 0 {
		return Plan{}, httpapi.NewValidationProblem(problems...)
	}
	if values.status == StatusArchived {
		if err := rejectDefaultPlan(ctx, transactionQueries, environment, planID); err != nil {
			return Plan{}, err
		}
	}
	holdTimes, err := json.Marshal(values.holdTimes)
	if err != nil {
		return Plan{}, fmt.Errorf("encode hold times: %w", err)
	}
	updated, err := transactionQueries.UpdatePlan(ctx, queries.UpdatePlanParams{
		Name:                    values.name,
		TargetMarginBasisPoints: values.targetMargin,
		AllowanceNanos:          values.allowance,
		HoldTimes:               holdTimes,
		Status:                  queries.RecordStatus(values.status),
		UpdatedAt:               service.clock.Now(),
		PlanID:                  planID,
	})
	if isNameTaken(err) {
		return Plan{}, ErrNameTaken
	}
	if err != nil {
		return Plan{}, fmt.Errorf("update plan %s: %w", planID, err)
	}
	counted, err := withCustomerCounts(ctx, transactionQueries, environment, []queries.Plan{updated})
	if err != nil {
		return Plan{}, err
	}
	return counted[0], nil
}

func (service *Service) publishInvalidation(ctx context.Context, plan Plan) error {
	return service.cache.PublishInvalidation(ctx, cache.Invalidation{
		Kind:        cache.InvalidationKindPlan,
		Environment: string(plan.Environment),
		ID:          identifiers.Encode(identifiers.PrefixPlan, plan.ID),
	})
}

func rejectDefaultPlan(ctx context.Context, transactionQueries *queries.Queries, environment httpapi.Environment, planID uuid.UUID) error {
	defaultPlanID, err := transactionQueries.SelectDefaultPlanID(ctx, queries.Environment(environment))
	if err != nil {
		return fmt.Errorf("select default plan of environment %s: %w", environment, err)
	}
	if defaultPlanID != nil && *defaultPlanID == planID {
		return ErrIsDefault
	}
	return nil
}

func withCustomerCounts(ctx context.Context, planQueries *queries.Queries, environment httpapi.Environment, rows []queries.Plan) ([]Plan, error) {
	planIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		planIDs = append(planIDs, row.PlanID)
	}
	countRows, err := planQueries.CountCustomersPerPlan(ctx, queries.CountCustomersPerPlanParams{
		Environment: queries.Environment(environment),
		PlanIds:     planIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("count customers per plan of environment %s: %w", environment, err)
	}
	customerCounts := make(map[uuid.UUID]int64, len(countRows))
	for _, countRow := range countRows {
		customerCounts[countRow.PlanID] = countRow.CustomerCount
	}
	counted := make([]Plan, 0, len(rows))
	for _, row := range rows {
		plan, err := planFromRow(row, customerCounts[row.PlanID])
		if err != nil {
			return nil, err
		}
		counted = append(counted, plan)
	}
	return counted, nil
}

func planFromRow(row queries.Plan, customerCount int64) (Plan, error) {
	var holdTimes map[string]int
	if err := json.Unmarshal(row.HoldTimes, &holdTimes); err != nil {
		return Plan{}, fmt.Errorf("decode hold times of plan %s: %w", row.PlanID, err)
	}
	mode := ModeMarginTarget
	if row.AllowanceNanos != nil {
		mode = ModeFixedAllowance
	}
	return Plan{
		ID:            row.PlanID,
		Environment:   httpapi.Environment(row.Environment),
		Name:          row.Name,
		Mode:          mode,
		TargetMargin:  row.TargetMarginBasisPoints,
		Allowance:     row.AllowanceNanos,
		HoldTimes:     holdTimes,
		Status:        Status(row.Status),
		CustomerCount: customerCount,
		CreatedAt:     row.CreatedAt.UTC(),
	}, nil
}

func createValues(input CreateInput) (planValues, problemList) {
	var problems problemList
	values := planValues{name: input.Name, holdTimes: map[string]int{}}
	problems.require(validName(input.Name), nameLocation, nameRule)
	problems.require(input.Mode.known(), modeLocation, modeRule)
	if input.TargetMargin == nil {
		problems.require(input.Mode != ModeMarginTarget, targetMarginLocation, targetMarginRequiredRule)
	} else {
		values.targetMargin = problems.targetMargin(*input.TargetMargin)
	}
	values.allowance = problems.allowance(input.Mode, input.Allowance, nil)
	if input.HoldTimes != nil {
		values.holdTimes = input.HoldTimes
		problems.holdTimes(input.HoldTimes)
	}
	return values, problems
}

func updateValues(current Plan, input UpdateInput) (planValues, problemList) {
	var problems problemList
	values := planValues{
		name:         current.Name,
		targetMargin: current.TargetMargin,
		holdTimes:    current.HoldTimes,
		status:       current.Status,
	}
	mode := current.Mode
	if input.Name != nil {
		values.name = *input.Name
		problems.require(validName(values.name), nameLocation, nameRule)
	}
	if input.Mode != nil {
		mode = *input.Mode
		problems.require(mode.known(), modeLocation, modeRule)
	}
	if input.TargetMargin != nil {
		values.targetMargin = problems.targetMargin(*input.TargetMargin)
	}
	values.allowance = problems.allowance(mode, input.Allowance, current.Allowance)
	if input.HoldTimes != nil {
		values.holdTimes = input.HoldTimes
		problems.holdTimes(input.HoldTimes)
	}
	if input.Status != nil {
		values.status = *input.Status
		problems.require(values.status == StatusActive || values.status == StatusArchived, statusLocation, statusRule)
	}
	return values, problems
}

func (problems *problemList) require(valid bool, location string, message string) {
	if !valid {
		*problems = append(*problems, httpapi.ProblemError{Location: location, Message: message})
	}
}

func (problems *problemList) targetMargin(value string) money.BasisPoints {
	targetMargin, err := money.ParseRatio(value, 0, targetMarginMaximum)
	problems.require(err == nil, targetMarginLocation, targetMarginRule)
	return targetMargin
}

func (problems *problemList) allowance(mode Mode, value *string, current *money.Amount) *money.Amount {
	switch mode {
	case ModeMarginTarget:
		problems.require(value == nil, allowanceLocation, allowanceRejectedRule)
	case ModeFixedAllowance:
		if value == nil {
			problems.require(current != nil, allowanceLocation, allowanceRequiredRule)
			return current
		}
		allowance, err := money.ParseNonNegativeAmount(*value)
		problems.require(err == nil, allowanceLocation, allowanceRule)
		return &allowance
	}
	return nil
}

func (problems *problemList) holdTimes(holdTimes map[string]int) {
	features := slices.Sorted(maps.Keys(holdTimes))
	problems.require(!slices.ContainsFunc(features, invalidFeature), holdTimesLocation, holdTimeFeatureRule)
	for _, feature := range features {
		seconds := holdTimes[feature]
		validSeconds := seconds >= holdTimeMinimumSeconds && seconds <= holdTimeMaximumSeconds
		problems.require(invalidFeature(feature) || validSeconds, holdTimesLocation+"."+feature, holdTimeSecondsRule)
	}
}

func (mode Mode) known() bool {
	switch mode {
	case ModeMarginTarget, ModeFixedAllowance:
		return true
	}
	return false
}

func invalidFeature(feature string) bool {
	return !FeaturePattern.MatchString(feature)
}

func validName(name string) bool {
	return strings.TrimSpace(name) != "" &&
		utf8.RuneCountInString(name) <= nameMaximumLength &&
		utf8.ValidString(name) &&
		!strings.ContainsFunc(name, unicode.IsControl)
}

func isNameTaken(err error) bool {
	pgError, isPostgresError := errors.AsType[*pgconn.PgError](err)
	return isPostgresError && pgError.Code == uniqueViolationCode && pgError.ConstraintName == nameConstraint
}
