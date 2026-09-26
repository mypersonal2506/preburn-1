package decisions_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
)

func TestReportRoutes(t *testing.T) {
	harness := newReportHarness(t)
	runtimeKey := harness.createRuntimeKey(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))

	reported := harness.send(t, reportPath, runtimeKey, "3",
		fmt.Sprintf(`{"decision_source": "server", "decision_id": %q, "usage": {"output_seconds": "6"}}`, checked.DecisionID))
	if reported.Code != http.StatusAccepted {
		t.Fatalf("report = %d %s, want 202", reported.Code, reported.Body.String())
	}
	var result decisions.ReportResult
	if err := json.Unmarshal(reported.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode report response %s: %v", reported.Body.String(), err)
	}
	if result.Cost == nil || *result.Cost != "0.900000000" || result.CostStatus != "costed" || result.Duplicate {
		t.Errorf("report response = %s, want cost 0.900000000 costed", reported.Body.String())
	}

	batch := harness.send(t, reportBatchPath, runtimeKey, "2", fmt.Sprintf(`{"reports": [
		{"decision_source": "fallback", "idempotency_key": %q, "customer_id": "acme", "feature": "text_to_video",
			"provider": "fal_ai", "model": "fal-ai/veo3.1/fast", "usage": {"output_seconds": "4"}},
		{"decision_source": "fallback", "usage": {"output_seconds": "4"}}
	]}`, identifiers.New()))
	if batch.Code != http.StatusAccepted {
		t.Fatalf("batch = %d %s, want 202", batch.Code, batch.Body.String())
	}
	var batchBody struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(batch.Body.Bytes(), &batchBody); err != nil {
		t.Fatalf("decode batch response %s: %v", batch.Body.String(), err)
	}
	if len(batchBody.Results) != 2 || batchBody.Results[0]["status"] != float64(202) || batchBody.Results[0]["error"] != nil ||
		batchBody.Results[1]["status"] != float64(422) || batchBody.Results[1]["result"] != nil {
		t.Errorf("batch response = %s, want a stored result and a 422 error", batch.Body.String())
	}

	released := harness.send(t, releasePath, runtimeKey, "", fmt.Sprintf(`{"decision_id": %q}`, checked.DecisionID))
	if released.Code != http.StatusOK {
		t.Fatalf("release = %d %s, want 200", released.Code, released.Body.String())
	}
	wantRelease := fmt.Sprintf(`{"decision_id":%q,"status":"settled"}`, checked.DecisionID)
	if diff := cmp.Diff(wantRelease, compactJSON(t, released.Body.Bytes())); diff != "" {
		t.Errorf("release response mismatch (-want +got):\n%s", diff)
	}

	day := harness.clock.Now()
	dropped, err := harness.cache.Redis().Get(t.Context(), decisions.DroppedReportsKey(harness.cache, httpapi.EnvironmentTest, day)).Result()
	if err != nil || dropped != "5" {
		t.Errorf("dropped reports of the day = %q, %v, want 5 from both headers", dropped, err)
	}
	expiry, err := harness.cache.Redis().TTL(t.Context(), decisions.DroppedReportsKey(harness.cache, httpapi.EnvironmentTest, day)).Result()
	if err != nil || expiry <= 7*24*time.Hour || expiry > 8*24*time.Hour {
		t.Errorf("dropped reports expire in %s, %v, want within 8 days of the day's start", expiry, err)
	}
	droppedMetric := labeledCounter(t, harness.registry, droppedReportsMetricName, map[string]string{"environment": "test"})
	if droppedMetric != 5 {
		t.Errorf("%s = %v, want 5", droppedReportsMetricName, droppedMetric)
	}
}

func TestReportRoutesRejectInvalidRequests(t *testing.T) {
	harness := newReportHarness(t)
	runtimeKey := harness.createRuntimeKey(t)
	tests := []struct {
		name           string
		path           string
		droppedReports string
		body           string
		status         int
		location       string
	}{
		{
			name:     "malformed decision id on release",
			path:     releasePath,
			body:     `{"decision_id": "dec_1"}`,
			status:   http.StatusUnprocessableEntity,
			location: "body.decision_id",
		},
		{
			name:     "unknown decision on release",
			path:     releasePath,
			body:     fmt.Sprintf(`{"decision_id": %q}`, identifiers.Encode(identifiers.PrefixDecision, identifiers.New())),
			status:   http.StatusNotFound,
			location: "",
		},
		{
			name:           "negative dropped reports",
			path:           reportPath,
			droppedReports: "-1",
			body:           `{"decision_source": "fallback", "usage": {}}`,
			status:         http.StatusUnprocessableEntity,
			location:       "header.Preburn-Dropped-Reports",
		},
		{
			name:     "invalid report",
			path:     reportPath,
			body:     `{"decision_source": "server", "usage": {}}`,
			status:   http.StatusUnprocessableEntity,
			location: "body.decision_id",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.send(t, test.path, runtimeKey, test.droppedReports, test.body)

			if recorder.Code != test.status {
				t.Fatalf("status = %d %s, want %d", recorder.Code, recorder.Body.String(), test.status)
			}
			var problem httpapi.Problem
			if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode problem %s: %v", recorder.Body.String(), err)
			}
			if test.location != "" && (len(problem.Errors) != 1 || problem.Errors[0].Location != test.location) {
				t.Errorf("problem = %s, want one error at %s", recorder.Body.String(), test.location)
			}
		})
	}
}

func TestReportBatchOfADisconnectedClientStopsWithOneInfoEvent(t *testing.T) {
	harness := newReportHarness(t)
	runtimeKey := harness.createRuntimeKey(t)
	if primed := harness.send(t, reportBatchPath, runtimeKey, "", fallbackBatchBody(t, 1)); primed.Code != http.StatusAccepted {
		t.Fatalf("batch that caches the key = %d %s, want 202", primed.Code, primed.Body.String())
	}
	harness.logs.Reset()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, reportBatchPath, strings.NewReader(fallbackBatchBody(t, 3)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(checkAuthorization, "Bearer "+runtimeKey)
	recorder := httptest.NewRecorder()

	harness.routes.ServeHTTP(recorder, request)

	if recorder.Code != clientClosedRequestStatus {
		t.Errorf("status = %d %s, want 499", recorder.Code, recorder.Body.String())
	}
	logLines := strings.Split(strings.TrimSpace(harness.logs.String()), "\n")
	if len(logLines) != 1 || !strings.Contains(logLines[0], `"level":"INFO","msg":"http.request_canceled"`) {
		t.Errorf("logs = %s, want one info http.request_canceled line", harness.logs.String())
	}
	if count := harness.ledgerEntryCount(t); count != 1 {
		t.Errorf("ledger entries = %d, want only the entry of the batch before", count)
	}
}

func fallbackBatchBody(t *testing.T, count int) string {
	t.Helper()
	reports := make([]decisions.ReportRequest, count)
	for index := range reports {
		reports[index] = fallbackReport(identifiers.New().String(), "4")
	}
	body, err := json.Marshal(map[string][]decisions.ReportRequest{"reports": reports})
	if err != nil {
		t.Fatalf("encode batch: %v", err)
	}
	return string(body)
}

func compactJSON(t *testing.T, encoded []byte) string {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode %s: %v", encoded, err)
	}
	compacted, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("encode %v: %v", decoded, err)
	}
	return string(compacted)
}
