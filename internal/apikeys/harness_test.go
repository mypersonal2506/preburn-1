package apikeys_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	testName            = "Checkout service"
	runtimeProbe        = "/api/v1/probes/runtime"
	adminProbe          = "/api/v1/probes/admin"
	keysPath            = "/api/v1/api-keys"
	authorizationHeader = "Authorization"
	invalidationChannel = "invalidate"
	waitTimeout         = 10 * time.Second
	pollInterval        = 20 * time.Millisecond
)

type harness struct {
	pool     *pgxpool.Pool
	cache    *cache.Client
	clock    *clock.Manual
	logs     *lockedBuffer
	logger   *logging.Logger
	service  *apikeys.Service
	handler  http.Handler
	memberID uuid.UUID
}

type testAuthenticator struct {
	apiKeys  *apikeys.Authenticator
	memberID uuid.UUID
}

type probeOutput struct {
	Body probeBody
}

type probeBody struct {
	APIKeyID    string              `json:"api_key_id"`
	Environment httpapi.Environment `json:"environment"`
	Scope       apikeys.Scope       `json:"scope"`
}

type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
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
	logs := &lockedBuffer{}
	logger := logging.New(io.MultiWriter(t.Output(), logs), slog.LevelDebug)
	service := apikeys.NewService(pool, cacheClient, manualClock, logger)
	memberID := identifiers.New()
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, testAuthenticator{apiKeys: service.Authenticator(), memberID: memberID})
	apikeys.RegisterRoutes(api, service)
	for path, group := range map[string]httpapi.RouteGroup{runtimeProbe: httpapi.RouteGroupRuntime, adminProbe: httpapi.RouteGroupAdmin} {
		httpapi.Register(api, group, huma.Operation{
			OperationID: string(group) + "-probe",
			Method:      http.MethodGet,
			Path:        path,
		}, probe)
	}
	return &harness{
		pool:     pool,
		cache:    cacheClient,
		clock:    manualClock,
		logs:     logs,
		logger:   logger,
		service:  service,
		handler:  mux,
		memberID: memberID,
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
	return httpapi.MemberPrincipal{MemberID: authenticator.memberID, Environment: environment}, nil
}

func probe(ctx context.Context, _ *struct{}) (*probeOutput, error) {
	principal, isAPIKey := httpapi.PrincipalFromContext(ctx).(apikeys.Principal)
	if !isAPIKey {
		return nil, fmt.Errorf("principal %T is not an API key", httpapi.PrincipalFromContext(ctx))
	}
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return &probeOutput{Body: probeBody{
		APIKeyID:    identifiers.Encode(identifiers.PrefixAPIKey, principal.APIKeyID),
		Environment: environment,
		Scope:       principal.Scope,
	}}, nil
}

func (harness *harness) newHookedService(t *testing.T, hook *queryHook) *apikeys.Service {
	t.Helper()
	configuration := harness.pool.Config().Copy()
	configuration.ConnConfig.Tracer = hook
	hookedPool, err := pgxpool.NewWithConfig(t.Context(), configuration)
	if err != nil {
		t.Fatalf("open hooked pool: %v", err)
	}
	t.Cleanup(hookedPool.Close)
	return apikeys.NewService(hookedPool, harness.cache, harness.clock, harness.logger)
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

func (harness *harness) createKey(t *testing.T, environment httpapi.Environment, scope apikeys.Scope) (apikeys.APIKey, string) {
	t.Helper()
	key, secret, err := harness.service.Create(t.Context(), environment, testName, scope, nil)
	if err != nil {
		t.Fatalf("create %s %s key: %v", environment, scope, err)
	}
	return key, secret
}

func (harness *harness) serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *harness) bearerRequest(t *testing.T, target string, secret string) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, http.MethodGet, target, "")
	request.Header.Set(authorizationHeader, "Bearer "+secret)
	return harness.serve(request)
}

func (harness *harness) memberRequest(t *testing.T, method string, target string, environment httpapi.Environment, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, method, target, body)
	request.Header.Set(httpapi.EnvironmentHeader, string(environment))
	return harness.serve(request)
}

func (harness *harness) lastUsedAt(t *testing.T, apiKeyID uuid.UUID) *time.Time {
	t.Helper()
	var lastUsedAt *time.Time
	if err := harness.pool.QueryRow(t.Context(), "SELECT last_used_at FROM api_keys WHERE api_key_id = $1", apiKeyID).Scan(&lastUsedAt); err != nil {
		t.Fatalf("read last_used_at: %v", err)
	}
	return lastUsedAt
}

func (buffer *lockedBuffer) Write(content []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.Write(content)
}

func (buffer *lockedBuffer) events(t *testing.T) []string {
	t.Helper()
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	var events []string
	for line := range strings.Lines(buffer.buffer.String()) {
		var entry struct {
			Message string `json:"msg"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		events = append(events, entry.Message)
	}
	return events
}

func newRequest(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func wellFormedSecret(environment httpapi.Environment, scope apikeys.Scope) string {
	return "pb_" + string(environment) + "_" + string(scope) + "_" + secrets.RandomBase62(32)
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
	var locations []string
	for _, fieldError := range problem["errors"].([]any) {
		locations = append(locations, fieldError.(map[string]any)["location"].(string))
	}
	if diff := cmp.Diff(want, locations); diff != "" {
		t.Errorf("locations mismatch (-want +got):\n%s", diff)
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
