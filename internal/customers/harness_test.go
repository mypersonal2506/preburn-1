package customers_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
)

const (
	testExternalID      = "acme"
	renamedDisplayName  = "Renamed"
	customersPath       = "/api/v1/customers/"
	authorizationHeader = "Authorization"
	invalidationChannel = "invalidate"
	waitTimeout         = 10 * time.Second
	pollInterval        = 20 * time.Millisecond
)

type harness struct {
	pool    *pgxpool.Pool
	cache   *cache.Client
	clock   *clock.Manual
	service *customers.Service
	apiKeys *apikeys.Service
	handler http.Handler
}

type queryHook struct {
	queryName string
	run       func(ctx context.Context)
	armed     atomic.Bool
}

type hookedQueryKey struct{}

var testStart = time.Date(2026, time.September, 26, 10, 0, 0, 0, time.UTC)

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(testStart)
	logger := logging.New(t.Output(), slog.LevelDebug)
	service := customers.NewService(pool, cacheClient, manualClock)
	apiKeys := apikeys.NewService(pool, cacheClient, manualClock, logger)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, apiKeys.Authenticator())
	customers.RegisterRoutes(api, service)
	return &harness{
		pool:    pool,
		cache:   cacheClient,
		clock:   manualClock,
		service: service,
		apiKeys: apiKeys,
		handler: mux,
	}
}

func (harness *harness) newHookedService(t *testing.T, hook *queryHook) *customers.Service {
	t.Helper()
	configuration := harness.pool.Config().Copy()
	configuration.ConnConfig.Tracer = hook
	hookedPool, err := pgxpool.NewWithConfig(t.Context(), configuration)
	if err != nil {
		t.Fatalf("open hooked pool: %v", err)
	}
	t.Cleanup(hookedPool.Close)
	return customers.NewService(hookedPool, harness.cache, harness.clock)
}

func (hook *queryHook) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name: "+hook.queryName+" ") {
		return context.WithValue(ctx, hookedQueryKey{}, true)
	}
	return ctx
}

func (hook *queryHook) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(hookedQueryKey{}) != nil && hook.armed.Swap(false) {
		hook.run(ctx)
	}
}

func (harness *harness) createPlan(t *testing.T, environment httpapi.Environment, name string, status string) uuid.UUID {
	t.Helper()
	planID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO plans (plan_id, environment, name, target_margin_basis_points, status) VALUES ($1, $2, $3, 4000, $4)",
		planID, environment, name, status)
	if err != nil {
		t.Fatalf("insert %s plan %s: %v", status, name, err)
	}
	return planID
}

func (harness *harness) upsert(t *testing.T, environment httpapi.Environment, externalID string, input customers.UpsertInput) customers.Customer {
	t.Helper()
	customer, err := harness.service.Upsert(t.Context(), environment, externalID, input)
	if err != nil {
		t.Fatalf("upsert customer %s: %v", externalID, err)
	}
	return customer
}

func (harness *harness) createKey(t *testing.T, environment httpapi.Environment, scope apikeys.Scope) string {
	t.Helper()
	_, secret, err := harness.apiKeys.Create(t.Context(), environment, "Checkout service", scope, nil)
	if err != nil {
		t.Fatalf("create %s %s key: %v", environment, scope, err)
	}
	return secret
}

func (harness *harness) putCustomer(t *testing.T, secret string, externalIDPath string, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPut, customersPath+externalIDPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if secret != "" {
		request.Header.Set(authorizationHeader, "Bearer "+secret)
	}
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *harness) testCustomerCount(t *testing.T, externalID string) int {
	t.Helper()
	var count int
	err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM customers WHERE environment = 'test' AND external_id = $1", externalID).Scan(&count)
	if err != nil {
		t.Fatalf("count customers: %v", err)
	}
	return count
}

func (harness *harness) renameInDatabase(t *testing.T, customerID uuid.UUID) {
	t.Helper()
	if _, err := harness.pool.Exec(t.Context(), "UPDATE customers SET display_name = $2 WHERE customer_id = $1", customerID, renamedDisplayName); err != nil {
		t.Fatalf("rename customer in the database: %v", err)
	}
}

func subscribe(t *testing.T, cacheClient *cache.Client, customerCache *customers.Cache) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		cacheClient.SubscribeInvalidations(ctx, customerCache.Invalidate, customerCache.Clear)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	channel := cacheClient.Key(invalidationChannel)
	waitFor(t, "subscriber on "+channel, func() bool {
		counts, err := cacheClient.Redis().PubSubNumSub(t.Context(), channel).Result()
		if err != nil {
			t.Fatalf("pubsub numsub: %v", err)
		}
		return counts[channel] == 1
	})
}

func waitFor(t *testing.T, description string, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	deadline := time.After(waitTimeout)
	for !condition() {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("%s: not reached within %s", description, waitTimeout)
		}
	}
}

func metadata(t *testing.T, object string) map[string]json.RawMessage {
	t.Helper()
	var values map[string]json.RawMessage
	if err := json.Unmarshal([]byte(object), &values); err != nil {
		t.Fatalf("decode metadata %s: %v", object, err)
	}
	return values
}

func pointer[Value any](value Value) *Value {
	return &value
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
