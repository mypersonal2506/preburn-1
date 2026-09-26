package pricing_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/catalog"
	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/jobs/jobstest"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	overridesPath       = "/api/v1/pricing/overrides"
	quotePath           = "/api/v1/pricing/quote"
	modelsPath          = "/api/v1/pricing/models"
	modelAttributesPath = "/api/v1/pricing/model-attributes"
	metersPath          = "/api/v1/pricing/meters"
	authorizationHeader = "Authorization"
	invalidationChannel = "invalidate"
	waitTimeout         = 10 * time.Second
	pollInterval        = 20 * time.Millisecond
	veoQuoteBody        = `{"provider":"fal_ai","model":"fal-ai/veo3.1/fast","attributes":{"audio":true,"resolution":"720p"},"usage":{"output_seconds":"8"}}`
	unknownModelBody    = `{"provider":"acme","model":"acme-video-1","usage":{"output_seconds":"8"}}`
)

type harness struct {
	pool    *pgxpool.Pool
	cache   *cache.Client
	jobs    *river.Client[pgx.Tx]
	clock   *clock.Manual
	files   catalogfiles.Catalog
	service *pricing.Service
	apiKeys *apikeys.Service
	handler http.Handler
}

type testAuthenticator struct {
	apiKeys *apikeys.Authenticator
}

type queryHook struct {
	queryName string
	run       func(ctx context.Context)
	armed     atomic.Bool
}

type hookedQueryKey struct{}

var serviceStart = importStart.Add(time.Hour)

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
	if _, err := pricing.ImportCurated(t.Context(), pool, files, clock.NewManual(importStart), discardLogger()); err != nil {
		t.Fatalf("import curated catalog: %v", err)
	}
	if _, err := pricing.ImportLiteLLM(t.Context(), pool, fetchedSnapshot(liteLLMFixture, importStart), clock.NewManual(importStart), discardLogger()); err != nil {
		t.Fatalf("import litellm fixture: %v", err)
	}
	jobs := jobstest.NewInsertClient(t, pool)
	service := pricing.NewService(pool, cacheClient, jobs, files, manualClock)
	apiKeyService := apikeys.NewService(pool, cacheClient, manualClock, logger)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, testAuthenticator{apiKeys: apiKeyService.Authenticator()})
	pricing.RegisterRoutes(api, service)
	return &harness{
		pool:    pool,
		cache:   cacheClient,
		jobs:    jobs,
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

func (harness *harness) newHookedService(t *testing.T, hook *queryHook) *pricing.Service {
	t.Helper()
	configuration := harness.pool.Config().Copy()
	configuration.ConnConfig.Tracer = hook
	hookedPool, err := pgxpool.NewWithConfig(t.Context(), configuration)
	if err != nil {
		t.Fatalf("open hooked pool: %v", err)
	}
	t.Cleanup(hookedPool.Close)
	return pricing.NewService(hookedPool, harness.cache, harness.jobs, harness.files, harness.clock)
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

func (harness *harness) memberRequest(t *testing.T, method string, target string, environment httpapi.Environment, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, method, target, body)
	request.Header.Set(httpapi.EnvironmentHeader, string(environment))
	return harness.serve(request)
}

func (harness *harness) bearerRequest(t *testing.T, method string, target string, secret string, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, method, target, body)
	request.Header.Set(authorizationHeader, "Bearer "+secret)
	return harness.serve(request)
}

func (harness *harness) serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *harness) createKey(t *testing.T, environment httpapi.Environment, scope apikeys.Scope) string {
	t.Helper()
	_, secret, err := harness.apiKeys.Create(t.Context(), environment, "Operations", scope, nil)
	if err != nil {
		t.Fatalf("create %s %s key: %v", environment, scope, err)
	}
	return secret
}

func (harness *harness) quote(t *testing.T, environment httpapi.Environment, body string) map[string]any {
	t.Helper()
	recorder := harness.memberRequest(t, http.MethodPost, quotePath, environment, body)
	assertStatus(t, recorder, http.StatusOK)
	return decodeBody(t, recorder)
}

func (harness *harness) createOverride(t *testing.T, environment httpapi.Environment, body string) map[string]any {
	t.Helper()
	recorder := harness.memberRequest(t, http.MethodPost, overridesPath, environment, body)
	assertStatus(t, recorder, http.StatusCreated)
	return decodeBody(t, recorder)
}

func (harness *harness) insertUnknownModelOverride(t *testing.T, environment httpapi.Environment) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO pricing_overrides (pricing_override_id, environment, provider, model, meter, unit_price_nanos, unit_quantity, effective_from)
		VALUES ($1, $2, 'acme', 'acme-video-1', 'output_seconds', 500000000, 1, $3)`,
		identifiers.New(), string(environment), importStart)
	if err != nil {
		t.Fatalf("insert override for acme-video-1 in %s: %v", environment, err)
	}
}

func (harness *harness) deprecateModel(t *testing.T, provider, model string) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		"UPDATE pricing_rules SET status = 'deprecated', effective_to = $3 WHERE provider = $1 AND model = $2",
		provider, model, importStart.Add(time.Minute))
	if err != nil {
		t.Fatalf("deprecate %s/%s: %v", provider, model, err)
	}
}

func subscribe(t *testing.T, cacheClient *cache.Client, ruleSets *pricing.RuleSetCache) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		cacheClient.SubscribeInvalidations(ctx, ruleSets.Invalidate, ruleSets.Clear)
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

func newRequest(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func overridePath(exposedID any) string {
	return overridesPath + "/" + exposedID.(string)
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
	if fieldErrors, present := problem["errors"].([]any); present {
		for _, fieldError := range fieldErrors {
			got = append(got, fieldError.(map[string]any)["location"].(string))
		}
	}
	if diff := cmp.Diff(locations, got); diff != "" {
		t.Errorf("locations mismatch (-want +got):\n%s", diff)
	}
}

func assertCost(t *testing.T, quote map[string]any, want any) {
	t.Helper()
	if quote["cost"] != want {
		t.Errorf("cost = %v, want %v, quote %v", quote["cost"], want, quote)
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
		items = append(items, entry.(map[string]any))
	}
	return items
}
