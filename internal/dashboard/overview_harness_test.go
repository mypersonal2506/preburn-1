package dashboard_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
)

const (
	nanosPerDollar      = 1_000_000_000
	authorizationHeader = "Authorization"
	overviewPath        = "/api/v1/dashboard/overview"
	customersPath       = "/api/v1/dashboard/customers"
	textToVideo         = "text_to_video"
	textToImage         = "text_to_image"
	falProvider         = "fal_ai"
	veoModel            = "fal-ai/veo3"
	klingModel          = "fal-ai/kling-video"
	fluxModel           = "fal-ai/flux"
)

type harness struct {
	pool      *pgxpool.Pool
	cache     *cache.Client
	clock     *clock.Manual
	counters  *decisions.Counters
	overview  *dashboard.OverviewService
	customers *dashboard.CustomerService
	apiKeys   *apikeys.Service
	handler   http.Handler
}

type testAuthenticator struct {
	apiKeys *apikeys.Authenticator
}

type ledgerFixture struct {
	environment  httpapi.Environment
	customerID   uuid.UUID
	feature      string
	model        string
	cost         *money.Amount
	periodStart  time.Time
	periodEnd    time.Time
	correctionOf *uuid.UUID
	occurredAt   time.Time
}

type decisionFixture struct {
	environment     httpapi.Environment
	customerID      uuid.UUID
	outcome         string
	reason          string
	matchedPolicyID *uuid.UUID
	requestedCost   *money.Amount
	estimatedCost   *money.Amount
	periodStart     time.Time
	periodEnd       time.Time
	createdAt       time.Time
}

var (
	testNow        = time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	juneStart      = time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	julyStart      = time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	augustStart    = time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	septemberStart = time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	octoberStart   = time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
)

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(testNow)
	logger := logging.New(t.Output(), slog.LevelDebug)
	states := customerstate.NewLoader(pool)
	counters := decisions.NewCounters(cacheClient)
	overview := dashboard.NewOverviewService(pool, cacheClient, states, counters, manualClock)
	customerService := dashboard.NewCustomerService(pool, states, counters, manualClock)
	apiKeyService := apikeys.NewService(pool, cacheClient, manualClock, logger)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, testAuthenticator{apiKeys: apiKeyService.Authenticator()})
	dashboard.RegisterOverviewRoutes(api, overview)
	dashboard.RegisterCustomerRoutes(api, customerService)
	return &harness{
		pool:      pool,
		cache:     cacheClient,
		clock:     manualClock,
		counters:  counters,
		overview:  overview,
		customers: customerService,
		apiKeys:   apiKeyService,
		handler:   mux,
	}
}

func (authenticator testAuthenticator) Authenticate(ctx context.Context, request *http.Request, group httpapi.RouteGroup) (httpapi.Principal, error) {
	if len(request.Header.Values(authorizationHeader)) > 0 {
		return authenticator.apiKeys.Authenticate(ctx, request, group)
	}
	environment, err := httpapi.EnvironmentFromHeader(request.Header)
	if err != nil {
		return nil, err
	}
	return httpapi.MemberPrincipal{MemberID: identifiers.New(), Environment: environment}, nil
}

func (harness *harness) insertPlan(t *testing.T, environment httpapi.Environment, name string, targetMargin money.BasisPoints, allowance *money.Amount) uuid.UUID {
	t.Helper()
	planID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO plans (plan_id, environment, name, target_margin_basis_points, allowance_nanos, status) VALUES ($1, $2, $3, $4, $5, 'active')",
		planID, string(environment), name, int64(targetMargin), allowance)
	if err != nil {
		t.Fatalf("insert plan %s: %v", name, err)
	}
	return planID
}

func (harness *harness) insertCustomer(t *testing.T, environment httpapi.Environment, externalID string, displayName *string, planID *uuid.UUID) uuid.UUID {
	t.Helper()
	return harness.insertCustomerWithStatus(t, environment, externalID, displayName, planID, "active")
}

func (harness *harness) insertCustomerWithStatus(t *testing.T, environment httpapi.Environment, externalID string, displayName *string, planID *uuid.UUID, status string) uuid.UUID {
	t.Helper()
	customerID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO customers (customer_id, environment, external_id, display_name, plan_id, status) VALUES ($1, $2, $3, $4, $5, $6)",
		customerID, string(environment), externalID, displayName, planID, status)
	if err != nil {
		t.Fatalf("insert customer %s: %v", externalID, err)
	}
	return customerID
}

func (harness *harness) insertRevenue(t *testing.T, environment httpapi.Environment, customerID uuid.UUID, kind string, amount money.Amount, periodStart, periodEnd, occurredAt time.Time) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO revenue_entries (revenue_entry_id, environment, customer_id, period_start, period_end, kind, amount_nanos, source, source_reference, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'api', $8, $9)`,
		identifiers.New(), string(environment), customerID, periodStart, periodEnd, kind, int64(amount), identifiers.New().String(), occurredAt)
	if err != nil {
		t.Fatalf("insert %s revenue: %v", kind, err)
	}
}

func (harness *harness) insertLedgerEntry(t *testing.T, entry ledgerFixture) uuid.UUID {
	t.Helper()
	costStatus := "costed"
	if entry.cost == nil {
		costStatus = "uncosted"
	}
	ledgerEntryID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO ledger_entries (ledger_entry_id, environment, customer_id, idempotency_key, feature, provider, model, attributes,
			usage, cost_nanos, cost_breakdown, cost_status, decision_source, period_start, period_end, correction_of, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, '{}', '{"output_seconds":8000000}', $8, '{}', $9, 'server', $10, $11, $12, $13)`,
		ledgerEntryID, string(entry.environment), entry.customerID, ledgerEntryID.String(), entry.feature, falProvider, entry.model,
		entry.cost, costStatus, entry.periodStart, entry.periodEnd, entry.correctionOf, entry.occurredAt)
	if err != nil {
		t.Fatalf("insert ledger entry: %v", err)
	}
	return ledgerEntryID
}

func (harness *harness) insertDecision(t *testing.T, decision decisionFixture) uuid.UUID {
	t.Helper()
	decisionID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO decisions (decision_id, environment, customer_id, feature, requested_provider, requested_model, provider, model,
			attributes, overrides, outcome, reason, matched_policy_id, signals, requested_estimated_cost_nanos, estimated_cost_nanos,
			reserved_nanos, estimate_basis, status, period_start, period_end, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $5, $7, '{}', '{}', $8, $9, $10, '{}', $11, $12, 0, 'none', 'settled', $13, $14, $15, $15)`,
		decisionID, string(decision.environment), decision.customerID, textToVideo, falProvider, veoModel, klingModel,
		decision.outcome, decision.reason, decision.matchedPolicyID, decision.requestedCost, decision.estimatedCost,
		decision.periodStart, decision.periodEnd, decision.createdAt)
	if err != nil {
		t.Fatalf("insert %s decision: %v", decision.outcome, err)
	}
	return decisionID
}

func (harness *harness) insertPolicy(t *testing.T, environment httpapi.Environment, name string, version int, createdAt, updatedAt time.Time) uuid.UUID {
	t.Helper()
	policyID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO policies (policy_id, environment, name, level, condition_group, action, enforcement, on_unreachable, on_uncosted,
			status, version, created_at, updated_at)
		VALUES ($1, $2, $3, 'everyone', '{"all":[]}', '{"outcome":"deny"}', 'soft', 'allow', 'allow', 'active', $4, $5, $6)`,
		policyID, string(environment), name, version, createdAt, updatedAt)
	if err != nil {
		t.Fatalf("insert policy %s: %v", name, err)
	}
	return policyID
}

func (harness *harness) insertRollup(t *testing.T, environment httpapi.Environment, customerID uuid.UUID, periodStart, periodEnd time.Time, revenueNet money.Amount) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO period_rollups (environment, customer_id, period_start, period_end, revenue_net_nanos, cost_nanos, uncosted_count, decision_counts)
		VALUES ($1, $2, $3, $4, $5, 0, 0, '{"allow":0,"route":0,"cap":0,"deny":0}')`,
		string(environment), customerID, periodStart, periodEnd, int64(revenueNet))
	if err != nil {
		t.Fatalf("insert rollup: %v", err)
	}
}

func (harness *harness) refreshRollups(t *testing.T, environment httpapi.Environment, customerID uuid.UUID, anchor time.Time) {
	t.Helper()
	err := database.InTransaction(t.Context(), harness.pool, func(ctx context.Context, transaction pgx.Tx) error {
		return ledger.RefreshRollups(ctx, transaction, environment, customerID, anchor)
	})
	if err != nil {
		t.Fatalf("refresh rollups at %s: %v", anchor, err)
	}
}

func (harness *harness) setSettled(t *testing.T, environment httpapi.Environment, customerID uuid.UUID, settled money.Amount) {
	t.Helper()
	key := harness.counters.CounterKey(environment, customerID, septemberStart)
	fields := map[string]int64{"settled": int64(settled), "settled:" + textToVideo: int64(settled)}
	if err := harness.counters.Overwrite(t.Context(), key, fields, octoberStart); err != nil {
		t.Fatalf("overwrite counter: %v", err)
	}
}

func (harness *harness) setDroppedReports(t *testing.T, environment httpapi.Environment, day time.Time, count int64) {
	t.Helper()
	if err := harness.cache.Redis().Set(t.Context(), decisions.DroppedReportsKey(harness.cache, environment, day), count, 0).Err(); err != nil {
		t.Fatalf("set dropped reports of %s: %v", day, err)
	}
}

func (harness *harness) createKey(t *testing.T, environment httpapi.Environment, scope apikeys.Scope) string {
	t.Helper()
	_, secret, err := harness.apiKeys.Create(t.Context(), environment, "Reporting", scope, nil)
	if err != nil {
		t.Fatalf("create %s key: %v", scope, err)
	}
	return secret
}

func (harness *harness) serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *harness) memberGet(t *testing.T, environment httpapi.Environment, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	request.Header.Set(httpapi.EnvironmentHeader, string(environment))
	return harness.serve(request)
}

func (harness *harness) bearerGet(t *testing.T, secret string, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	request.Header.Set(authorizationHeader, "Bearer "+secret)
	return harness.serve(request)
}

func dollars(value float64) money.Amount {
	return money.Amount(value * nanosPerDollar)
}

func amount(value float64) *money.Amount {
	converted := dollars(value)
	return &converted
}

func day(month time.Month, dayOfMonth int) time.Time {
	return time.Date(2026, month, dayOfMonth, 0, 0, 0, 0, time.UTC)
}

func pointer[Value any](value Value) *Value {
	return &value
}

func encodedCustomer(customerID uuid.UUID) string {
	return identifiers.Encode(identifiers.PrefixCustomer, customerID)
}

func encodedPlan(planID uuid.UUID) *string {
	return pointer(identifiers.Encode(identifiers.PrefixPlan, planID))
}

func encodedPolicy(policyID uuid.UUID) string {
	return identifiers.Encode(identifiers.PrefixPolicy, policyID)
}

func assertStatus(t *testing.T, recorder *httptest.ResponseRecorder, status int) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d, body %s", recorder.Code, status, recorder.Body.String())
	}
}

func decodeInto[Body any](t *testing.T, recorder *httptest.ResponseRecorder) Body {
	t.Helper()
	assertStatus(t, recorder, http.StatusOK)
	var body Body
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	return body
}

func assertProblem(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string, locations ...string) {
	t.Helper()
	assertStatus(t, recorder, status)
	var problem httpapi.Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem %q: %v", recorder.Body.String(), err)
	}
	var got []string
	for _, fieldError := range problem.Errors {
		got = append(got, fieldError.Location)
	}
	if problem.Code != code || !cmp.Equal(locations, got) {
		t.Errorf("problem code=%s locations=%v, want %s at %v", problem.Code, got, code, locations)
	}
}
