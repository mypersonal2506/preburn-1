package installation_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/installation"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/members"
)

const (
	insecureURL     = "http://localhost:8080"
	secureURL       = "https://preburn.example.com"
	testEmail       = "sam@example.com"
	testDisplayName = "Sam Rivera"
	testPassword    = "correct horse battery"
	sessionCookie   = "preburn_session"
	csrfCookie      = "preburn_csrf"
	environmentName = "test"
)

type harness struct {
	pool    *pgxpool.Pool
	service *installation.Service
	handler http.Handler
}

var testStart = time.Date(2026, time.September, 26, 10, 0, 0, 0, time.UTC)

func newHarness(t *testing.T, publicURL string) *harness {
	t.Helper()
	parsedURL, err := url.Parse(publicURL)
	if err != nil {
		t.Fatalf("parse public URL: %v", err)
	}
	pool := databasetest.NewPool(t)
	manualClock := clock.NewManual(testStart)
	configuration := config.Config{PublicURL: parsedURL}
	memberService := members.NewService(pool, cachetest.NewClient(t), manualClock, configuration)
	service := installation.NewService(pool, memberService, manualClock, configuration)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logging.New(t.Output(), slog.LevelDebug), members.NewSessionAuthenticator(memberService.Sessions()))
	members.RegisterRoutes(api, memberService)
	installation.RegisterRoutes(api, service)
	return &harness{pool: pool, service: service, handler: mux}
}

func (harness *harness) serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *harness) prepareSetupToken(t *testing.T) string {
	t.Helper()
	link, pending, err := harness.service.PrepareSetupLink(t.Context())
	if err != nil {
		t.Fatalf("prepare setup link: %v", err)
	}
	if !pending {
		t.Fatal("prepare setup link: setup is not pending")
	}
	return setupToken(t, link)
}

func (harness *harness) completeSetup(t *testing.T, token string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"token":        token,
		"email":        testEmail,
		"display_name": testDisplayName,
		"password":     testPassword,
	})
	if err != nil {
		t.Fatalf("encode setup body: %v", err)
	}
	return harness.serve(newRequest(t, http.MethodPost, "/api/v1/setup", string(body)))
}

func (harness *harness) setupRequired(t *testing.T) bool {
	t.Helper()
	recorder := harness.serve(newRequest(t, http.MethodGet, "/api/v1/setup/status", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("setup status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	setupRequired, isBool := decodeBody(t, recorder)["setup_required"].(bool)
	if !isBool {
		t.Fatalf("setup status body %s has no boolean setup_required", recorder.Body.String())
	}
	return setupRequired
}

func (harness *harness) readInstallation(t *testing.T) ([]byte, *time.Time) {
	t.Helper()
	var setupTokenHash []byte
	var setupCompletedAt *time.Time
	err := harness.pool.QueryRow(t.Context(), "SELECT setup_token_hash, setup_completed_at FROM installation").Scan(&setupTokenHash, &setupCompletedAt)
	if err != nil {
		t.Fatalf("read installation: %v", err)
	}
	return setupTokenHash, setupCompletedAt
}

func (harness *harness) memberIdentifier(t *testing.T, email string) string {
	t.Helper()
	var memberID uuid.UUID
	if err := harness.pool.QueryRow(t.Context(), "SELECT member_id FROM members WHERE email = $1", email).Scan(&memberID); err != nil {
		t.Fatalf("read member %s: %v", email, err)
	}
	return identifiers.Encode(identifiers.PrefixMember, memberID)
}

func setupToken(t *testing.T, link string) string {
	t.Helper()
	parsedLink, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse setup link %q: %v", link, err)
	}
	return parsedLink.Fragment
}

func newRequest(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
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

func assertProblem(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d, body %s", recorder.Code, status, recorder.Body.String())
	}
	if problemCode := decodeBody(t, recorder)["code"]; problemCode != code {
		t.Errorf("code = %v, want %s", problemCode, code)
	}
}
