package plans_test

import (
	"context"
	"encoding/json"
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
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/plans"
)

const (
	plansPath           = "/api/v1/plans"
	authorizationHeader = "Authorization"
	invalidationChannel = "invalidate"
	waitTimeout         = 10 * time.Second
	pollInterval        = 10 * time.Millisecond
)

type harness struct {
	pool    *pgxpool.Pool
	cache   *cache.Client
	service *plans.Service
	apiKeys *apikeys.Service
	handler http.Handler
}

type testAuthenticator struct {
	apiKeys *apikeys.Authenticator
}

var testStart = time.Date(2026, time.September, 26, 10, 0, 0, 0, time.UTC)

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(testStart)
	logger := logging.New(t.Output(), slog.LevelDebug)
	service := plans.NewService(pool, cacheClient, manualClock)
	apiKeyService := apikeys.NewService(pool, cacheClient, manualClock, logger)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, testAuthenticator{apiKeys: apiKeyService.Authenticator()})
	plans.RegisterRoutes(api, service)
	return &harness{pool: pool, cache: cacheClient, service: service, apiKeys: apiKeyService, handler: mux}
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

func (harness *harness) serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *harness) memberRequest(t *testing.T, method string, target string, environment httpapi.Environment, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, method, target, body)
	request.Header.Set(httpapi.EnvironmentHeader, string(environment))
	return harness.serve(request)
}

func (harness *harness) bearerRequest(t *testing.T, target string, secret string) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, http.MethodGet, target, "")
	request.Header.Set(authorizationHeader, "Bearer "+secret)
	return harness.serve(request)
}

func (harness *harness) createPlan(t *testing.T, environment httpapi.Environment, name string) plans.Plan {
	t.Helper()
	targetMargin := "0.40"
	plan, err := harness.service.Create(t.Context(), environment, plans.CreateInput{Name: name, Mode: plans.ModeMarginTarget, TargetMargin: &targetMargin})
	if err != nil {
		t.Fatalf("create plan %s in %s: %v", name, environment, err)
	}
	return plan
}

func (harness *harness) setDefaultPlan(t *testing.T, environment httpapi.Environment, planID uuid.UUID) {
	t.Helper()
	if _, err := harness.pool.Exec(t.Context(), "UPDATE environment_settings SET default_plan_id = $1 WHERE environment = $2", planID, string(environment)); err != nil {
		t.Fatalf("set default plan of %s: %v", environment, err)
	}
}

func (harness *harness) insertCustomer(t *testing.T, environment httpapi.Environment, externalID string, planID *uuid.UUID) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO customers (customer_id, environment, external_id, plan_id, status) VALUES ($1, $2, $3, $4, 'active')",
		identifiers.New(), string(environment), externalID, planID)
	if err != nil {
		t.Fatalf("insert customer %s: %v", externalID, err)
	}
}

func (harness *harness) planStatus(t *testing.T, planID uuid.UUID) string {
	t.Helper()
	var status string
	if err := harness.pool.QueryRow(t.Context(), "SELECT status FROM plans WHERE plan_id = $1", planID).Scan(&status); err != nil {
		t.Fatalf("read status of plan %s: %v", planID, err)
	}
	return status
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

func waitForLockWaiters(ctx context.Context, t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		var waiting int
		err := pool.QueryRow(ctx, `SELECT count(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
				AND wait_event_type = 'Lock'`).Scan(&waiting)
		if err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if waiting == want {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("sessions waiting on a lock = %d, want %d", waiting, want)
		}
	}
}

func newRequest(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func planPath(planID uuid.UUID) string {
	return plansPath + "/" + identifiers.Encode(identifiers.PrefixPlan, planID)
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

func assertProblem(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) map[string]any {
	t.Helper()
	assertStatus(t, recorder, status)
	problem := decodeBody(t, recorder)
	if problem["code"] != code {
		t.Errorf("code = %v, want %s", problem["code"], code)
	}
	return problem
}

func assertLocations(t *testing.T, problem map[string]any, want ...string) {
	t.Helper()
	fieldErrors, isList := problem["errors"].([]any)
	if !isList {
		t.Fatalf("problem %v has no errors list", problem)
	}
	var locations []string
	for _, fieldError := range fieldErrors {
		fields, isObject := fieldError.(map[string]any)
		if !isObject {
			t.Fatalf("problem error %v is not an object", fieldError)
		}
		location, isString := fields["location"].(string)
		if !isString {
			t.Fatalf("problem error %v has no location", fieldError)
		}
		locations = append(locations, location)
	}
	if diff := cmp.Diff(want, locations); diff != "" {
		t.Errorf("locations mismatch (-want +got):\n%s", diff)
	}
}
