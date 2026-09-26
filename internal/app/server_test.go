package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/installation"
	"github.com/preburn/preburn/internal/logging"
)

const (
	invalidationChannel = "invalidate"
	keysPath            = "/api/v1/api-keys"
)

func TestServerSpansAreNamedAfterTheMatchedRoute(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	spans := tracetest.NewSpanRecorder()
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previousProvider) })
	application, _ := newTestApp(t, RoleAPI)

	application.handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))

	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	if name := ended[0].Name(); name != "GET /readyz" {
		t.Errorf("span name = %q, want %q", name, "GET /readyz")
	}
}

func TestServeLogsSetupLinkWhileSetupIsPending(t *testing.T) {
	configuration := testConfiguration(t)
	pending, pendingLogs := openTestApp(t, configuration, RoleAPI)
	_, stop := startServer(t, pending, pendingLogs)
	stop()

	records := pendingLogs.records(t, logging.InstallationSetupLinkCreated)
	if len(records) != 1 {
		t.Fatalf("logged installation.setup_link_created %d times, want 1", len(records))
	}
	if records[0]["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", records[0]["level"])
	}
	link, _ := records[0]["url"].(string)
	token, isSetupLink := strings.CutPrefix(link, testPublicURL+"/setup#")
	if !isSetupLink {
		t.Fatalf("url = %q, want a setup link under %s", link, testPublicURL)
	}
	_, err := pending.Installation.CompleteSetup(t.Context(), installation.CompleteSetupInput{
		Token:       token,
		Email:       testEmail,
		DisplayName: testDisplayName,
		Password:    testPassword,
	})
	if err != nil {
		t.Fatalf("complete setup with the logged link: %v", err)
	}

	complete, completeLogs := openTestApp(t, configuration, RoleAPI)
	_, stop = startServer(t, complete, completeLogs)
	stop()

	if records := completeLogs.records(t, logging.InstallationSetupLinkCreated); len(records) != 0 {
		t.Errorf("logged installation.setup_link_created %d times after setup, want 0", len(records))
	}
}

func TestServeClearsKeysRevokedByAnotherProcess(t *testing.T) {
	application, logs := newTestApp(t, RoleAPI)
	baseURL, _ := startServer(t, application, logs)
	key, secret, err := application.APIKeys.Create(t.Context(), httpapi.EnvironmentTest, testKeyName, apikeys.ScopeAdmin, nil)
	if err != nil {
		t.Fatalf("create admin key: %v", err)
	}
	if status := bearerStatus(t, baseURL+keysPath, secret); status != http.StatusForbidden {
		t.Fatalf("status before revoke = %d, want 403 from the cached key", status)
	}
	waitForInvalidationSubscriber(t, application)
	otherProcess := apikeys.NewService(application.Pool, application.Cache, application.Clock, application.Logger)

	if err := otherProcess.Revoke(t.Context(), httpapi.EnvironmentTest, key.ID); err != nil {
		t.Fatalf("revoke from another process: %v", err)
	}

	waitFor(t, "revoked key rejected", func() bool {
		return bearerStatus(t, baseURL+keysPath, secret) == http.StatusUnauthorized
	})
}

func bearerStatus(t *testing.T, target string, secret string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("create request %s: %v", target, err)
	}
	request.Header.Set(authorizationHeader, "Bearer "+secret)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close body of %s: %v", target, err)
	}
	return response.StatusCode
}
