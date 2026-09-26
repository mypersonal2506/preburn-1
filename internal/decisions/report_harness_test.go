package decisions_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/preburn/preburn/catalog"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/jobs/jobstest"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	reportPath                = "/api/v1/report"
	reportBatchPath           = "/api/v1/reports"
	releasePath               = "/api/v1/release"
	reportsMetricName         = "preburn_reports_total"
	droppedReportsMetricName  = "preburn_dropped_reports_total"
	droppedReportsHeader      = "Preburn-Dropped-Reports"
	clientClosedRequestStatus = 499
	fallbackCustomer          = "fallback-customer"
	ledgerColumnsSelect       = `SELECT customer_id, customer_user_id, decision_id, idempotency_key, feature, provider, model,
		attributes, usage, cost_nanos, cost_status, decision_source, period_start, period_end, correction_of, occurred_at, created_at
		FROM ledger_entries WHERE ledger_entry_id = $1`
)

type reportHarness struct {
	*checkHarness
	reports *decisions.ReportService
	logs    *bytes.Buffer
	routes  http.Handler
}

type ledgerRow struct {
	CustomerID     uuid.UUID
	CustomerUserID *uuid.UUID
	DecisionID     *uuid.UUID
	IdempotencyKey string
	Feature        string
	Provider       string
	Model          string
	Attributes     map[string]any
	Usage          map[string]any
	CostNanos      *int64
	CostStatus     string
	DecisionSource string
	PeriodStart    time.Time
	PeriodEnd      time.Time
	CorrectionOf   *uuid.UUID
	OccurredAt     time.Time
	CreatedAt      time.Time
}

func newReportHarness(t *testing.T) *reportHarness {
	t.Helper()
	check := newCheckHarness(t)
	files, err := catalogfiles.Load(catalog.Files)
	if err != nil {
		t.Fatalf("load catalog files: %v", err)
	}
	logs := &bytes.Buffer{}
	logger := logging.New(logs, slog.LevelDebug)
	jobs := jobstest.NewInsertClient(t, check.pool)
	service, err := decisions.NewReportService(decisions.ReportDependencies{
		Pool:           check.pool,
		Jobs:           jobs,
		Customers:      check.customers,
		CustomerStates: check.states,
		RuleSets:       pricing.NewService(check.pool, check.cache, jobs, files, check.clock).RuleSets(),
		Policies:       check.policies,
		ModelAliases:   files.Aliases,
		Counters:       check.counters,
		Clock:          check.clock,
		Logger:         logger,
		Registry:       check.registry,
	})
	if err != nil {
		t.Fatalf("new report service: %v", err)
	}
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, checkAuthenticator{apiKeys: check.apiKeys.Authenticator()})
	decisions.RegisterReportRoutes(api, service)
	return &reportHarness{checkHarness: check, reports: service, logs: logs, routes: mux}
}

func (harness *reportHarness) report(t *testing.T, request decisions.ReportRequest) decisions.ReportResult {
	t.Helper()
	result, err := harness.reports.Report(t.Context(), httpapi.EnvironmentTest, request)
	if err != nil {
		t.Fatalf("report %+v: %v", request, err)
	}
	return result
}

func (harness *reportHarness) ledgerEntry(t *testing.T, result decisions.ReportResult) ledgerRow {
	t.Helper()
	ledgerEntryID := decodeIdentifier(t, identifiers.PrefixLedgerEntry, result.LedgerEntryID)
	var row ledgerRow
	var attributes, usage []byte
	err := harness.pool.QueryRow(t.Context(), ledgerColumnsSelect, ledgerEntryID).Scan(
		&row.CustomerID, &row.CustomerUserID, &row.DecisionID, &row.IdempotencyKey, &row.Feature, &row.Provider, &row.Model,
		&attributes, &usage, &row.CostNanos, &row.CostStatus, &row.DecisionSource, &row.PeriodStart, &row.PeriodEnd,
		&row.CorrectionOf, &row.OccurredAt, &row.CreatedAt,
	)
	if err != nil {
		t.Fatalf("select ledger entry %s: %v", result.LedgerEntryID, err)
	}
	if err := json.Unmarshal(attributes, &row.Attributes); err != nil {
		t.Fatalf("decode ledger attributes %s: %v", attributes, err)
	}
	if err := json.Unmarshal(usage, &row.Usage); err != nil {
		t.Fatalf("decode ledger usage %s: %v", usage, err)
	}
	row.PeriodStart = row.PeriodStart.UTC()
	row.PeriodEnd = row.PeriodEnd.UTC()
	row.OccurredAt = row.OccurredAt.UTC()
	row.CreatedAt = row.CreatedAt.UTC()
	return row
}

func (harness *reportHarness) ledgerEntryCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM ledger_entries").Scan(&count); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	return count
}

func (harness *reportHarness) decisionStatus(t *testing.T, decisionID string) (string, *time.Time) {
	t.Helper()
	var status string
	var settledAt *time.Time
	err := harness.pool.QueryRow(t.Context(), "SELECT status, settled_at FROM decisions WHERE decision_id = $1",
		decodeIdentifier(t, identifiers.PrefixDecision, decisionID)).Scan(&status, &settledAt)
	if err != nil {
		t.Fatalf("select decision %s: %v", decisionID, err)
	}
	return status, settledAt
}

func (harness *reportHarness) reservationStatus(t *testing.T, decisionID string) string {
	t.Helper()
	key := harness.counters.ReservationKey(decodeIdentifier(t, identifiers.PrefixDecision, decisionID))
	status, err := harness.cache.Redis().HGet(t.Context(), key, "status").Result()
	if err != nil {
		t.Fatalf("read reservation %s: %v", key, err)
	}
	return status
}

func (harness *reportHarness) expire(t *testing.T, decisionID string) {
	t.Helper()
	decoded := decodeIdentifier(t, identifiers.PrefixDecision, decisionID)
	if _, err := harness.pool.Exec(t.Context(), "UPDATE decisions SET status = 'expired' WHERE decision_id = $1", decoded); err != nil {
		t.Fatalf("mark decision %s expired: %v", decisionID, err)
	}
	result, err := harness.counters.Release(t.Context(), decoded, decisions.ReservationStatusExpired)
	if err != nil || !result.Applied {
		t.Fatalf("expire reservation %s = %+v, %v, want applied", decisionID, result, err)
	}
}

func (harness *reportHarness) send(t *testing.T, path string, secret string, droppedReports string, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(checkAuthorization, "Bearer "+secret)
	if droppedReports != "" {
		request.Header.Set(droppedReportsHeader, droppedReports)
	}
	recorder := httptest.NewRecorder()
	harness.routes.ServeHTTP(recorder, request)
	return recorder
}

func serverReport(decisionID string, seconds string) decisions.ReportRequest {
	return decisions.ReportRequest{
		DecisionSource: ledger.DecisionSourceServer,
		DecisionID:     decisionID,
		Usage:          map[string]string{outputSeconds: seconds},
	}
}

func fallbackReport(idempotencyKey string, seconds string) decisions.ReportRequest {
	return decisions.ReportRequest{
		DecisionSource: ledger.DecisionSourceFallback,
		IdempotencyKey: idempotencyKey,
		CustomerID:     fallbackCustomer,
		Feature:        checkFeature,
		Provider:       falProvider,
		Model:          veoModel,
		Usage:          map[string]string{outputSeconds: seconds},
	}
}

func problemLocations(t *testing.T, err error) []string {
	t.Helper()
	problem, isProblem := errors.AsType[*httpapi.Problem](err)
	if !isProblem || problem.Status != http.StatusUnprocessableEntity || problem.Code != "validation_failed" {
		t.Fatalf("error = %v, want a 422 validation_failed problem", err)
	}
	locations := make([]string, 0, len(problem.Errors))
	for _, problemError := range problem.Errors {
		locations = append(locations, problemError.Location)
	}
	return locations
}

func assertCodedError(t *testing.T, err error, status int, code string) {
	t.Helper()
	coded, isCoded := errors.AsType[httpapi.CodedError](err)
	if !isCoded || coded.ProblemStatus() != status || coded.ProblemCode() != code {
		t.Fatalf("error = %v, want %d %s", err, status, code)
	}
}
