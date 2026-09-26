package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	testRedisURLVariable      = "PREBURN_TEST_REDIS_URL"
	testPoolConnections       = 4
	testPublicURL             = "http://localhost:8080"
	testEmail                 = "sam@example.com"
	testDisplayName           = "Sam Rivera"
	testPassword              = "correct horse battery"
	testKeyName               = "Checkout service"
	loopbackAddress           = "127.0.0.1:0"
	waitTimeout               = 10 * time.Second
	testDecisionRetentionDays = 90
	pollInterval              = 20 * time.Millisecond
	minimumLiteLLMRules       = 1_000

	refreshedLiteLLMSnapshot = `{
		"gpt-4o": {"litellm_provider": "openai", "input_cost_per_token": 2e-06},
		"refresh-example": {"litellm_provider": "openai", "input_cost_per_token": 1e-06}
	}`
)

type lockedBuffer struct {
	mutex    sync.Mutex
	contents bytes.Buffer
}

type modelPrice struct {
	model string
	nanos int64
}

func TestServeAnswersHealthAPIAndDashboard(t *testing.T) {
	application, logs := newTestApp(t, RoleAPI)
	baseURL, stop := startServer(t, application, logs)

	tests := []struct {
		name        string
		path        string
		status      int
		contentType string
		body        string
	}{
		{name: "liveness", path: "/healthz", status: http.StatusOK, contentType: "application/json", body: `"status":"ok"`},
		{name: "readiness", path: "/readyz", status: http.StatusOK, contentType: "application/json", body: `"status":"ready"`},
		{name: "openapi document", path: "/api/v1/openapi.json", status: http.StatusOK, contentType: "application/openapi+json", body: `"title":"Preburn"`},
		{name: "setup status", path: "/api/v1/setup/status", status: http.StatusOK, contentType: "application/json", body: `"setup_required":true`},
		{name: "dashboard", path: "/", status: http.StatusOK, contentType: "text/html; charset=utf-8", body: "Dashboard not built. Run make web-build."},
		{name: "unknown api path", path: "/api/v1/unknown", status: http.StatusNotFound, contentType: "application/problem+json", body: `"code":"not_found"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, contentType, body := get(t, baseURL+test.path)

			if status != test.status {
				t.Errorf("status = %d, want %d, body %s", status, test.status, body)
			}
			if contentType != test.contentType {
				t.Errorf("Content-Type = %q, want %q", contentType, test.contentType)
			}
			if !strings.Contains(body, test.body) {
				t.Errorf("body = %s, want it to contain %s", body, test.body)
			}
		})
	}

	stop()
	for _, event := range []logging.Event{logging.ServerStarted, logging.ServerStopped} {
		if records := logs.records(t, event); len(records) != 1 {
			t.Errorf("logged %s %d times, want 1", event, len(records))
		}
	}
	started := logs.records(t, logging.ServerStarted)[0]
	if address := strings.TrimPrefix(baseURL, "http://"); started["address"] != address {
		t.Errorf("server.started address = %v, want %s", started["address"], address)
	}
}

func TestWorkWorksMemberCleanupJobsUntilStopped(t *testing.T) {
	application, logs := newTestApp(t, RoleWorker)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := make(chan error, 1)
	go func() {
		stopped <- application.Work(ctx)
	}()
	waitFor(t, "worker.started logged", func() bool {
		return len(logs.records(t, logging.WorkerStarted)) == 1
	})

	if _, err := application.Jobs.Insert(t.Context(), members.CleanupArgs{}, nil); err != nil {
		t.Fatalf("insert member_cleanup job: %v", err)
	}
	waitFor(t, "members.cleanup_completed logged", func() bool {
		return len(logs.records(t, logging.MembersCleanupCompleted)) == 1
	})
	cancel()

	if err := receive(t, stopped); err != nil {
		t.Fatalf("work: %v", err)
	}
	if records := logs.records(t, logging.WorkerStopped); len(records) != 1 {
		t.Errorf("logged worker.stopped %d times, want 1", len(records))
	}
}

func TestNewReportsUnreachableRedis(t *testing.T) {
	configuration := testConfiguration(t)
	configuration.RedisURL = &url.URL{Scheme: "redis", Host: "127.0.0.1:1"}

	application, err := New(t.Context(), configuration, RoleAPI, logging.New(io.Discard, slog.LevelDebug))

	if err == nil {
		t.Fatalf("New with an unreachable Redis returned no error, close: %v", application.Close())
	}
	if !strings.Contains(err.Error(), "open cache") {
		t.Errorf("error = %v, want it to name the cache", err)
	}
}

func TestMigrateAppliesPendingMigrationsAndImportsTheCatalog(t *testing.T) {
	pool := databasetest.NewEmptyPool(t)
	configuration := migrateConfiguration(t, pool)
	logs := &lockedBuffer{}

	for range 2 {
		if err := Migrate(t.Context(), configuration, logging.New(logs, slog.LevelDebug)); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}

	var exists bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass('river_job') IS NOT NULL").Scan(&exists); err != nil || !exists {
		t.Errorf("river_job exists = %t, err %v, want true", exists, err)
	}
	records := logs.records(t, logging.DatabaseMigrationsApplied)
	if len(records) != 2 {
		t.Fatalf("logged database.migrations_applied %d times, want 2", len(records))
	}
	if applied := records[1]["versions"].([]any); len(applied) != 0 {
		t.Errorf("second run applied versions %v, want none", applied)
	}
	assertCatalogImportedTwice(t, pool, logs)
}

func TestMigrateKeepsNewerLiteLLMImport(t *testing.T) {
	pool := databasetest.NewEmptyPool(t)
	configuration := migrateConfiguration(t, pool)
	logger := logging.New(io.Discard, slog.LevelDebug)
	if err := Migrate(t.Context(), configuration, logger); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	refreshed := pricing.LiteLLMSnapshot{Content: []byte(refreshedLiteLLMSnapshot), FetchedAt: clock.System{}.Now()}
	if _, err := pricing.ImportLiteLLM(t.Context(), pool, refreshed, clock.System{}, logger); err != nil {
		t.Fatalf("import refreshed snapshot: %v", err)
	}

	if err := Migrate(t.Context(), configuration, logger); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	want := map[string]int64{"gpt-4o": 2_000_000_000, "refresh-example": 1_000_000_000}
	if diff := cmp.Diff(want, openInputTokenPrices(t, pool)); diff != "" {
		t.Errorf("open openai input token prices mismatch (-want +got):\n%s", diff)
	}
}

func migrateConfiguration(t *testing.T, pool *pgxpool.Pool) config.Config {
	t.Helper()
	databaseURL, err := url.Parse(pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	return config.Config{DatabaseURL: databaseURL, DatabaseMaximumConnections: testPoolConnections}
}

func openInputTokenPrices(t *testing.T, pool *pgxpool.Pool) map[string]int64 {
	t.Helper()
	rows, err := pool.Query(t.Context(), `SELECT model, unit_price_nanos FROM pricing_rules
		WHERE source = 'litellm' AND provider = 'openai' AND model IN ('gpt-4o', 'refresh-example')
			AND meter = 'input_tokens' AND effective_to IS NULL AND status = 'active'`)
	if err != nil {
		t.Fatalf("select open input token prices: %v", err)
	}
	prices, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (modelPrice, error) {
		var price modelPrice
		return price, row.Scan(&price.model, &price.nanos)
	})
	if err != nil {
		t.Fatalf("collect open input token prices: %v", err)
	}
	byModel := make(map[string]int64, len(prices))
	for _, price := range prices {
		byModel[price.model] = price.nanos
	}
	return byModel
}

func assertCatalogImportedTwice(t *testing.T, pool *pgxpool.Pool, logs *lockedBuffer) {
	t.Helper()
	imports := logs.records(t, logging.PricingCatalogImported)
	if len(imports) != 3 {
		t.Fatalf("logged pricing.catalog_imported %d times, want 3", len(imports))
	}
	for index, want := range []string{"curated", "litellm", "curated"} {
		if imports[index]["source"] != want {
			t.Errorf("import %d source = %v, want %s", index, imports[index]["source"], want)
		}
	}
	for _, count := range []string{"created", "changed", "updated", "deprecated"} {
		if imports[2][count] != float64(0) {
			t.Errorf("second curated import %s = %v, want 0", count, imports[2][count])
		}
	}
	skipped := logs.records(t, logging.PricingCatalogImportSkipped)
	if len(skipped) != 1 || skipped[0]["source"] != "litellm" {
		t.Errorf("pricing.catalog_import_skipped records = %v, want one for the second litellm import", skipped)
	}
	var curatedRules, liteLLMRules, aliases int
	err := pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM pricing_rules WHERE source = 'curated' AND status = 'active'),
		(SELECT count(*) FROM pricing_rules WHERE source = 'litellm' AND status = 'active'),
		(SELECT count(*) FROM provider_model_aliases)`).Scan(&curatedRules, &liteLLMRules, &aliases)
	if err != nil {
		t.Fatalf("count imported catalog rows: %v", err)
	}
	if curatedRules == 0 || liteLLMRules <= minimumLiteLLMRules || aliases == 0 {
		t.Errorf("active curated rules %d, litellm rules %d, aliases %d, want curated and aliases above 0 and litellm above %d",
			curatedRules, liteLLMRules, aliases, minimumLiteLLMRules)
	}
}

func newTestApp(t *testing.T, role Role) (*App, *lockedBuffer) {
	t.Helper()
	return openTestApp(t, testConfiguration(t), role)
}

func openTestApp(t *testing.T, configuration config.Config, role Role) (*App, *lockedBuffer) {
	t.Helper()
	logs := &lockedBuffer{}
	application, err := New(t.Context(), configuration, role, logging.New(logs, slog.LevelDebug))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		if err := application.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})
	return application, logs
}

func testConfiguration(t *testing.T) config.Config {
	t.Helper()
	databaseURL, err := url.Parse(databasetest.NewPool(t).Config().ConnString())
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	value := os.Getenv(testRedisURLVariable)
	if value == "" {
		t.Fatalf("%s is not set, run the tests with make test-go", testRedisURLVariable)
	}
	redisURL, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse %s: %v", testRedisURLVariable, err)
	}
	publicURL, err := url.Parse(testPublicURL)
	if err != nil {
		t.Fatalf("parse public URL: %v", err)
	}
	return config.Config{
		DatabaseURL:                databaseURL,
		DatabaseMaximumConnections: testPoolConnections,
		RedisURL:                   redisURL,
		RedisKeyPrefix:             "test:" + rand.Text() + ":",
		PublicURL:                  publicURL,
		DecisionRetentionDays:      testDecisionRetentionDays,
	}
}

func startServer(t *testing.T, application *App, logs *lockedBuffer) (string, func()) {
	t.Helper()
	baseURL, cancel, served := serveInBackground(t, application, logs)
	stop := sync.OnceFunc(func() {
		cancel()
		if err := receive(t, served); err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	t.Cleanup(stop)
	return baseURL, stop
}

func serveInBackground(t *testing.T, application *App, logs *lockedBuffer) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", loopbackAddress)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	served := make(chan error, 1)
	go func() {
		served <- application.Serve(ctx, listener)
	}()
	waitFor(t, "server.started logged", func() bool {
		return len(logs.records(t, logging.ServerStarted)) == 1
	})
	baseURL := "http://" + listener.Addr().String()
	waitFor(t, "readyz answers 200", func() bool {
		status, _, _ := get(t, baseURL+"/readyz")
		return status == http.StatusOK
	})
	return baseURL, cancel, served
}

func get(t *testing.T, target string) (status int, contentType string, body string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("create request %s: %v", target, err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body of %s: %v", target, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close body of %s: %v", target, err)
	}
	return response.StatusCode, response.Header.Get("Content-Type"), string(contents)
}

func receive[Value any](t *testing.T, values <-chan Value) Value {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(waitTimeout):
	}
	t.Fatalf("no value within %s", waitTimeout)
	return *new(Value)
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

func (buffer *lockedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.contents.Write(data)
}

func (buffer *lockedBuffer) records(t *testing.T, event logging.Event) []map[string]any {
	t.Helper()
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	var matching []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(buffer.contents.Bytes()))
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("parse log line %q: %v", scanner.Text(), err)
		}
		if record["msg"] == string(event) {
			matching = append(matching, record)
		}
	}
	return matching
}
