package decisions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/catalog"
	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/jobs/jobstest"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
	"github.com/preburn/preburn/internal/signals"
)

const (
	checkFeature          = "text_to_video"
	otherCheckFeature     = "image_to_video"
	checkCustomer         = "acme"
	falProvider           = "fal_ai"
	veoModel              = "fal-ai/veo3.1/fast"
	klingModel            = "fal-ai/kling-video/v2.5-turbo/pro/text-to-video"
	klingImageAlias       = "fal-ai/kling-video/v2.5-turbo/pro/image-to-video"
	unpricedProvider      = "acme"
	unpricedModel         = "acme-video-1"
	outputSeconds         = "output_seconds"
	checkPath             = "/api/v1/check"
	decisionsMetricName   = "preburn_decisions_total"
	checkDurationMetric   = "preburn_check_duration_seconds"
	checkAuthorization    = "Authorization"
	userInsertQueryPrefix = "-- name: InsertCustomerUserIfMissing "
	defaultCheckHoldTime  = 10 * time.Minute
	nanosPerDollar        = 1_000_000_000
	decisionColumnsSelect = `SELECT customer_id, customer_user_id, feature, requested_provider, requested_model, provider, model,
		attributes, overrides, outcome, reason, matched_policy_id, matched_policy_version, signals,
		requested_estimated_cost_nanos, estimated_cost_nanos, reserved_nanos, estimate_basis, status,
		period_start, period_end, expires_at, created_at
		FROM decisions WHERE decision_id = $1`
)

type checkHarness struct {
	pool        *pgxpool.Pool
	cache       *cache.Client
	clock       *clock.Manual
	customers   *customers.Service
	userInserts *atomic.Int64
	policies    *policies.Service
	states      *customerstate.Cache
	counters    *decisions.Counters
	bootstrap   *decisions.CounterBootstrap
	registry    *prometheus.Registry
	service     *decisions.CheckService
	apiKeys     *apikeys.Service
	handler     http.Handler
	checkLogs   *bytes.Buffer
}

type decisionRow struct {
	CustomerID                  uuid.UUID
	CustomerUserID              *uuid.UUID
	Feature                     string
	RequestedProvider           string
	RequestedModel              string
	Provider                    string
	Model                       string
	Attributes                  map[string]any
	Overrides                   map[string]any
	Outcome                     string
	Reason                      string
	MatchedPolicyID             *uuid.UUID
	MatchedPolicyVersion        *int32
	Signals                     map[string]any
	RequestedEstimatedCostNanos *int64
	EstimatedCostNanos          *int64
	ReservedNanos               int64
	EstimateBasis               string
	Status                      string
	PeriodStart                 time.Time
	PeriodEnd                   time.Time
	ExpiresAt                   time.Time
	CreatedAt                   time.Time
}

type unreachableRedisHook struct{}

type cancelingRedisHook struct {
	cancel context.CancelFunc
}

type userInsertCounter struct {
	inserts atomic.Int64
}

type checkAuthenticator struct {
	apiKeys *apikeys.Authenticator
}

var (
	checkStart       = time.Now().UTC().Truncate(time.Hour)
	checkPeriod      = signals.ResolvePeriod(checkStart, nil, nil)
	checkImportStart = checkStart.Add(-time.Hour)
)

func newCheckHarness(t *testing.T) *checkHarness {
	t.Helper()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(checkStart)
	checkLogs := &bytes.Buffer{}
	logger := logging.New(io.MultiWriter(t.Output(), checkLogs), slog.LevelDebug)
	files, err := catalogfiles.Load(catalog.Files)
	if err != nil {
		t.Fatalf("load catalog files: %v", err)
	}
	if _, err := pricing.ImportCurated(t.Context(), pool, files, clock.NewManual(checkImportStart), logger); err != nil {
		t.Fatalf("import curated catalog: %v", err)
	}
	userInserts := &userInsertCounter{}
	customerService := customers.NewService(tracedPool(t, pool, userInserts), cacheClient, manualClock)
	policyService := policies.NewService(pool, cacheClient, files, manualClock)
	states := customerstate.NewCache(customerstate.NewLoader(pool), manualClock)
	counters := decisions.NewCounters(cacheClient)
	if err := counters.SetReady(t.Context(), checkStart); err != nil {
		t.Fatalf("set counters ready: %v", err)
	}
	registry := prometheus.NewRegistry()
	jobs := jobstest.NewInsertClient(t, pool)
	bootstrap, err := decisions.NewCounterBootstrap(pool, counters, jobs, manualClock, logger, registry)
	if err != nil {
		t.Fatalf("new counter bootstrap: %v", err)
	}
	service, err := decisions.NewCheckService(decisions.CheckDependencies{
		Pool:           pool,
		Jobs:           jobs,
		Customers:      customerService,
		CustomerStates: states,
		RuleSets:       pricing.NewService(pool, cacheClient, jobs, files, manualClock).RuleSets(),
		Policies:       policyService,
		ModelAliases:   files.Aliases,
		Counters:       counters,
		Bootstrap:      bootstrap,
		Stream:         decisions.NewStreamWriter(cacheClient, logger),
		Clock:          manualClock,
		Logger:         logger,
		Registry:       registry,
	})
	if err != nil {
		t.Fatalf("new check service: %v", err)
	}
	apiKeyService := apikeys.NewService(pool, cacheClient, manualClock, logger)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, checkAuthenticator{apiKeys: apiKeyService.Authenticator()})
	decisions.RegisterCheckRoutes(api, service)
	return &checkHarness{
		pool:        pool,
		cache:       cacheClient,
		clock:       manualClock,
		customers:   customerService,
		userInserts: &userInserts.inserts,
		policies:    policyService,
		states:      states,
		counters:    counters,
		bootstrap:   bootstrap,
		registry:    registry,
		service:     service,
		apiKeys:     apiKeyService,
		handler:     mux,
		checkLogs:   checkLogs,
	}
}

func tracedPool(t *testing.T, pool *pgxpool.Pool, tracer pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	configuration := pool.Config().Copy()
	configuration.ConnConfig.Tracer = tracer
	traced, err := pgxpool.NewWithConfig(t.Context(), configuration)
	if err != nil {
		t.Fatalf("open traced pool: %v", err)
	}
	t.Cleanup(traced.Close)
	return traced
}

func (counter *userInsertCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, userInsertQueryPrefix) {
		counter.inserts.Add(1)
	}
	return ctx
}

func (*userInsertCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (authenticator checkAuthenticator) Authenticate(ctx context.Context, request *http.Request, group httpapi.RouteGroup) (httpapi.Principal, error) {
	return authenticator.apiKeys.Authenticate(ctx, request, group)
}

func (unreachableRedisHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return next(ctx, network, address)
	}
}

func (unreachableRedisHook) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, command redis.Cmder) error {
		<-ctx.Done()
		command.SetErr(ctx.Err())
		return ctx.Err()
	}
}

func (unreachableRedisHook) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) error {
		<-ctx.Done()
		for _, command := range commands {
			command.SetErr(ctx.Err())
		}
		return ctx.Err()
	}
}

func (hook cancelingRedisHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (hook cancelingRedisHook) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, command redis.Cmder) error {
		hook.cancel()
		<-ctx.Done()
		command.SetErr(ctx.Err())
		return ctx.Err()
	}
}

func (hook cancelingRedisHook) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) error {
		hook.cancel()
		<-ctx.Done()
		for _, command := range commands {
			command.SetErr(ctx.Err())
		}
		return ctx.Err()
	}
}

func (harness *checkHarness) check(t *testing.T, request decisions.CheckRequest) decisions.CheckResponse {
	t.Helper()
	response, err := harness.service.Check(t.Context(), httpapi.EnvironmentTest, request)
	if err != nil {
		t.Fatalf("check %s %s: %v", request.Feature, request.Model, err)
	}
	return response
}

func (harness *checkHarness) createPolicy(t *testing.T, fields map[string]string) policies.Policy {
	t.Helper()
	document := map[string]json.RawMessage{
		"name":           json.RawMessage(`"Check policy"`),
		"level":          json.RawMessage(`"everyone"`),
		"when":           json.RawMessage(`{"all": []}`),
		"action":         json.RawMessage(`{"outcome": "allow"}`),
		"enforcement":    json.RawMessage(`"soft"`),
		"on_unreachable": json.RawMessage(`"allow"`),
		"on_uncosted":    json.RawMessage(`"allow"`),
	}
	for field, value := range fields {
		document[field] = json.RawMessage(value)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode policy document: %v", err)
	}
	policy, err := harness.policies.Create(t.Context(), httpapi.EnvironmentTest, encoded)
	if err != nil {
		t.Fatalf("create policy %s: %v", encoded, err)
	}
	return policy
}

func (harness *checkHarness) insertDefaultPlan(t *testing.T, allowance *money.Amount, holdTimes string) uuid.UUID {
	t.Helper()
	planID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO plans (plan_id, environment, name, target_margin_basis_points, allowance_nanos, hold_times, status) VALUES ($1, 'test', 'Studio', 4000, $2, $3, 'active')",
		planID, allowance, holdTimes)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if _, err := harness.pool.Exec(t.Context(), "UPDATE environment_settings SET default_plan_id = $1 WHERE environment = 'test'", planID); err != nil {
		t.Fatalf("set default plan: %v", err)
	}
	return planID
}

func (harness *checkHarness) ensureCustomer(t *testing.T) customers.Customer {
	t.Helper()
	customer, err := harness.customers.Ensure(t.Context(), httpapi.EnvironmentTest, checkCustomer)
	if err != nil {
		t.Fatalf("ensure customer %s: %v", checkCustomer, err)
	}
	return customer
}

func (harness *checkHarness) setUsageEstimate(t *testing.T, provider, model string, p95 money.Quantity, sampleCount int) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(), `INSERT INTO usage_estimates (environment, feature, provider, model, meter, p95_quantity_micros, sample_count, refreshed_at)
		VALUES ('test', $1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (environment, feature, provider, model, meter) DO UPDATE SET p95_quantity_micros = EXCLUDED.p95_quantity_micros, sample_count = EXCLUDED.sample_count`,
		checkFeature, provider, model, outputSeconds, int64(p95), sampleCount, checkStart)
	if err != nil {
		t.Fatalf("set usage estimate: %v", err)
	}
}

func (harness *checkHarness) decision(t *testing.T, response decisions.CheckResponse) decisionRow {
	t.Helper()
	decisionID := decodeIdentifier(t, identifiers.PrefixDecision, response.DecisionID)
	var row decisionRow
	var attributes, overrides, signalValues []byte
	err := harness.pool.QueryRow(t.Context(), decisionColumnsSelect, decisionID).Scan(
		&row.CustomerID, &row.CustomerUserID, &row.Feature, &row.RequestedProvider, &row.RequestedModel, &row.Provider, &row.Model,
		&attributes, &overrides, &row.Outcome, &row.Reason, &row.MatchedPolicyID, &row.MatchedPolicyVersion, &signalValues,
		&row.RequestedEstimatedCostNanos, &row.EstimatedCostNanos, &row.ReservedNanos, &row.EstimateBasis, &row.Status,
		&row.PeriodStart, &row.PeriodEnd, &row.ExpiresAt, &row.CreatedAt,
	)
	if err != nil {
		t.Fatalf("select decision %s: %v", response.DecisionID, err)
	}
	for _, column := range []struct {
		encoded []byte
		target  *map[string]any
	}{{attributes, &row.Attributes}, {overrides, &row.Overrides}, {signalValues, &row.Signals}} {
		if err := json.Unmarshal(column.encoded, column.target); err != nil {
			t.Fatalf("decode decision column %s: %v", column.encoded, err)
		}
	}
	row.PeriodStart = row.PeriodStart.UTC()
	row.PeriodEnd = row.PeriodEnd.UTC()
	row.ExpiresAt = row.ExpiresAt.UTC()
	row.CreatedAt = row.CreatedAt.UTC()
	return row
}

func (harness *checkHarness) decisionCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM decisions").Scan(&count); err != nil {
		t.Fatalf("count decisions: %v", err)
	}
	return count
}

func (harness *checkHarness) counter(t *testing.T, customerID uuid.UUID) map[string]string {
	t.Helper()
	key := harness.counters.CounterKey(httpapi.EnvironmentTest, customerID, checkPeriod.Start)
	fields, err := harness.cache.Redis().HGetAll(t.Context(), key).Result()
	if err != nil {
		t.Fatalf("read counter %s: %v", key, err)
	}
	return fields
}

func (harness *checkHarness) streamEntries(t *testing.T) []redis.XMessage {
	t.Helper()
	entries, err := harness.cache.Redis().XRange(t.Context(), decisions.StreamKey(harness.cache, httpapi.EnvironmentTest), "-", "+").Result()
	if err != nil {
		t.Fatalf("read decision stream: %v", err)
	}
	return entries
}

func (harness *checkHarness) post(t *testing.T, secret string, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, checkPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(checkAuthorization, "Bearer "+secret)
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *checkHarness) createRuntimeKey(t *testing.T) string {
	t.Helper()
	_, secret, err := harness.apiKeys.Create(t.Context(), httpapi.EnvironmentTest, "Check key", apikeys.ScopeRuntime, nil)
	if err != nil {
		t.Fatalf("create runtime key: %v", err)
	}
	return secret
}

func videoCheck(model string, estimateSeconds string) decisions.CheckRequest {
	return decisions.CheckRequest{
		CustomerID:    checkCustomer,
		Feature:       checkFeature,
		Provider:      falProvider,
		Model:         model,
		UsageEstimate: map[string]string{outputSeconds: estimateSeconds},
	}
}

func decodeIdentifier(t *testing.T, prefix identifiers.Prefix, encoded string) uuid.UUID {
	t.Helper()
	decoded, err := identifiers.Decode(prefix, encoded)
	if err != nil {
		t.Fatalf("decode %s id %q: %v", prefix, encoded, err)
	}
	return decoded
}

func dollarAmount(cents int64) money.Amount {
	return money.Amount(cents * nanosPerDollar / 100)
}

func formattedDollars(cents int64) *string {
	formatted := money.FormatAmount(dollarAmount(cents))
	return &formatted
}

func labeledCounter(t *testing.T, registry prometheus.Gatherer, name string, labels map[string]string) float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			got := map[string]string{}
			for _, label := range metric.GetLabel() {
				got[label.GetName()] = label.GetValue()
			}
			if cmp.Equal(labels, got) {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func assertCheckProblem(t *testing.T, err error, code string) {
	t.Helper()
	coded, isCoded := errors.AsType[httpapi.CodedError](err)
	if !isCoded || coded.ProblemStatus() != http.StatusServiceUnavailable || coded.ProblemCode() != code {
		t.Fatalf("error = %v, want 503 %s", err, code)
	}
}

func assertCanceledCheck(t *testing.T, harness *checkHarness, err error) {
	t.Helper()
	if _, isCoded := errors.AsType[httpapi.CodedError](err); isCoded || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the uncoded context.Canceled cause", err)
	}
	logs := harness.checkLogs.String()
	for _, unwanted := range []string{`"level":"ERROR"`, "decisions.database_unavailable", "decisions.counters_unavailable"} {
		if strings.Contains(logs, unwanted) {
			t.Errorf("logs = %s, want no %s", logs, unwanted)
		}
	}
}

func manyAttributes(count int) string {
	attributes := make([]string, 0, count)
	for index := range count {
		attributes = append(attributes, fmt.Sprintf(`"key_%d": "value"`, index))
	}
	return strings.Join(attributes, ", ")
}
