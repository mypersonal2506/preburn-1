package decisions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/decisions/queries"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
	"github.com/preburn/preburn/internal/signals"
)

const (
	countersTimeout             = 150 * time.Millisecond
	defaultReservationTTL       = 10 * time.Minute
	usageEstimateMinimumSamples = 50

	decisionStatusReserved   = "reserved"
	decisionStatusUnreserved = "unreserved"

	checkDurationBucketStart  = 0.001
	checkDurationBucketFactor = 2
	checkDurationBucketCount  = 12
)

// EstimateBasis names the usage a check reserved cost for.
type EstimateBasis string

const (
	// EstimateBasisP95 reserved the cost of the 95th percentile usage of the
	// feature and model over the last 30 days.
	EstimateBasisP95 EstimateBasis = "p95"
	// EstimateBasisCeiling reserved the cost of the request's usage ceiling.
	EstimateBasisCeiling EstimateBasis = "ceiling"
	// EstimateBasisRequestEstimate reserved the cost of the request's usage
	// estimate.
	EstimateBasisRequestEstimate EstimateBasis = "request_estimate"
	// EstimateBasisNone reserved nothing: the check denied, or no usage of
	// the request could be priced.
	EstimateBasisNone EstimateBasis = "none"
)

// CheckRequest is the body of POST /api/v1/check: a provider request that a
// customer is about to make.
type CheckRequest struct {
	CustomerID     string             `json:"customer_id" doc:"Id of the customer in your system: 1 to 128 letters, digits or the characters . _ : @ -. An unknown customer is created and follows the default plan."`
	CustomerUserID *string            `json:"customer_user_id,omitempty" doc:"Id of the customer's user in your system, in the format of customer_id. An unknown user is created."`
	Feature        string             `json:"feature" doc:"Feature of your product the request serves: a lowercase letter followed by at most 63 lowercase letters, digits or underscores, such as text_to_video."`
	Provider       string             `json:"provider" doc:"Provider name, such as fal_ai or openai, 1 to 200 characters."`
	Model          string             `json:"model" doc:"Model name or one of its aliases, 1 to 200 characters."`
	Attributes     pricing.Attributes `json:"attributes,omitempty" doc:"At most 32 attributes of the request, such as resolution or audio. Known keys take the values of the attribute vocabulary, other keys take any value."`
	UsageEstimate  map[string]string  `json:"usage_estimate,omitempty" doc:"Expected quantity per meter, a non-negative decimal with at most 6 decimals, such as 8 for output_seconds."`
	UsageCeiling   map[string]string  `json:"usage_ceiling,omitempty" doc:"Largest quantity per meter the request can use, in the format of usage_estimate. Hard policies reserve its cost."`
}

// CheckResponse is the decision of a check: what to run and what the
// decision reserved.
type CheckResponse struct {
	DecisionID      string                            `json:"decision_id" doc:"Decision id, such as dec_01jbvagescfn78y0938nkrkayd. Reports and releases name it."`
	Outcome         policies.Outcome                  `json:"outcome" enum:"allow,route,cap,deny" doc:"allow runs the request as asked, route runs it on provider and model, cap runs it with overrides, deny rejects it."`
	Reason          policies.Reason                   `json:"reason" enum:"no_policy_matched,policy_matched,hard_limit_reached,route_chain_exhausted,uncosted_allowed,uncosted_denied,cap_not_applicable" doc:"Why the check decided the outcome."`
	Provider        string                            `json:"provider" doc:"Provider to run the request on, the requested one unless the outcome is route."`
	Model           string                            `json:"model" doc:"Model to run the request on, the requested one unless the outcome is route."`
	Overrides       map[string]policies.OverrideValue `json:"overrides" doc:"Attributes to set on the request, such as duration or audio, by Preburn attribute name. GET /api/v1/policies/parameter-mappings names the provider parameter of each attribute, such as generate_audio for audio. Empty unless the outcome is route or cap."`
	EstimatedCost   *string                           `json:"estimated_cost" doc:"USD cost of the usage estimate of the request as it runs, or of its ceiling without an estimate. Null when it is uncosted."`
	ReservedAmount  string                            `json:"reserved_amount" doc:"USD amount the decision holds against the customer's allowance until it is reported, released or expires."`
	EstimateBasis   EstimateBasis                     `json:"estimate_basis" enum:"p95,ceiling,request_estimate,none" doc:"Usage whose cost the decision reserved."`
	CostStatus      pricing.CostStatus                `json:"cost_status" enum:"costed,uncosted" doc:"uncosted when a meter of the request as it runs has no price."`
	MatchedPolicyID *string                           `json:"matched_policy_id" doc:"Policy that decided, such as pol_01jbvagescfn78y0938nkrkayd, or null."`
	FallbackOutcome policies.Outcome                  `json:"fallback_outcome" enum:"allow,deny" doc:"Outcome to use for this customer and feature when Preburn cannot be reached."`
	ExpiresAt       time.Time                         `json:"expires_at" doc:"When the reservation is released unless the request is reported first. The check time for a deny."`
	Signals         signals.Response                  `json:"signals" doc:"Margin signals of the customer's period at the check."`
}

// CheckDependencies are what a CheckService reads and writes.
type CheckDependencies struct {
	// Pool stores decisions and holds the usage estimates.
	Pool *pgxpool.Pool
	// Jobs inserts the rollup refresh job of a deny decision.
	Jobs *river.Client[pgx.Tx]
	// Customers resolves and creates customers and customer users through
	// its cache.
	Customers *customers.Service
	// CustomerStates loads the plan, period and net revenue of customers.
	CustomerStates *customerstate.Cache
	// RuleSets holds the pricing rules of each environment.
	RuleSets *pricing.RuleSetCache
	// Policies holds the active policies of each environment and the
	// parameter mappings.
	Policies *policies.Service
	// ModelAliases maps other model names to the models whose parameter
	// mappings apply to them.
	ModelAliases []catalogfiles.Alias
	// Counters keeps the period counters and reservations.
	Counters *Counters
	// Bootstrap rebuilds the counters when a check finds the counters_ready
	// marker missing.
	Bootstrap *CounterBootstrap
	// Stream feeds the dashboard's decision stream.
	Stream *StreamWriter
	// Clock tells the time of each check.
	Clock clock.Clock
	// Logger writes the events of failed checks, pending changes and stream
	// appends.
	Logger *logging.Logger
	// Registry receives preburn_check_duration_seconds and
	// preburn_decisions_total.
	Registry prometheus.Registerer
}

// CheckService decides checks. Create one with NewCheckService. It is safe
// for concurrent use.
type CheckService struct {
	pool           *pgxpool.Pool
	queries        *queries.Queries
	jobs           *river.Client[pgx.Tx]
	customers      *customers.Service
	customerStates *customerstate.Cache
	ruleSets       *pricing.RuleSetCache
	policies       *policies.Service
	modelAliases   map[catalogfiles.ModelKey]string
	counters       *Counters
	bootstrap      *CounterBootstrap
	stream         *StreamWriter
	clock          clock.Clock
	logger         *logging.Logger
	durations      *prometheus.HistogramVec
	decided        *prometheus.CounterVec
}

type checkInputs struct {
	environment    httpapi.Environment
	feature        string
	customer       customers.Customer
	customerUserID *uuid.UUID
	state          signals.CustomerState
	ruleSet        *pricing.RuleSet
	activePolicies []policies.Policy
	usageEstimates map[catalogfiles.ModelKey]usage
	now            time.Time
}

type decision struct {
	id         uuid.UUID
	outcome    policies.Outcome
	reason     policies.Reason
	resolution policies.Resolution
	requested  pricedRequest
	run        pricedRequest
	signals    signals.Signals
	reserved   money.Amount
	basis      EstimateBasis
	status     string
	expiresAt  time.Time
	counted    bool
}

var (
	// ErrCountersUnavailable is the CodedError of a check whose counter could
	// not be read or reserved in Redis within 150 ms, or while the
	// counters_ready marker is missing: 503 counters_unavailable. The SDK
	// falls back.
	ErrCountersUnavailable = httpapi.NewCodedError(http.StatusServiceUnavailable, "counters_unavailable", "the counters are unavailable")
	// ErrDatabaseUnavailable is the CodedError of a check that failed on a
	// Postgres read or write: 503 database_unavailable. The SDK falls back.
	ErrDatabaseUnavailable = httpapi.NewCodedError(http.StatusServiceUnavailable, "database_unavailable", "the database is unavailable")
)

// NewCheckService returns a CheckService over dependencies and registers
// preburn_check_duration_seconds{outcome} and
// preburn_decisions_total{environment,outcome,reason} on
// dependencies.Registry. It returns an error when either is already
// registered there.
func NewCheckService(dependencies CheckDependencies) (*CheckService, error) {
	durations := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "preburn_check_duration_seconds",
		Help:    "Duration in seconds of checks that decided, by outcome.",
		Buckets: prometheus.ExponentialBuckets(checkDurationBucketStart, checkDurationBucketFactor, checkDurationBucketCount),
	}, []string{"outcome"})
	decided := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "preburn_decisions_total",
		Help: "Decisions by environment, outcome and reason.",
	}, []string{"environment", "outcome", "reason"})
	for _, collector := range []prometheus.Collector{durations, decided} {
		if err := dependencies.Registry.Register(collector); err != nil {
			return nil, fmt.Errorf("register check metrics: %w", err)
		}
	}
	modelAliases := make(map[catalogfiles.ModelKey]string, len(dependencies.ModelAliases))
	for _, alias := range dependencies.ModelAliases {
		modelAliases[catalogfiles.ModelKey{Provider: alias.Provider, Model: alias.Alias}] = alias.Model
	}
	return &CheckService{
		pool:           dependencies.Pool,
		queries:        queries.New(dependencies.Pool),
		jobs:           dependencies.Jobs,
		customers:      dependencies.Customers,
		customerStates: dependencies.CustomerStates,
		ruleSets:       dependencies.RuleSets,
		policies:       dependencies.Policies,
		modelAliases:   modelAliases,
		counters:       dependencies.Counters,
		bootstrap:      dependencies.Bootstrap,
		stream:         dependencies.Stream,
		clock:          dependencies.Clock,
		logger:         dependencies.Logger,
		durations:      durations,
		decided:        decided,
	}, nil
}

// Check decides request in environment:
//  1. It validates the request, then resolves the customer and the customer
//     user through the customer cache, creating unknown ones.
//  2. It loads the customer state, the rule set, the active policies and the
//     usage estimates of the feature, and rates the usage estimate of the
//     requested model, or its ceiling without an estimate.
//  3. Within 150 ms it reads the period counter, computes the signals,
//     resolves the policies, applies the outcome and runs the reservation. A
//     route takes the first target that can be priced and whose reservation
//     fits the allowance under a hard allowance condition. A cap re-rates the
//     request with its overrides. A request left uncosted follows the
//     matched policy's on_uncosted. A deny only counts the decision. Hard
//     enforcement reserves the rated ceiling, else the estimate. Soft
//     enforcement reserves the p95 usage estimate of at least 50 samples,
//     else the ceiling, else the estimate. A cap limit bounds the checked
//     feature, or every feature when the policy has none. A reservation the
//     script refuses becomes a deny with reason hard_limit_reached.
//  4. It stores the decision, with the rollup refresh job of the customer
//     period for a deny, and appends it to the decision stream.
//
// A reservation holds for the plan's hold time of the feature, else 10
// minutes. The reserve script registers the decision as a pending change of
// the counter, which ends once the decision is stored. Invalid fields return
// a 422 validation_failed problem. A Redis failure returns
// ErrCountersUnavailable and a Postgres failure ErrDatabaseUnavailable, both
// logged with their cause. A failure after the client closed the request
// logs nothing and returns its cause, which the API answers with 499
// client_closed_request. While the counters_ready marker is missing the
// reserve script changes nothing, the check returns ErrCountersUnavailable
// and asks Bootstrap for a rebuild without waiting for it. A decision whose
// reservation was made but could not be stored has its reservation released.
func (service *CheckService) Check(ctx context.Context, environment httpapi.Environment, request CheckRequest) (CheckResponse, error) {
	started := time.Now()
	parsed, err := parseCheckRequest(request)
	if err != nil {
		return CheckResponse{}, err
	}
	inputs, err := service.loadInputs(ctx, environment, request)
	if err != nil {
		return CheckResponse{}, service.databaseFailure(ctx, err)
	}
	requested, err := parsed.plan(request.Provider, request.Model, inputs.usageEstimates).price(inputs.ruleSet, inputs.now)
	if err != nil {
		return CheckResponse{}, err
	}
	decided, err := service.decide(ctx, inputs, requested)
	if err != nil {
		return CheckResponse{}, err
	}
	if err := service.record(ctx, inputs, decided); err != nil {
		if decided.status == decisionStatusReserved {
			err = errors.Join(err, service.releaseUnrecorded(ctx, decided.id))
		}
		service.endChange(ctx, inputs, decided)
		return CheckResponse{}, service.databaseFailure(ctx, err)
	}
	service.endChange(ctx, inputs, decided)
	service.stream.Append(ctx, environment, decided.streamEntry(inputs))
	service.decided.WithLabelValues(string(environment), string(decided.outcome), string(decided.reason)).Inc()
	service.durations.WithLabelValues(string(decided.outcome)).Observe(time.Since(started).Seconds())
	return decided.response(), nil
}

func (service *CheckService) loadInputs(ctx context.Context, environment httpapi.Environment, request CheckRequest) (checkInputs, error) {
	inputs := checkInputs{environment: environment, feature: request.Feature, now: service.clock.Now()}
	var err error
	if inputs.customer, err = service.customers.Cache().Ensure(ctx, environment, request.CustomerID); err != nil {
		return checkInputs{}, err
	}
	if request.CustomerUserID != nil {
		user, err := service.customers.Cache().EnsureUser(ctx, environment, inputs.customer.ID, *request.CustomerUserID)
		if err != nil {
			return checkInputs{}, err
		}
		inputs.customerUserID = &user.ID
	}
	if inputs.state, err = service.customerStates.Load(ctx, environment, inputs.customer.ID, inputs.now); err != nil {
		return checkInputs{}, fmt.Errorf("load state of customer %s: %w", inputs.customer.ID, err)
	}
	if inputs.ruleSet, err = service.ruleSets.Get(ctx, environment); err != nil {
		return checkInputs{}, fmt.Errorf("load rule set: %w", err)
	}
	if inputs.activePolicies, err = service.policies.Cache().Active(ctx, environment); err != nil {
		return checkInputs{}, fmt.Errorf("load active policies: %w", err)
	}
	if inputs.usageEstimates, err = service.loadUsageEstimates(ctx, environment, request.Feature); err != nil {
		return checkInputs{}, err
	}
	return inputs, nil
}

func (service *CheckService) loadUsageEstimates(ctx context.Context, environment httpapi.Environment, feature string) (map[catalogfiles.ModelKey]usage, error) {
	rows, err := service.queries.ListFeatureUsageEstimates(ctx, queries.ListFeatureUsageEstimatesParams{
		Environment: queries.Environment(environment),
		Feature:     feature,
	})
	if err != nil {
		return nil, fmt.Errorf("list usage estimates of feature %s: %w", feature, err)
	}
	estimates := map[catalogfiles.ModelKey]usage{}
	for _, row := range rows {
		if row.SampleCount < usageEstimateMinimumSamples {
			continue
		}
		meter, err := pricing.ParseMeter(row.Meter)
		if err != nil {
			return nil, fmt.Errorf("usage estimate of %s %s: %w", row.Provider, row.Model, err)
		}
		model := catalogfiles.ModelKey{Provider: row.Provider, Model: row.Model}
		if estimates[model] == nil {
			estimates[model] = usage{}
		}
		estimates[model][meter] = money.Quantity(row.P95QuantityMicros)
	}
	return estimates, nil
}

func (service *CheckService) decide(ctx context.Context, inputs checkInputs, requested pricedRequest) (decision, error) {
	countersContext, cancel := context.WithTimeout(ctx, countersTimeout)
	defer cancel()
	snapshot, err := service.counters.Snapshot(countersContext, inputs.environment, inputs.customer.ID, inputs.state.Period.Start, inputs.feature)
	if err != nil {
		return decision{}, service.countersFailure(ctx, err)
	}
	signalValues, err := signals.Compute(inputs.state, snapshot, requested.expectedCost(), inputs.now)
	if err != nil {
		return decision{}, err
	}
	resolution := policies.Resolve(inputs.activePolicies, policies.ResolutionRequest{
		CustomerID:      inputs.customer.ID,
		PlanID:          inputs.state.PlanID,
		Feature:         inputs.feature,
		ModelParameters: service.modelParameters(requested.provider, requested.model),
	}, signalValues)
	decided, err := service.applyOutcome(inputs, requested, resolution, signalValues)
	if err != nil {
		return decision{}, err
	}
	decided, err = service.reserve(countersContext, inputs, decided)
	if errors.Is(err, ErrCountersNotReady) {
		service.bootstrap.RequestRebuild()
	}
	if err != nil {
		return decision{}, service.countersFailure(ctx, err)
	}
	return decided, nil
}

func (service *CheckService) applyOutcome(inputs checkInputs, requested pricedRequest, resolution policies.Resolution, signalValues signals.Signals) (decision, error) {
	decided := decision{
		id:         identifiers.New(),
		outcome:    resolution.Outcome,
		reason:     resolution.Reason,
		resolution: resolution,
		requested:  requested,
		run:        requested,
		signals:    signalValues,
	}
	switch resolution.Outcome {
	case policies.OutcomeDeny:
		return decided.denied(resolution.Reason), nil
	case policies.OutcomeRoute:
		target, found, err := service.routeTarget(inputs, requested, resolution, signalValues)
		if err != nil {
			return decision{}, err
		}
		if !found {
			return decided.denied(policies.ReasonRouteChainExhausted), nil
		}
		decided.run = target
	case policies.OutcomeCap:
		capped, err := requested.withOverrides(resolution.Overrides, service.modelParameters(requested.provider, requested.model)).price(inputs.ruleSet, inputs.now)
		if err != nil {
			return decision{}, err
		}
		decided.run = capped
	case policies.OutcomeAllow:
	}
	if decided.run.expectedCost() == nil {
		if resolution.OnUncosted == policies.OutcomeDeny {
			return decided.denied(policies.ReasonUncostedDenied), nil
		}
		decided.reason = policies.ReasonUncostedAllowed
	}
	decided.reserved, decided.basis = decided.run.reservation(resolution.Enforcement)
	return decided, nil
}

func (service *CheckService) routeTarget(inputs checkInputs, requested pricedRequest, resolution policies.Resolution, signalValues signals.Signals) (pricedRequest, bool, error) {
	for _, target := range resolution.RouteChain {
		parameters := service.modelParameters(target.Provider, target.Model)
		planned := requested.retarget(target.Provider, target.Model, inputs.usageEstimates).
			withOverrides(policies.ApplicableOverrides(resolution.Overrides, parameters), parameters)
		priced, err := planned.price(inputs.ruleSet, inputs.now)
		if err != nil {
			return pricedRequest{}, false, err
		}
		if priced.expectedCost() == nil {
			continue
		}
		if amount, _ := priced.reservation(resolution.Enforcement); resolution.HardAllowanceCondition && amount > signalValues.AllowanceRemaining {
			continue
		}
		return priced, true, nil
	}
	return pricedRequest{}, false, nil
}

func (service *CheckService) reserve(ctx context.Context, inputs checkInputs, decided decision) (decision, error) {
	request := decided.reserveRequest(inputs)
	if decided.outcome == policies.OutcomeDeny {
		if _, err := service.counters.CountOnly(ctx, request); err != nil {
			return decision{}, err
		}
		decided.status = decisionStatusUnreserved
		decided.expiresAt = inputs.now
		decided.counted = true
		return decided, nil
	}
	request.Ceilings = decided.ceilings()
	result, err := service.counters.Reserve(ctx, request)
	if err != nil {
		return decision{}, err
	}
	switch result {
	case ReserveResultReserved:
		decided.status = decisionStatusReserved
		decided.expiresAt = request.ExpiresAt
		decided.counted = true
	case ReserveResultDeniedAllowance, ReserveResultDeniedLimit:
		decided = decided.denied(policies.ReasonHardLimitReached)
		decided.status = decisionStatusUnreserved
		decided.expiresAt = inputs.now
	case ReserveResultCounted:
		return decision{}, fmt.Errorf("reserve of decision %s returned %s", decided.id, result)
	}
	return decided, nil
}

func (service *CheckService) record(ctx context.Context, inputs checkInputs, decided decision) error {
	parameters, err := decided.insertParameters(inputs)
	if err != nil {
		return err
	}
	if decided.outcome != policies.OutcomeDeny {
		if err := service.queries.InsertDecision(ctx, parameters); err != nil {
			return fmt.Errorf("insert decision: %w", err)
		}
		return nil
	}
	return database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		if err := service.queries.WithTx(transaction).InsertDecision(ctx, parameters); err != nil {
			return fmt.Errorf("insert decision: %w", err)
		}
		refresh := ledger.NewRollupRefreshArgs(inputs.environment, inputs.customer.ID, inputs.state.Period.Start)
		if _, err := service.jobs.InsertTx(ctx, transaction, refresh, nil); err != nil {
			return fmt.Errorf("insert rollup refresh: %w", err)
		}
		return nil
	})
}

func (service *CheckService) endChange(ctx context.Context, inputs checkInputs, decided decision) {
	if !decided.counted {
		return
	}
	countersContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), countersTimeout)
	defer cancel()
	if err := service.counters.EndChange(countersContext, decided.reserveRequest(inputs)); err != nil {
		service.logger.Warn(ctx, logging.DecisionsCounterChangeEndFailed,
			slog.String("environment", string(inputs.environment)),
			slog.String("decision_id", identifiers.Encode(identifiers.PrefixDecision, decided.id)),
			slog.String("error", err.Error()),
		)
	}
}

func (service *CheckService) releaseUnrecorded(ctx context.Context, decisionID uuid.UUID) error {
	countersContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), countersTimeout)
	defer cancel()
	if _, err := service.counters.Release(countersContext, decisionID, ReservationStatusReleased); err != nil {
		return fmt.Errorf("release unrecorded reservation %s: %w", decisionID, err)
	}
	return nil
}

func (service *CheckService) modelParameters(provider string, model string) map[string]catalogfiles.Parameter {
	key := catalogfiles.ModelKey{Provider: provider, Model: model}
	if canonical, isAlias := service.modelAliases[key]; isAlias {
		key.Model = canonical
	}
	return service.policies.ParameterMappings()[key]
}

func (service *CheckService) countersFailure(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return err
	}
	service.logger.Error(ctx, logging.DecisionsCountersUnavailable, slog.String("error", err.Error()))
	return fmt.Errorf("%w: %w", ErrCountersUnavailable, err)
}

func (service *CheckService) databaseFailure(ctx context.Context, err error) error {
	if _, isProblem := errors.AsType[*httpapi.Problem](err); isProblem {
		return err
	}
	if _, isCoded := errors.AsType[httpapi.CodedError](err); isCoded {
		return err
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return err
	}
	service.logger.Error(ctx, logging.DecisionsDatabaseUnavailable, slog.String("error", err.Error()))
	return fmt.Errorf("%w: %w", ErrDatabaseUnavailable, err)
}

func (decided decision) denied(reason policies.Reason) decision {
	decided.outcome = policies.OutcomeDeny
	decided.reason = reason
	decided.run = decided.requested
	decided.reserved = 0
	decided.basis = EstimateBasisNone
	return decided
}

func (decided decision) reserveRequest(inputs checkInputs) ReserveRequest {
	return ReserveRequest{
		Environment: inputs.environment,
		CustomerID:  inputs.customer.ID,
		PeriodStart: inputs.state.Period.Start,
		PeriodEnd:   inputs.state.Period.End,
		DecisionID:  decided.id,
		Feature:     inputs.feature,
		Amount:      decided.reserved,
		ExpiresAt:   inputs.now.Add(holdTime(inputs.state, inputs.feature)),
		DecidedAt:   inputs.now,
	}
}

func (decided decision) ceilings() Ceilings {
	var ceilings Ceilings
	if decided.resolution.HardAllowanceCondition {
		ceilings.Allowance = &decided.signals.CostAllowance
	}
	limit := decided.resolution.Limit
	if limit == nil {
		return ceilings
	}
	allFeatures := decided.resolution.MatchedPolicy.Feature == nil
	switch limit.Kind {
	case policies.LimitKindCount:
		if allFeatures {
			ceilings.TotalCount = &limit.Count
		} else {
			ceilings.FeatureCount = &limit.Count
		}
	case policies.LimitKindAmount:
		if allFeatures {
			ceilings.TotalAmount = &limit.Amount
		} else {
			ceilings.FeatureAmount = &limit.Amount
		}
	}
	return ceilings
}

func (decided decision) insertParameters(inputs checkInputs) (queries.InsertDecisionParams, error) {
	attributes, err := json.Marshal(decided.requested.attributes)
	if err != nil {
		return queries.InsertDecisionParams{}, fmt.Errorf("encode attributes: %w", err)
	}
	overrides, err := json.Marshal(decided.run.overrides)
	if err != nil {
		return queries.InsertDecisionParams{}, fmt.Errorf("encode overrides: %w", err)
	}
	signalValues, err := json.Marshal(decided.signals)
	if err != nil {
		return queries.InsertDecisionParams{}, fmt.Errorf("encode signals: %w", err)
	}
	parameters := queries.InsertDecisionParams{
		DecisionID:                  decided.id,
		Environment:                 queries.Environment(inputs.environment),
		CustomerID:                  inputs.customer.ID,
		CustomerUserID:              inputs.customerUserID,
		Feature:                     inputs.feature,
		RequestedProvider:           decided.requested.provider,
		RequestedModel:              decided.requested.model,
		Provider:                    decided.run.provider,
		Model:                       decided.run.model,
		Attributes:                  attributes,
		Overrides:                   overrides,
		Outcome:                     string(decided.outcome),
		Reason:                      string(decided.reason),
		Signals:                     signalValues,
		RequestedEstimatedCostNanos: decided.requested.expectedCost(),
		EstimatedCostNanos:          decided.run.expectedCost(),
		ReservedNanos:               decided.reserved,
		EstimateBasis:               string(decided.basis),
		Status:                      decided.status,
		PeriodStart:                 inputs.state.Period.Start,
		PeriodEnd:                   inputs.state.Period.End,
		ExpiresAt:                   decided.expiresAt,
		CreatedAt:                   inputs.now,
	}
	if policy := decided.resolution.MatchedPolicy; policy != nil {
		parameters.MatchedPolicyID = &policy.ID
		parameters.MatchedPolicyVersion = &policy.Version
	}
	return parameters, nil
}

func (decided decision) streamEntry(inputs checkInputs) StreamEntry {
	entry := StreamEntry{
		DecisionID:          decided.id,
		CreatedAt:           inputs.now,
		CustomerID:          inputs.customer.ID,
		CustomerExternalID:  inputs.customer.ExternalID,
		CustomerDisplayName: inputs.customer.DisplayName,
		Feature:             inputs.feature,
		RequestedModel:      decided.requested.model,
		Model:               decided.run.model,
		Outcome:             decided.outcome,
		Reason:              decided.reason,
		EstimatedCost:       decided.run.expectedCost(),
	}
	if policy := decided.resolution.MatchedPolicy; policy != nil {
		entry.MatchedPolicyID = &policy.ID
	}
	return entry
}

func (decided decision) response() CheckResponse {
	response := CheckResponse{
		DecisionID:      identifiers.Encode(identifiers.PrefixDecision, decided.id),
		Outcome:         decided.outcome,
		Reason:          decided.reason,
		Provider:        decided.run.provider,
		Model:           decided.run.model,
		Overrides:       decided.run.overrides,
		ReservedAmount:  money.FormatAmount(decided.reserved),
		EstimateBasis:   decided.basis,
		CostStatus:      pricing.CostStatusUncosted,
		FallbackOutcome: decided.resolution.FallbackOutcome,
		ExpiresAt:       decided.expiresAt,
		Signals:         signals.NewResponse(decided.signals),
	}
	if cost := decided.run.expectedCost(); cost != nil {
		response.EstimatedCost = new(money.FormatAmount(*cost))
		response.CostStatus = pricing.CostStatusCosted
	}
	if policy := decided.resolution.MatchedPolicy; policy != nil {
		response.MatchedPolicyID = new(identifiers.Encode(identifiers.PrefixPolicy, policy.ID))
	}
	return response
}

func holdTime(state signals.CustomerState, feature string) time.Duration {
	if seconds, found := state.HoldTimes[feature]; found {
		return time.Duration(seconds) * time.Second
	}
	return defaultReservationTTL
}
