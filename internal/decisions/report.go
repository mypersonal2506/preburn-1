package decisions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/cache"
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
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
	"github.com/preburn/preburn/internal/signals"
)

const (
	reportBatchMaximum           = 500
	decisionIdempotencyKeyPrefix = "decision:"
	fallbackCountIncrement       = 1
	droppedReportsKeyName        = "dropped_reports"
	droppedReportsDayLayout      = "20060102"
	droppedReportsRetention      = 8 * 24 * time.Hour
	internalErrorCode            = "internal_error"
	internalErrorDetail          = "internal error"

	bodyLocation             = "body"
	batchReportLocation      = "body.reports[%d]"
	reportsLocation          = "body.reports"
	decisionSourceLocation   = "body.decision_source"
	decisionIDLocation       = "body.decision_id"
	idempotencyKeyLocation   = "body.idempotency_key"
	reportCustomerIDLocation = "body.customer_id"
	usageLocation            = "body.usage"

	decisionSourceRule     = "expected server or fallback"
	decisionIDRule         = "expected a decision id such as dec_01jbvagescfn78y0938nkrkayd"
	fallbackDecisionIDRule = "expected no decision id in a fallback report"
	idempotencyKeyRule     = "expected a UUID"
	reportCustomerIDRule   = "expected the id of the customer in your system"
	usageRule              = "expected the quantity used per meter"
)

// ReportRequest is one usage report: the body of POST /api/v1/report and one
// report of POST /api/v1/reports. A server report names its decision, which
// supplies the customer, feature, provider, model and attributes. A fallback
// report names them itself.
type ReportRequest struct {
	DecisionSource ledger.DecisionSource `json:"decision_source,omitempty" doc:"Required. server for the usage of a checked decision named by decision_id, fallback for usage that ran without a decision, such as on the fallback outcome while Preburn was unreachable."`
	DecisionID     string                `json:"decision_id,omitempty" doc:"Decision the usage belongs to, such as dec_01jbvagescfn78y0938nkrkayd. Required in a server report and rejected in a fallback report. Reporting a decision again returns its first ledger entry."`
	IdempotencyKey string                `json:"idempotency_key,omitempty" doc:"A UUID you generate for a fallback report. Reporting the same key again returns the first ledger entry. Ignored in a server report."`
	CustomerID     string                `json:"customer_id,omitempty" doc:"Id of the customer in your system, for a fallback report. An unknown customer is created and follows the default plan. Ignored in a server report."`
	Feature        string                `json:"feature,omitempty" doc:"Feature of your product the usage served, for a fallback report, such as text_to_video. Ignored in a server report."`
	Provider       string                `json:"provider,omitempty" doc:"Provider the request ran on, for a fallback report, 1 to 200 characters. Ignored in a server report."`
	Model          string                `json:"model,omitempty" doc:"Model the request ran on or one of its aliases, for a fallback report, 1 to 200 characters. Ignored in a server report."`
	Attributes     pricing.Attributes    `json:"attributes,omitempty" doc:"At most 32 attributes of the request as it ran, such as resolution. In a server report they replace the decision's attributes of the same key before rating."`
	Usage          map[string]string     `json:"usage,omitempty" doc:"Required. Quantity the request used per meter, a non-negative decimal with at most 6 decimals, such as 6 for output_seconds."`
	OccurredAt     *time.Time            `json:"occurred_at,omitempty" doc:"When the request ran. It picks the prices in effect and, in a fallback report, the customer period. The time of the report when omitted."`
}

// ReportResult is the ledger entry a report stored or found: the body of the
// 202 answer to POST /api/v1/report.
type ReportResult struct {
	LedgerEntryID string             `json:"ledger_entry_id" doc:"Ledger entry id, such as led_01jbvagescfn78y0938nkrkayd."`
	Cost          *string            `json:"cost" doc:"USD cost of the usage. Null when it is uncosted."`
	CostStatus    pricing.CostStatus `json:"cost_status" enum:"costed,uncosted" doc:"uncosted when a meter of the usage has no price."`
	Duplicate     bool               `json:"duplicate" doc:"True when an earlier report of the same decision or idempotency key stored the entry and this one changed nothing."`
}

// ReportBatchResult is the result of one report of POST /api/v1/reports, at
// the index of the report.
type ReportBatchResult struct {
	Status int            `json:"status" doc:"202 when the report stored or found its ledger entry, else the status the report alone would have answered, such as 422."`
	Result *ReportResult  `json:"result,omitempty" doc:"The ledger entry, present when the report succeeded."`
	Error  *ReportFailure `json:"error,omitempty" doc:"Why the report failed, present when it failed."`
}

// ReportFailure is why one report of a batch failed: the code, detail and
// invalid fields of the problem the report alone would have answered.
type ReportFailure struct {
	Code   string                 `json:"code" doc:"Stable error code, such as validation_failed."`
	Detail string                 `json:"detail" doc:"Explanation of the failure."`
	Errors []httpapi.ProblemError `json:"errors,omitempty" nullable:"false" doc:"Invalid fields of the report, at locations such as body.reports[3].usage."`
}

// ReportDependencies are what a ReportService reads and writes.
type ReportDependencies struct {
	// Pool stores ledger entries and decision statuses.
	Pool *pgxpool.Pool
	// Jobs inserts the rollup refresh job of each new ledger entry.
	Jobs *river.Client[pgx.Tx]
	// Customers resolves and creates the customers of fallback reports
	// through its cache.
	Customers *customers.Service
	// CustomerStates gives the period of fallback reports: the cached state
	// for usage that occurred at the time of the report, and a state loaded
	// through its Loader for usage that occurred earlier.
	CustomerStates *customerstate.Cache
	// RuleSets holds the pricing rules of each environment.
	RuleSets *pricing.RuleSetCache
	// Policies holds the parameter mappings.
	Policies *policies.Service
	// ModelAliases maps other model names to the models whose parameter
	// mappings apply to them.
	ModelAliases []catalogfiles.Alias
	// Counters keeps the period counters and reservations.
	Counters *Counters
	// Clock tells the time of each report and release.
	Clock clock.Clock
	// Logger writes the events of deferred settles and releases.
	Logger *logging.Logger
	// Registry receives preburn_reports_total and
	// preburn_dropped_reports_total.
	Registry prometheus.Registerer
}

// ReportService stores usage reports in the ledger and releases decisions.
// Create one with NewReportService. It is safe for concurrent use.
type ReportService struct {
	pool           *pgxpool.Pool
	queries        *queries.Queries
	jobs           *river.Client[pgx.Tx]
	customers      *customers.Service
	customerStates *customerstate.Cache
	ruleSets       *pricing.RuleSetCache
	policies       *policies.Service
	modelAliases   map[catalogfiles.ModelKey]string
	counters       *Counters
	clock          clock.Clock
	logger         *logging.Logger
	reported       *prometheus.CounterVec
	droppedReports *prometheus.CounterVec
}

type storedReport struct {
	entry        ledger.Entry
	duplicate    bool
	registration error
}

type parsedReport struct {
	source             ledger.DecisionSource
	decisionID         uuid.UUID
	idempotencyKey     string
	customerExternalID string
	feature            string
	provider           string
	model              string
	attributes         pricing.Attributes
	usage              usage
	occurredAt         time.Time
}

var (
	// ErrDecisionNotReportable is the CodedError of a report whose decision
	// was denied and reserved nothing: 409 decision_not_reportable.
	ErrDecisionNotReportable = httpapi.NewCodedError(http.StatusConflict, "decision_not_reportable", "the decision was denied and reserved nothing")

	reportsRule = fmt.Sprintf("expected at most %d reports", reportBatchMaximum)
)

// NewReportService returns a ReportService over dependencies and registers
// preburn_reports_total{environment,cost_status,duplicate} and
// preburn_dropped_reports_total{environment} on dependencies.Registry. It
// returns an error when either is already registered there.
func NewReportService(dependencies ReportDependencies) (*ReportService, error) {
	reported := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "preburn_reports_total",
		Help: "Usage reports stored or found, by environment, cost status and whether an earlier report stored the entry.",
	}, []string{"environment", "cost_status", "duplicate"})
	droppedReports := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "preburn_dropped_reports_total",
		Help: "Usage reports the SDK dropped, from the Preburn-Dropped-Reports header, by environment.",
	}, []string{"environment"})
	for _, collector := range []prometheus.Collector{reported, droppedReports} {
		if err := dependencies.Registry.Register(collector); err != nil {
			return nil, fmt.Errorf("register report metrics: %w", err)
		}
	}
	modelAliases := make(map[catalogfiles.ModelKey]string, len(dependencies.ModelAliases))
	for _, alias := range dependencies.ModelAliases {
		modelAliases[catalogfiles.ModelKey{Provider: alias.Provider, Model: alias.Alias}] = alias.Model
	}
	return &ReportService{
		pool:           dependencies.Pool,
		queries:        queries.New(dependencies.Pool),
		jobs:           dependencies.Jobs,
		customers:      dependencies.Customers,
		customerStates: dependencies.CustomerStates,
		ruleSets:       dependencies.RuleSets,
		policies:       dependencies.Policies,
		modelAliases:   modelAliases,
		counters:       dependencies.Counters,
		clock:          dependencies.Clock,
		logger:         dependencies.Logger,
		reported:       reported,
		droppedReports: droppedReports,
	}, nil
}

// DroppedReportsKey returns the key of the dropped report counter of
// environment for the UTC day of day in cacheClient:
// dropped_reports:{environment}:{yyyymmdd}, after the key prefix.
func DroppedReportsKey(cacheClient *cache.Client, environment httpapi.Environment, day time.Time) string {
	return cacheClient.Key(droppedReportsKeyName, string(environment), day.UTC().Format(droppedReportsDayLayout))
}

// Report stores the usage of request as a ledger entry of environment and
// returns it with duplicate false.
//
// A server report locks its decision, rates the usage at occurred_at with
// the decision's provider and model, and with the decision's attributes
// after its overrides that set a pricing attribute and then the report's
// attributes. It stores the entry in the decision's period under the
// idempotency key decision:<decision UUID>, marks the decision settled and
// inserts the rollup refresh job of the period, in one transaction. A
// fallback report creates its customer when it is missing, rates the usage
// with its own provider, model and attributes, and stores the entry in the
// customer period that contains occurred_at under its idempotency key, with
// the rollup refresh job in the same transaction. The customer period of
// usage that occurred before the report comes from Postgres, never from the
// customer state cache, which holds only current periods.
//
// Before the commit the transaction registers the entry as a pending change
// of its counter with Counters.BeginChange. After the commit a server report
// runs Settle with the cost, which adds it without a count when the
// reservation is no longer reserved, and a fallback report runs
// SettleUnreserved with a count of one. Uncosted usage settles 0. A Redis
// failure, a missing counters_ready marker or a change that is no longer
// pending logs decisions.settle_deferred and Report still succeeds, because
// counters_reconcile or the counter rebuild repairs the counter.
//
// When environment already has an entry with the idempotency key, Report
// changes nothing and returns that entry with duplicate true. Every stored
// or found entry adds one to preburn_reports_total. Invalid fields return a
// 422 validation_failed problem, a decision missing from environment
// httpapi.ErrNotFound and a denied decision ErrDecisionNotReportable.
func (service *ReportService) Report(ctx context.Context, environment httpapi.Environment, request ReportRequest) (ReportResult, error) {
	now := service.clock.Now()
	report, err := parseReportRequest(request, now)
	if err != nil {
		return ReportResult{}, err
	}
	ruleSet, err := service.ruleSets.Get(ctx, environment)
	if err != nil {
		return ReportResult{}, fmt.Errorf("load rule set: %w", err)
	}
	var stored storedReport
	if report.source == ledger.DecisionSourceServer {
		stored, err = service.reportDecision(ctx, environment, report, ruleSet, now)
	} else {
		stored, err = service.reportFallback(ctx, environment, report, ruleSet, now)
	}
	if err != nil {
		return ReportResult{}, err
	}
	if !stored.duplicate {
		service.settle(ctx, stored)
	}
	service.reported.WithLabelValues(string(environment), string(stored.entry.Rating.CostStatus), strconv.FormatBool(stored.duplicate)).Inc()
	result := ReportResult{
		LedgerEntryID: identifiers.Encode(identifiers.PrefixLedgerEntry, stored.entry.ID),
		CostStatus:    stored.entry.Rating.CostStatus,
		Duplicate:     stored.duplicate,
	}
	if cost := stored.entry.Rating.Cost; cost != nil {
		result.Cost = new(money.FormatAmount(*cost))
	}
	return result, nil
}

// ReportBatch runs Report for each of reports, one after another, and
// returns one result per report at its index. A failed report does not stop
// the others: its result holds the status, code, detail and invalid fields
// of the problem it would have answered alone, with locations under
// body.reports[index]. A failure that is not a problem or a CodedError is
// logged as decisions.report_failed and becomes 500 internal_error. A report
// that fails once ctx is done, such as after the client closed the request,
// stops the batch: ReportBatch returns its error without logging it, the
// reports before it keep their entries and the rest are not attempted. More
// than 500 reports return a 422 validation_failed problem at body.reports
// and store nothing.
func (service *ReportService) ReportBatch(ctx context.Context, environment httpapi.Environment, reports []ReportRequest) ([]ReportBatchResult, error) {
	if len(reports) > reportBatchMaximum {
		return nil, httpapi.NewValidationProblem(httpapi.ProblemError{Location: reportsLocation, Message: reportsRule})
	}
	results := make([]ReportBatchResult, 0, len(reports))
	for index, request := range reports {
		result, err := service.Report(ctx, environment, request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("report %d: %w", index, err)
			}
			results = append(results, service.batchFailure(ctx, index, err))
			continue
		}
		results = append(results, ReportBatchResult{Status: http.StatusAccepted, Result: &result})
	}
	return results, nil
}

// RecordDroppedReports adds count, the reports the SDK dropped, to
// preburn_dropped_reports_total and to the dropped report counter of
// environment for the current UTC day, which expires 8 days after the day
// starts. A count of 0 records nothing. A Redis failure logs
// decisions.dropped_reports_record_failed.
func (service *ReportService) RecordDroppedReports(ctx context.Context, environment httpapi.Environment, count int64) {
	if count == 0 {
		return
	}
	service.droppedReports.WithLabelValues(string(environment)).Add(float64(count))
	now := service.clock.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	key := DroppedReportsKey(service.counters.cache, environment, dayStart)
	countersContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), countersTimeout)
	defer cancel()
	pipeline := service.counters.cache.Redis().TxPipeline()
	pipeline.IncrBy(countersContext, key, count)
	pipeline.ExpireAt(countersContext, key, dayStart.Add(droppedReportsRetention))
	if _, err := pipeline.Exec(countersContext); err != nil {
		service.logger.Warn(ctx, logging.DecisionsDroppedReportsRecordFailed,
			slog.String("environment", string(environment)),
			slog.Int64("count", count),
			slog.String("error", err.Error()),
		)
	}
}

func (service *ReportService) reportDecision(ctx context.Context, environment httpapi.Environment, report parsedReport, ruleSet *pricing.RuleSet, now time.Time) (storedReport, error) {
	var stored storedReport
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		store := service.queries.WithTx(transaction)
		decided, err := store.LockDecision(ctx, queries.LockDecisionParams{Environment: queries.Environment(environment), DecisionID: report.decisionID})
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock decision %s: %w", report.decisionID, err)
		}
		if decided.Status == decisionStatusUnreserved {
			return ErrDecisionNotReportable
		}
		entry, err := service.decisionEntry(environment, decided, report, ruleSet, now)
		if err != nil {
			return err
		}
		stored.entry, stored.duplicate, err = ledger.InsertEntry(ctx, transaction, entry)
		if err != nil || stored.duplicate {
			return err
		}
		if err := store.MarkDecisionSettled(ctx, queries.MarkDecisionSettledParams{SettledAt: now, DecisionID: decided.DecisionID}); err != nil {
			return fmt.Errorf("mark decision %s settled: %w", decided.DecisionID, err)
		}
		if err := service.insertRollupRefresh(ctx, transaction, stored.entry); err != nil {
			return err
		}
		stored.registration = service.beginSettlement(ctx, stored.entry)
		return nil
	})
	return stored, err
}

func (service *ReportService) reportFallback(ctx context.Context, environment httpapi.Environment, report parsedReport, ruleSet *pricing.RuleSet, now time.Time) (storedReport, error) {
	customer, err := service.customers.Cache().Ensure(ctx, environment, report.customerExternalID)
	if err != nil {
		return storedReport{}, err
	}
	state, err := service.fallbackState(ctx, environment, customer.ID, report.occurredAt, now)
	if err != nil {
		return storedReport{}, fmt.Errorf("load state of customer %s: %w", customer.ID, err)
	}
	entry, err := rateEntry(ruleSet, ledger.Entry{
		ID:             identifiers.New(),
		Environment:    environment,
		CustomerID:     customer.ID,
		IdempotencyKey: report.idempotencyKey,
		Feature:        report.feature,
		Provider:       report.provider,
		Model:          report.model,
		Attributes:     report.attributes,
		Usage:          report.usage,
		DecisionSource: ledger.DecisionSourceFallback,
		PeriodStart:    state.Period.Start,
		PeriodEnd:      state.Period.End,
		OccurredAt:     report.occurredAt,
		CreatedAt:      now,
	})
	if err != nil {
		return storedReport{}, err
	}
	var stored storedReport
	err = database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		var err error
		stored.entry, stored.duplicate, err = ledger.InsertEntry(ctx, transaction, entry)
		if err != nil || stored.duplicate {
			return err
		}
		if err := service.insertRollupRefresh(ctx, transaction, stored.entry); err != nil {
			return err
		}
		stored.registration = service.beginSettlement(ctx, stored.entry)
		return nil
	})
	return stored, err
}

func (service *ReportService) fallbackState(ctx context.Context, environment httpapi.Environment, customerID uuid.UUID, occurredAt time.Time, now time.Time) (signals.CustomerState, error) {
	if occurredAt.Equal(now) {
		return service.customerStates.Load(ctx, environment, customerID, now)
	}
	return service.customerStates.Loader().Load(ctx, environment, customerID, occurredAt)
}

func (service *ReportService) decisionEntry(environment httpapi.Environment, decided queries.LockDecisionRow, report parsedReport, ruleSet *pricing.RuleSet, now time.Time) (ledger.Entry, error) {
	attributes := pricing.Attributes{}
	if err := json.Unmarshal(decided.Attributes, &attributes); err != nil {
		return ledger.Entry{}, fmt.Errorf("decode attributes of decision %s: %w", decided.DecisionID, err)
	}
	var overrides pricing.Attributes
	if err := json.Unmarshal(decided.Overrides, &overrides); err != nil {
		return ledger.Entry{}, fmt.Errorf("decode overrides of decision %s: %w", decided.DecisionID, err)
	}
	parameters := service.modelParameters(decided.Provider, decided.Model)
	for key, value := range overrides {
		if parameters[key].Effect == catalogfiles.EffectPrices {
			attributes[key] = value
		}
	}
	maps.Copy(attributes, report.attributes)
	return rateEntry(ruleSet, ledger.Entry{
		ID:             identifiers.New(),
		Environment:    environment,
		CustomerID:     decided.CustomerID,
		CustomerUserID: decided.CustomerUserID,
		DecisionID:     &decided.DecisionID,
		IdempotencyKey: decisionIdempotencyKeyPrefix + decided.DecisionID.String(),
		Feature:        decided.Feature,
		Provider:       decided.Provider,
		Model:          decided.Model,
		Attributes:     attributes,
		Usage:          report.usage,
		DecisionSource: ledger.DecisionSourceServer,
		PeriodStart:    decided.PeriodStart,
		PeriodEnd:      decided.PeriodEnd,
		OccurredAt:     report.occurredAt,
		CreatedAt:      now,
	})
}

func (service *ReportService) insertRollupRefresh(ctx context.Context, transaction pgx.Tx, entry ledger.Entry) error {
	refresh := ledger.NewRollupRefreshArgs(entry.Environment, entry.CustomerID, entry.PeriodStart)
	if _, err := service.jobs.InsertTx(ctx, transaction, refresh, nil); err != nil {
		return fmt.Errorf("insert rollup refresh: %w", err)
	}
	return nil
}

func (service *ReportService) beginSettlement(ctx context.Context, entry ledger.Entry) error {
	countersContext, cancel := context.WithTimeout(ctx, countersTimeout)
	defer cancel()
	return service.counters.BeginChange(countersContext, settlementOf(entry), entry.CreatedAt)
}

func (service *ReportService) settle(ctx context.Context, stored storedReport) {
	err := stored.registration
	if err == nil {
		err = service.applySettlement(ctx, stored.entry)
	}
	if err != nil {
		service.logger.Warn(ctx, logging.DecisionsSettleDeferred,
			slog.String("environment", string(stored.entry.Environment)),
			slog.String("ledger_entry_id", identifiers.Encode(identifiers.PrefixLedgerEntry, stored.entry.ID)),
			slog.String("error", err.Error()),
		)
	}
}

func (service *ReportService) applySettlement(ctx context.Context, entry ledger.Entry) error {
	countersContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), countersTimeout)
	defer cancel()
	if entry.DecisionID != nil {
		return service.counters.Settle(countersContext, *entry.DecisionID, settlementOf(entry))
	}
	return service.counters.SettleUnreserved(countersContext, settlementOf(entry))
}

func (service *ReportService) batchFailure(ctx context.Context, index int, err error) ReportBatchResult {
	if problem, isProblem := errors.AsType[*httpapi.Problem](err); isProblem {
		var locatedErrors []httpapi.ProblemError
		for _, problemError := range problem.Errors {
			locatedErrors = append(locatedErrors, httpapi.ProblemError{
				Location: fmt.Sprintf(batchReportLocation, index) + strings.TrimPrefix(problemError.Location, bodyLocation),
				Message:  problemError.Message,
			})
		}
		return ReportBatchResult{Status: problem.Status, Error: &ReportFailure{Code: problem.Code, Detail: problem.Detail, Errors: locatedErrors}}
	}
	if coded, isCoded := errors.AsType[httpapi.CodedError](err); isCoded {
		return ReportBatchResult{Status: coded.ProblemStatus(), Error: &ReportFailure{Code: coded.ProblemCode(), Detail: coded.Error()}}
	}
	service.logger.Error(ctx, logging.DecisionsReportFailed, slog.Int("index", index), slog.String("error", err.Error()))
	return ReportBatchResult{Status: http.StatusInternalServerError, Error: &ReportFailure{Code: internalErrorCode, Detail: internalErrorDetail}}
}

func (service *ReportService) modelParameters(provider string, model string) map[string]catalogfiles.Parameter {
	key := catalogfiles.ModelKey{Provider: provider, Model: model}
	if canonical, isAlias := service.modelAliases[key]; isAlias {
		key.Model = canonical
	}
	return service.policies.ParameterMappings()[key]
}

func settlementOf(entry ledger.Entry) Settlement {
	settlement := Settlement{
		Environment: entry.Environment,
		CustomerID:  entry.CustomerID,
		PeriodStart: entry.PeriodStart,
		PeriodEnd:   entry.PeriodEnd,
		ChangeID:    entry.ID,
		Feature:     entry.Feature,
	}
	if entry.Rating.Cost != nil {
		settlement.Amount = *entry.Rating.Cost
	}
	if entry.DecisionID == nil {
		settlement.CountIncrement = fallbackCountIncrement
	}
	return settlement
}

func parseReportRequest(request ReportRequest, now time.Time) (parsedReport, error) {
	var problems checkProblems
	report := parsedReport{
		source:     request.DecisionSource,
		attributes: pricing.Attributes{},
		usage:      usage{},
		occurredAt: now,
	}
	switch request.DecisionSource {
	case ledger.DecisionSourceServer:
		decisionID, err := identifiers.Decode(identifiers.PrefixDecision, request.DecisionID)
		problems.require(err == nil, decisionIDLocation, decisionIDRule)
		report.decisionID = decisionID
	case ledger.DecisionSourceFallback:
		problems.require(request.DecisionID == "", decisionIDLocation, fallbackDecisionIDRule)
		idempotencyKey, err := uuid.Parse(request.IdempotencyKey)
		problems.require(err == nil, idempotencyKeyLocation, idempotencyKeyRule)
		problems.require(request.CustomerID != "", reportCustomerIDLocation, reportCustomerIDRule)
		problems.require(plans.FeaturePattern.MatchString(request.Feature), featureLocation, featureRule)
		problems.require(validName(request.Provider), providerLocation, nameRule)
		problems.require(validName(request.Model), modelLocation, nameRule)
		report.idempotencyKey = idempotencyKey.String()
		report.customerExternalID = request.CustomerID
		report.feature = request.Feature
		report.provider = request.Provider
		report.model = request.Model
	default:
		problems.add(decisionSourceLocation, decisionSourceRule)
	}
	problems.require(len(request.Attributes) <= attributesMaximum, attributesLocation, attributesRule)
	for _, key := range slices.Sorted(maps.Keys(request.Attributes)) {
		err := pricing.ValidateAttributes(pricing.Attributes{key: request.Attributes[key]})
		if invalid, isInvalid := errors.AsType[*pricing.InvalidAttributeError](err); isInvalid {
			problems.add(attributesLocation+"."+key, "expected "+invalid.Requirement)
		}
	}
	problems.require(request.Usage != nil, usageLocation, usageRule)
	maps.Copy(report.usage, problems.usage(request.Usage, usageLocation))
	maps.Copy(report.attributes, request.Attributes)
	if request.OccurredAt != nil {
		report.occurredAt = *request.OccurredAt
	}
	if len(problems) > 0 {
		return parsedReport{}, httpapi.NewValidationProblem(problems...)
	}
	return report, nil
}

func rateEntry(ruleSet *pricing.RuleSet, entry ledger.Entry) (ledger.Entry, error) {
	rating, err := pricing.Rate(pricing.RatingRequest{
		Provider:   entry.Provider,
		Model:      entry.Model,
		Attributes: entry.Attributes,
		Usage:      entry.Usage,
		OccurredAt: entry.OccurredAt,
	}, ruleSet)
	if err != nil {
		return ledger.Entry{}, fmt.Errorf("rate usage of %s %s: %w", entry.Provider, entry.Model, err)
	}
	entry.Rating = rating
	return entry, nil
}
