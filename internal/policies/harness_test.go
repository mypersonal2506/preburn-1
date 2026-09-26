package policies_test

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

	"github.com/preburn/preburn/catalog"
	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
)

const (
	policiesPath          = "/api/v1/policies"
	previewPath           = "/api/v1/policies/preview"
	parameterMappingsPath = "/api/v1/policies/parameter-mappings"
	authorizationHeader   = "Authorization"
	invalidationChannel   = "invalidate"
	waitTimeout           = 10 * time.Second
	pollInterval          = 10 * time.Millisecond
	nanosPerDollar        = 1_000_000_000
)

type harness struct {
	pool    *pgxpool.Pool
	cache   *cache.Client
	clock   *clock.Manual
	files   catalogfiles.Catalog
	service *policies.Service
	apiKeys *apikeys.Service
	handler http.Handler
}

type testAuthenticator struct {
	apiKeys *apikeys.Authenticator
}

var serviceStart = time.Date(2026, time.September, 26, 10, 0, 0, 0, time.UTC)

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(serviceStart)
	logger := logging.New(t.Output(), slog.LevelDebug)
	files, err := catalogfiles.Load(catalog.Files)
	if err != nil {
		t.Fatalf("load catalog files: %v", err)
	}
	service := policies.NewService(pool, cacheClient, files, manualClock)
	apiKeyService := apikeys.NewService(pool, cacheClient, manualClock, logger)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, testAuthenticator{apiKeys: apiKeyService.Authenticator()})
	policies.RegisterRoutes(api, service)
	return &harness{
		pool:    pool,
		cache:   cacheClient,
		clock:   manualClock,
		files:   files,
		service: service,
		apiKeys: apiKeyService,
		handler: mux,
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

func (harness *harness) memberRequest(t *testing.T, method string, target string, environment httpapi.Environment, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set(httpapi.EnvironmentHeader, string(environment))
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *harness) bearerRequest(t *testing.T, target string, secret string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	request.Header.Set(authorizationHeader, "Bearer "+secret)
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *harness) createPolicy(t *testing.T, environment httpapi.Environment, document string) map[string]any {
	t.Helper()
	recorder := harness.memberRequest(t, http.MethodPost, policiesPath, environment, document)
	assertStatus(t, recorder, http.StatusCreated)
	return decodeBody(t, recorder)
}

func (harness *harness) insertPlan(t *testing.T, environment httpapi.Environment, allowance money.Amount) uuid.UUID {
	t.Helper()
	planID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO plans (plan_id, environment, name, target_margin_basis_points, allowance_nanos, status) VALUES ($1, $2, $3, 0, $4, 'active')",
		planID, string(environment), "plan "+planID.String(), allowance)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	return planID
}

func (harness *harness) insertArchivedPlan(t *testing.T, environment httpapi.Environment) uuid.UUID {
	t.Helper()
	planID := harness.insertPlan(t, environment, dollars(10))
	harness.archivePlan(t, identifiers.Encode(identifiers.PrefixPlan, planID))
	return planID
}

func (harness *harness) archivePlan(t *testing.T, exposedPlanID string) {
	t.Helper()
	planID, err := identifiers.Decode(identifiers.PrefixPlan, exposedPlanID)
	if err != nil {
		t.Fatalf("decode plan id %s: %v", exposedPlanID, err)
	}
	if _, err := harness.pool.Exec(t.Context(), "UPDATE plans SET status = 'archived' WHERE plan_id = $1", planID); err != nil {
		t.Fatalf("archive plan %s: %v", exposedPlanID, err)
	}
}

func (harness *harness) insertCustomer(t *testing.T, environment httpapi.Environment, planID *uuid.UUID, status string) uuid.UUID {
	t.Helper()
	customerID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO customers (customer_id, environment, external_id, plan_id, status) VALUES ($1, $2, $3, $4, $5)",
		customerID, string(environment), "customer-"+customerID.String(), planID, status)
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	return customerID
}

func (harness *harness) insertModelAlias(t *testing.T, provider string, alias string, model string) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(), "INSERT INTO provider_model_aliases (provider, alias, model) VALUES ($1, $2, $3)", provider, alias, model)
	if err != nil {
		t.Fatalf("insert alias %s of %s: %v", alias, provider, err)
	}
}

func subscribeInvalidationMessages(t *testing.T, cacheClient *cache.Client) *redis.PubSub {
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

func subscribeCache(t *testing.T, cacheClient *cache.Client, policyCache *policies.Cache) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		cacheClient.SubscribeInvalidations(ctx, policyCache.Invalidate, policyCache.Clear)
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

func policyPath(exposedID any) string {
	return policiesPath + "/" + exposedID.(string)
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

func problemLocations(t *testing.T, problem map[string]any) []string {
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
	return locations
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

func dollars(amount int64) money.Amount {
	return money.Amount(amount * nanosPerDollar)
}
