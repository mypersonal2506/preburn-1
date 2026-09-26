package members_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/members"
)

const (
	testEmail       = "sam@example.com"
	testPassword    = "correct horse battery"
	insecureURL     = "http://localhost:8080"
	runtimeProbe    = "/api/v1/runtime-probe"
	sessionCookie   = "preburn_session"
	csrfCookie      = "preburn_csrf"
	csrfHeader      = "X-CSRF-Token"
	environmentName = "test"
)

type harness struct {
	pool    *pgxpool.Pool
	clock   *clock.Manual
	service *members.Service
	handler http.Handler
}

type browser struct {
	sessionToken string
	csrfToken    string
}

type queryHook struct {
	queryName string
	run       func(ctx context.Context)
	armed     atomic.Bool
}

type hookedQueryKey struct{}

var testStart = time.Date(2026, time.September, 26, 10, 0, 0, 0, time.UTC)

func newHarness(t *testing.T, publicURL string, trustedProxies ...netip.Prefix) *harness {
	t.Helper()
	pool := databasetest.NewPool(t)
	return newHarnessOnPools(t, pool, pool, publicURL, trustedProxies)
}

func newHookedHarness(t *testing.T, hook *queryHook) *harness {
	t.Helper()
	pool := databasetest.NewPool(t)
	configuration := pool.Config().Copy()
	configuration.ConnConfig.Tracer = hook
	hookedPool, err := pgxpool.NewWithConfig(t.Context(), configuration)
	if err != nil {
		t.Fatalf("open hooked pool: %v", err)
	}
	t.Cleanup(hookedPool.Close)
	return newHarnessOnPools(t, pool, hookedPool, insecureURL, nil)
}

func newHarnessOnPools(t *testing.T, pool *pgxpool.Pool, servicePool *pgxpool.Pool, publicURL string, trustedProxies []netip.Prefix) *harness {
	t.Helper()
	parsedURL, err := url.Parse(publicURL)
	if err != nil {
		t.Fatalf("parse public URL: %v", err)
	}
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(testStart)
	configuration := config.Config{PublicURL: parsedURL, TrustedProxies: trustedProxies}
	service := members.NewService(servicePool, cacheClient, manualClock, configuration)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logging.New(t.Output(), slog.LevelDebug), members.NewSessionAuthenticator(service.Sessions()))
	members.RegisterRoutes(api, service)
	httpapi.Register(api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID: "runtime-probe",
		Method:      http.MethodGet,
		Path:        runtimeProbe,
	}, func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	return &harness{pool: pool, clock: manualClock, service: service, handler: mux}
}

func (harness *harness) createMember(t *testing.T, email string, password *string) members.Member {
	t.Helper()
	var member members.Member
	err := database.InTransaction(t.Context(), harness.pool, func(ctx context.Context, transaction pgx.Tx) error {
		var err error
		member, err = harness.service.CreateMember(ctx, transaction, members.CreateMemberInput{Email: email, DisplayName: testDisplayName, Password: password})
		return err
	})
	if err != nil {
		t.Fatalf("create member %s: %v", email, err)
	}
	return member
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

func (harness *harness) serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *harness) login(t *testing.T, email string, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		t.Fatalf("encode login body: %v", err)
	}
	return harness.serve(newRequest(t, http.MethodPost, "/api/v1/auth/login", string(body)))
}

func (harness *harness) signIn(t *testing.T) browser {
	t.Helper()
	recorder := harness.login(t, testEmail, testPassword)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	cookies := responseCookies(t, recorder)
	return browser{sessionToken: cookies[sessionCookie].Value, csrfToken: cookies[csrfCookie].Value}
}

func (harness *harness) sessionStatus(t *testing.T, browser browser) int {
	t.Helper()
	return harness.serve(browser.request(t, http.MethodGet, "/api/v1/auth/me", "")).Code
}

func (browser browser) request(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()
	request := newRequest(t, method, target, body)
	addCookie(request, sessionCookie, browser.sessionToken)
	addCookie(request, csrfCookie, browser.csrfToken)
	request.Header.Set(csrfHeader, browser.csrfToken)
	request.Header.Set(httpapi.EnvironmentHeader, environmentName)
	return request
}

func newRequest(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func addCookie(request *http.Request, name string, value string) {
	request.AddCookie(&http.Cookie{Name: name, Value: value}) //nolint:gosec // G124: a request cookie carries no attributes.
}

func responseCookies(t *testing.T, recorder *httptest.ResponseRecorder) map[string]*http.Cookie {
	t.Helper()
	cookies := map[string]*http.Cookie{}
	for _, line := range recorder.Header().Values("Set-Cookie") {
		cookie, err := http.ParseSetCookie(line)
		if err != nil {
			t.Fatalf("parse Set-Cookie %q: %v", line, err)
		}
		cookies[cookie.Name] = cookie
	}
	return cookies
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	return body
}

func assertProblem(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) map[string]any {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d, body %s", recorder.Code, status, recorder.Body.String())
	}
	problem := decodeBody(t, recorder)
	if problem["code"] != code {
		t.Errorf("code = %v, want %s", problem["code"], code)
	}
	return problem
}

func pointer[Value any](value Value) *Value {
	return &value
}
