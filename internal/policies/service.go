package policies

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/policies/queries"
)

const (
	listingName  = "policies"
	bodyLocation = "body"
)

// Service creates, updates, reads, lists and previews policies. It keeps
// the active policies of each environment in its Cache. Create one with
// NewService. It is safe for concurrent use.
type Service struct {
	pool              *pgxpool.Pool
	queries           *queries.Queries
	cache             *cache.Client
	clock             clock.Clock
	parameterMappings map[catalogfiles.ModelKey]map[string]catalogfiles.Parameter
	customerStates    *customerstate.Loader
	activePolicies    *Cache
}

// ListFilter narrows the policies Service.List returns. A nil field keeps
// every policy.
type ListFilter struct {
	// Status keeps the policies with this status.
	Status *Status
	// PlanID keeps the plan level policies of this plan.
	PlanID *uuid.UUID
}

type listPosition struct {
	CreatedAt time.Time `json:"created_at"`
	PolicyID  uuid.UUID `json:"policy_id"`
}

var (
	listCursor       = httpapi.NewCursor[listPosition](listingName)
	activeStatusJSON = json.RawMessage(`"` + StatusActive + `"`)
)

// NewService returns a Service that stores policies in pool, validates
// overrides against the parameter mappings of files, publishes the policies
// invalidation through cacheClient and reads time from timeSource. It only
// stores its arguments, so zero values serve route registration for the
// OpenAPI document.
func NewService(pool *pgxpool.Pool, cacheClient *cache.Client, files catalogfiles.Catalog, timeSource clock.Clock) *Service {
	service := &Service{
		pool:              pool,
		queries:           queries.New(pool),
		cache:             cacheClient,
		clock:             timeSource,
		parameterMappings: files.ParameterMappings,
		customerStates:    customerstate.NewLoader(pool),
	}
	service.activePolicies = newCache(service, timeSource)
	return service
}

// Cache returns the Cache of the service. Every policy change clears it.
func (service *Service) Cache() *Cache {
	return service.activePolicies
}

// ParameterMappings returns the overridable parameters of every provider
// model in the catalog's parameter mappings. The map is shared, so callers
// never modify it.
func (service *Service) ParameterMappings() map[catalogfiles.ModelKey]map[string]catalogfiles.Parameter {
	return service.parameterMappings
}

// Create stores document, the API JSON form of a policy document, as version
// 1 of a new policy in environment, clears the Cache and publishes the
// policies invalidation. An omitted status stores an active policy. A route
// chain target that names a model alias is stored as the model it names. A
// document that DecodeDocument or Validate rejects returns a 422
// policy_invalid problem with one error per failing JSON path under body,
// such as body.when.all[1].value. A customer id must belong to environment.
// A plan-level policy whose plan is missing from environment or archived
// returns plans.ErrPlanNotFound.
func (service *Service) Create(ctx context.Context, environment httpapi.Environment, document []byte) (Policy, error) {
	checked, err := service.checkedDraft(ctx, environment, document)
	if err != nil {
		return Policy{}, err
	}
	conditionGroup, action, err := encodeRules(checked)
	if err != nil {
		return Policy{}, err
	}
	row, err := service.queries.InsertPolicy(ctx, queries.InsertPolicyParams{
		PolicyID:       identifiers.New(),
		Environment:    queries.Environment(environment),
		Name:           checked.Name,
		Level:          string(checked.Scope.Level),
		PlanID:         checked.Scope.PlanID,
		CustomerID:     checked.Scope.CustomerID,
		Feature:        checked.Feature,
		ConditionGroup: conditionGroup,
		Action:         action,
		Enforcement:    string(checked.Enforcement),
		OnUnreachable:  string(checked.OnUnreachable),
		OnUncosted:     string(checked.OnUncosted),
		Status:         queries.RecordStatus(checked.Status),
		CreatedAt:      service.clock.Now(),
	})
	if err != nil {
		return Policy{}, fmt.Errorf("insert policy: %w", err)
	}
	policy, err := policyFromRow(row)
	if err != nil {
		return Policy{}, err
	}
	if err := service.announceChange(ctx, environment, policy.ID); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

// Update applies changes, a JSON object of policy document fields, to the
// policy with policyID in environment under its row lock. Each field of
// changes replaces the stored field whole, so when and action are replaced
// as a unit and null clears plan_id, customer_id and feature. The result is
// checked like a document for Create, stored with the next version and the
// current time as updated_at, and then Update clears the Cache and publishes
// the policies invalidation. It returns httpapi.ErrNotFound when environment
// has no such policy and a 422 policy_invalid problem for an invalid result.
// A plan archived after the policy stored it stays accepted, and moving the
// policy to a plan that is missing from environment or archived returns
// plans.ErrPlanNotFound.
func (service *Service) Update(ctx context.Context, environment httpapi.Environment, policyID uuid.UUID, changes []byte) (Policy, error) {
	var policy Policy
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		var err error
		policy, err = service.updateLocked(ctx, service.queries.WithTx(transaction), environment, policyID, changes)
		return err
	})
	if err != nil {
		return Policy{}, err
	}
	if err := service.announceChange(ctx, environment, policy.ID); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

// Get returns the policy with policyID in environment, or
// httpapi.ErrNotFound when environment has no such policy.
func (service *Service) Get(ctx context.Context, environment httpapi.Environment, policyID uuid.UUID) (Policy, error) {
	row, err := service.queries.SelectPolicy(ctx, queries.SelectPolicyParams{Environment: queries.Environment(environment), PolicyID: policyID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, httpapi.ErrNotFound
	}
	if err != nil {
		return Policy{}, fmt.Errorf("select policy %s: %w", policyID, err)
	}
	return policyFromRow(row)
}

// List returns one page of the policies of environment that filter keeps,
// newest first, and the cursor of the next page, which is empty on the last
// page. A plan id that is not a plan of environment keeps no policy. An
// empty cursor starts at the newest policy. A limit of 0 selects
// httpapi.ListLimitDefault, and a limit outside 1 to
// httpapi.ListLimitMaximum returns a 422 validation_failed problem at
// query.limit. A cursor that List did not return for environment fails with
// httpapi.ErrInvalidCursor.
func (service *Service) List(ctx context.Context, environment httpapi.Environment, filter ListFilter, cursor string, limit int) ([]Policy, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	parameters := queries.ListPoliciesParams{
		Environment: queries.Environment(environment),
		Status:      (*string)(filter.Status),
		PlanID:      filter.PlanID,
		RowLimit:    int64(pageSize) + 1,
	}
	if cursor != "" {
		position, err := listCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		parameters.AfterCreatedAt = &position.CreatedAt
		parameters.AfterPolicyID = &position.PolicyID
	}
	rows, err := service.queries.ListPolicies(ctx, parameters)
	if err != nil {
		return nil, "", fmt.Errorf("list policies of environment %s: %w", environment, err)
	}
	nextCursor := ""
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		last := rows[pageSize-1]
		nextCursor, err = listCursor.Encode(environment, listPosition{CreatedAt: last.CreatedAt, PolicyID: last.PolicyID})
		if err != nil {
			return nil, "", err
		}
	}
	page, err := policiesFromRows(rows)
	if err != nil {
		return nil, "", err
	}
	return page, nextCursor, nil
}

func (service *Service) updateLocked(ctx context.Context, transactionQueries *queries.Queries, environment httpapi.Environment, policyID uuid.UUID, changes []byte) (Policy, error) {
	row, err := transactionQueries.SelectPolicyForUpdate(ctx, queries.SelectPolicyForUpdateParams{
		Environment: queries.Environment(environment),
		PolicyID:    policyID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, httpapi.ErrNotFound
	}
	if err != nil {
		return Policy{}, fmt.Errorf("select policy %s for update: %w", policyID, err)
	}
	current, err := policyFromRow(row)
	if err != nil {
		return Policy{}, err
	}
	changedFields, isObject := documentMembers(changes)
	if !isObject {
		return Policy{}, policyInvalid([]FieldError{{Path: documentPath, Message: documentRule}})
	}
	fields, err := storedMembers(current.Document)
	if err != nil {
		return Policy{}, err
	}
	maps.Copy(fields, changedFields)
	checked, err := service.checkedDocument(ctx, transactionQueries, environment, fields, current.Scope.PlanID)
	if err != nil {
		return Policy{}, err
	}
	conditionGroup, action, err := encodeRules(checked)
	if err != nil {
		return Policy{}, err
	}
	updated, err := transactionQueries.UpdatePolicy(ctx, queries.UpdatePolicyParams{
		Name:           checked.Name,
		Level:          string(checked.Scope.Level),
		PlanID:         checked.Scope.PlanID,
		CustomerID:     checked.Scope.CustomerID,
		Feature:        checked.Feature,
		ConditionGroup: conditionGroup,
		Action:         action,
		Enforcement:    string(checked.Enforcement),
		OnUnreachable:  string(checked.OnUnreachable),
		OnUncosted:     string(checked.OnUncosted),
		Status:         queries.RecordStatus(checked.Status),
		UpdatedAt:      service.clock.Now(),
		PolicyID:       policyID,
	})
	if err != nil {
		return Policy{}, fmt.Errorf("update policy %s: %w", policyID, err)
	}
	return policyFromRow(updated)
}

func (service *Service) checkedDraft(ctx context.Context, environment httpapi.Environment, document []byte) (Document, error) {
	fields, isObject := documentMembers(document)
	if !isObject {
		return Document{}, policyInvalid([]FieldError{{Path: documentPath, Message: documentRule}})
	}
	if _, present := fields[statusPath]; !present {
		fields[statusPath] = activeStatusJSON
	}
	return service.checkedDocument(ctx, service.queries, environment, fields, nil)
}

func (service *Service) checkedDocument(ctx context.Context, lookupQueries *queries.Queries, environment httpapi.Environment, fields map[string]json.RawMessage, storedPlanID *uuid.UUID) (Document, error) {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return Document{}, fmt.Errorf("encode policy document: %w", err)
	}
	document, fieldErrors := DecodeDocument(encoded)
	if len(fieldErrors) > 0 {
		return Document{}, policyInvalid(fieldErrors)
	}
	if err := resolveModelAliases(ctx, lookupQueries, document.Action.RouteChain); err != nil {
		return Document{}, err
	}
	var lookupErrors error
	fieldErrors = Validate(Policy{Document: document}, ValidationContext{
		CustomerExists: func(customerID uuid.UUID) bool {
			found, err := lookupQueries.CustomerExists(ctx, queries.CustomerExistsParams{Environment: queries.Environment(environment), CustomerID: customerID})
			lookupErrors = errors.Join(lookupErrors, err)
			return found
		},
		ParameterMappings: service.parameterMappings,
	})
	if lookupErrors != nil {
		return Document{}, fmt.Errorf("look up policy customer: %w", lookupErrors)
	}
	if len(fieldErrors) > 0 {
		return Document{}, policyInvalid(fieldErrors)
	}
	planKept := storedPlanID != nil && document.Scope.PlanID != nil && *storedPlanID == *document.Scope.PlanID
	if document.Scope.Level != LevelPlan || planKept {
		return document, nil
	}
	planStatus, err := lookupQueries.SelectPlanStatus(ctx, queries.SelectPlanStatusParams{Environment: queries.Environment(environment), PlanID: *document.Scope.PlanID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, plans.ErrPlanNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("select status of plan %s: %w", *document.Scope.PlanID, err)
	}
	if planStatus != queries.RecordStatusActive {
		return Document{}, plans.ErrPlanNotFound
	}
	return document, nil
}

func (service *Service) announceChange(ctx context.Context, environment httpapi.Environment, policyID uuid.UUID) error {
	service.activePolicies.Clear()
	return service.cache.PublishInvalidation(ctx, cache.Invalidation{
		Kind:        cache.InvalidationKindPolicies,
		Environment: string(environment),
		ID:          identifiers.Encode(identifiers.PrefixPolicy, policyID),
	})
}

func (service *Service) loadActivePolicies(ctx context.Context, environment httpapi.Environment) ([]Policy, error) {
	rows, err := service.queries.ListActivePolicies(ctx, queries.Environment(environment))
	if err != nil {
		return nil, fmt.Errorf("list active policies of environment %s: %w", environment, err)
	}
	return policiesFromRows(rows)
}

func resolveModelAliases(ctx context.Context, lookupQueries *queries.Queries, routeChain []RouteTarget) error {
	for index, target := range routeChain {
		if !validText(target.Provider, routeTargetNameMaximumLength) || !validText(target.Model, routeTargetNameMaximumLength) {
			continue
		}
		model, err := lookupQueries.SelectAliasedModel(ctx, queries.SelectAliasedModelParams{Provider: target.Provider, Alias: target.Model})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("select alias %s of provider %s: %w", target.Model, target.Provider, err)
		}
		routeChain[index].Model = model
	}
	return nil
}

func encodeRules(document Document) ([]byte, []byte, error) {
	conditionGroup, err := json.Marshal(document.When)
	if err != nil {
		return nil, nil, fmt.Errorf("encode condition group: %w", err)
	}
	action, err := json.Marshal(document.Action)
	if err != nil {
		return nil, nil, fmt.Errorf("encode action: %w", err)
	}
	return conditionGroup, action, nil
}

func policiesFromRows(rows []queries.Policy) ([]Policy, error) {
	decoded := make([]Policy, 0, len(rows))
	for _, row := range rows {
		policy, err := policyFromRow(row)
		if err != nil {
			return nil, err
		}
		decoded = append(decoded, policy)
	}
	return decoded, nil
}

func policyFromRow(row queries.Policy) (Policy, error) {
	var when ConditionGroup
	if err := json.Unmarshal(row.ConditionGroup, &when); err != nil {
		return Policy{}, fmt.Errorf("decode condition group of policy %s: %w", row.PolicyID, err)
	}
	var action Action
	if err := json.Unmarshal(row.Action, &action); err != nil {
		return Policy{}, fmt.Errorf("decode action of policy %s: %w", row.PolicyID, err)
	}
	return Policy{
		ID: row.PolicyID,
		Document: Document{
			Name:          row.Name,
			Scope:         Scope{Level: Level(row.Level), PlanID: row.PlanID, CustomerID: row.CustomerID},
			Feature:       row.Feature,
			When:          when,
			Action:        action,
			Enforcement:   Enforcement(row.Enforcement),
			OnUnreachable: Outcome(row.OnUnreachable),
			OnUncosted:    Outcome(row.OnUncosted),
			Status:        Status(row.Status),
		},
		Version:   row.Version,
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}, nil
}

func documentMembers(document []byte) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	err := json.Unmarshal(document, &fields)
	return fields, err == nil && fields != nil
}

func storedMembers(document Document) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(NewDocumentResponse(document))
	if err != nil {
		return nil, fmt.Errorf("encode stored policy document: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, fmt.Errorf("decode stored policy document: %w", err)
	}
	return fields, nil
}

func policyInvalid(fieldErrors []FieldError) *httpapi.Problem {
	problemErrors := make([]httpapi.ProblemError, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		location := bodyLocation
		if fieldError.Path != documentPath {
			location += "." + fieldError.Path
		}
		problemErrors = append(problemErrors, httpapi.ProblemError{Location: location, Message: fieldError.Message})
	}
	return httpapi.NewCodedValidationProblem("policy_invalid", "policy document is invalid", problemErrors...)
}
