package revenue_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/revenue"
)

const (
	nanosPerDollar      = 1_000_000_000
	testExternalID      = "acme"
	testSourceReference = "in_1Q2w3E4r5T6y-line-1"
	revenuePath         = "/api/v1/revenue"
	authorizationHeader = "Authorization"
	invalidationChannel = "invalidate"
	waitTimeout         = 10 * time.Second
)

type harness struct {
	pool      *pgxpool.Pool
	cache     *cache.Client
	clock     *clock.Manual
	customers *customers.Service
	states    *customerstate.Cache
	service   *revenue.Service
	apiKeys   *apikeys.Service
	handler   http.Handler
}

type testAuthenticator struct {
	apiKeys *apikeys.Authenticator
}

type rollup struct {
	PeriodStart time.Time
	PeriodEnd   time.Time
	RevenueNet  money.Amount
}

var (
	testStart        = time.Date(2026, time.September, 26, 10, 0, 0, 0, time.UTC)
	augustStart      = time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	augustSecond     = time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
	septemberStart   = time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	septemberSecond  = time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC)
	octoberStart     = time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	planTargetMargin = money.BasisPoints(4000)
)

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(testStart)
	logger := logging.New(t.Output(), slog.LevelDebug)
	customerService := customers.NewService(pool, cacheClient, manualClock)
	states := customerstate.NewCache(customerstate.NewLoader(pool), manualClock)
	service := revenue.NewService(pool, customerService.Cache(), states, cacheClient, manualClock)
	apiKeyService := apikeys.NewService(pool, cacheClient, manualClock, logger)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, testAuthenticator{apiKeys: apiKeyService.Authenticator()})
	revenue.RegisterRoutes(api, service)
	return &harness{
		pool:      pool,
		cache:     cacheClient,
		clock:     manualClock,
		customers: customerService,
		states:    states,
		service:   service,
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

func (harness *harness) record(t *testing.T, environment httpapi.Environment, source revenue.Source, input revenue.RecordInput) (revenue.Entry, bool) {
	t.Helper()
	entry, duplicate, err := harness.service.Record(t.Context(), environment, source, input)
	if err != nil {
		t.Fatalf("record %s revenue %s: %v", input.Kind, input.SourceReference, err)
	}
	return entry, duplicate
}

func (harness *harness) testCustomerID(t *testing.T) uuid.UUID {
	t.Helper()
	var customerID uuid.UUID
	err := harness.pool.QueryRow(t.Context(), "SELECT customer_id FROM customers WHERE environment = 'test' AND external_id = $1", testExternalID).Scan(&customerID)
	if err != nil {
		t.Fatalf("select test customer %s: %v", testExternalID, err)
	}
	return customerID
}

func (harness *harness) count(t *testing.T, table string) int {
	t.Helper()
	var count int
	if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func (harness *harness) rollups(t *testing.T, customerID uuid.UUID) []rollup {
	t.Helper()
	rows, err := harness.pool.Query(t.Context(),
		"SELECT period_start, period_end, revenue_net_nanos FROM period_rollups WHERE customer_id = $1 ORDER BY period_start", customerID)
	if err != nil {
		t.Fatalf("select rollups: %v", err)
	}
	defer rows.Close()
	var rollups []rollup
	for rows.Next() {
		var current rollup
		if err := rows.Scan(&current.PeriodStart, &current.PeriodEnd, &current.RevenueNet); err != nil {
			t.Fatalf("scan rollup: %v", err)
		}
		current.PeriodStart = current.PeriodStart.UTC()
		current.PeriodEnd = current.PeriodEnd.UTC()
		rollups = append(rollups, current)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read rollups: %v", err)
	}
	return rollups
}

func (harness *harness) insertPlan(t *testing.T, environment httpapi.Environment, name string) uuid.UUID {
	t.Helper()
	planID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO plans (plan_id, environment, name, target_margin_basis_points, status) VALUES ($1, $2, $3, $4, 'active')",
		planID, string(environment), name, int64(planTargetMargin))
	if err != nil {
		t.Fatalf("insert plan %s: %v", name, err)
	}
	return planID
}

func (harness *harness) setDefaultPlan(t *testing.T, environment httpapi.Environment, planID uuid.UUID) {
	t.Helper()
	if _, err := harness.pool.Exec(t.Context(), "UPDATE environment_settings SET default_plan_id = $1 WHERE environment = $2", planID, string(environment)); err != nil {
		t.Fatalf("set default plan of %s: %v", environment, err)
	}
}

func (harness *harness) createKey(t *testing.T, environment httpapi.Environment, scope apikeys.Scope) string {
	t.Helper()
	_, secret, err := harness.apiKeys.Create(t.Context(), environment, "Billing webhooks", scope, nil)
	if err != nil {
		t.Fatalf("create %s %s key: %v", environment, scope, err)
	}
	return secret
}

func (harness *harness) serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *harness) bearerRequest(t *testing.T, method string, secret string, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, method, revenuePath, body)
	request.Header.Set(authorizationHeader, "Bearer "+secret)
	return harness.serve(request)
}

func (harness *harness) memberRequest(t *testing.T, target string, environment httpapi.Environment) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, http.MethodGet, target, "")
	request.Header.Set(httpapi.EnvironmentHeader, string(environment))
	return harness.serve(request)
}

func newRequest(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func subscription(externalID string, sourceReference string, amount money.Amount, periodStart, periodEnd time.Time) revenue.RecordInput {
	return revenue.RecordInput{
		CustomerExternalID: externalID,
		Kind:               revenue.KindSubscription,
		Amount:             amount,
		PeriodStart:        periodStart,
		PeriodEnd:          periodEnd,
		SourceReference:    sourceReference,
	}
}

func dollars(amount int64) money.Amount {
	return money.Amount(amount * nanosPerDollar)
}

func pointer[Value any](value Value) *Value {
	return &value
}

func subscribeInvalidations(t *testing.T, cacheClient *cache.Client) *redis.PubSub {
	t.Helper()
	subscription := cacheClient.Redis().Subscribe(t.Context(), cacheClient.Key(invalidationChannel))
	t.Cleanup(func() {
		if err := subscription.Close(); err != nil {
			t.Errorf("close subscription: %v", err)
		}
	})
	if _, err := subscription.ReceiveTimeout(t.Context(), waitTimeout); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	return subscription
}

func assertInvalidation(t *testing.T, subscription *redis.PubSub, want cache.Invalidation) {
	t.Helper()
	received, err := subscription.ReceiveTimeout(t.Context(), waitTimeout)
	if err != nil {
		t.Fatalf("receive invalidation: %v", err)
	}
	message, isMessage := received.(*redis.Message)
	if !isMessage {
		t.Fatalf("received %T, want *redis.Message", received)
	}
	var invalidation cache.Invalidation
	if err := json.Unmarshal([]byte(message.Payload), &invalidation); err != nil {
		t.Fatalf("decode invalidation %q: %v", message.Payload, err)
	}
	if diff := cmp.Diff(want, invalidation); diff != "" {
		t.Errorf("invalidation mismatch (-want +got):\n%s", diff)
	}
}

func assertValidationProblem(t *testing.T, err error, locations ...string) {
	t.Helper()
	problem, isProblem := errors.AsType[*httpapi.Problem](err)
	if !isProblem {
		t.Fatalf("error = %v, want a validation problem", err)
	}
	var got []string
	for _, fieldError := range problem.Errors {
		got = append(got, fieldError.Location)
	}
	if problem.Code != "validation_failed" || !cmp.Equal(locations, got) {
		t.Errorf("problem code=%s locations=%v, want validation_failed at %v", problem.Code, got, locations)
	}
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	return body
}

func assertStatus(t *testing.T, recorder *httptest.ResponseRecorder, status int) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d, body %s", recorder.Code, status, recorder.Body.String())
	}
}

func assertProblem(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string, locations ...string) {
	t.Helper()
	assertStatus(t, recorder, status)
	problem := decodeBody(t, recorder)
	if problem["code"] != code {
		t.Errorf("code = %v, want %s", problem["code"], code)
	}
	var got []string
	if fieldErrors, present := problem["errors"]; present {
		for _, fieldError := range fieldErrors.([]any) {
			got = append(got, fieldError.(map[string]any)["location"].(string))
		}
	}
	if diff := cmp.Diff(locations, got); diff != "" {
		t.Errorf("locations mismatch (-want +got):\n%s", diff)
	}
}

func pageItems(t *testing.T, page map[string]any) []map[string]any {
	t.Helper()
	listed, isList := page["items"].([]any)
	if !isList {
		t.Fatalf("page %v has no items list", page)
	}
	items := make([]map[string]any, 0, len(listed))
	for _, entry := range listed {
		item, isObject := entry.(map[string]any)
		if !isObject {
			t.Fatalf("item %v is not an object", entry)
		}
		items = append(items, item)
	}
	return items
}
