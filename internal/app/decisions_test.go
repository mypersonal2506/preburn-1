package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/preburn/preburn/catalog"
	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/installation"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
	"github.com/preburn/preburn/internal/signals"
)

const (
	checkPath             = "/api/v1/check"
	reportPath            = "/api/v1/report"
	adminCustomerPath     = "/api/v1/customers/"
	decisionStreamPath    = "/api/v1/dashboard/decisions/stream?environment=test"
	countersReadyKeyName  = "counters_ready"
	testCheckBody         = `{"customer_id": "acme", "feature": "text_to_video", "provider": "fal_ai", "model": "fal-ai/kling-video/v2.5-turbo/pro/text-to-video", "usage_estimate": {"output_seconds": "5"}}`
	testReportBodyFormat  = `{"decision_source": "server", "decision_id": %q, "usage": {"output_seconds": "5"}}`
	testPolicyDocument    = `{"name": "Stop heavy video", "level": "everyone", "when": {"all": [{"signal": "pace", "operator": "gt", "value": "1.5"}]}, "action": {"outcome": "deny"}, "enforcement": "soft", "on_unreachable": "allow", "on_uncosted": "allow"}`
	klingMinimumCharge    = "0.350000000"
	testAllowanceNanos    = money.Amount(10_000_000_000)
	streamDataPrefix      = "data: "
	streamEventNamePrefix = "event: "
	testAllowance         = "10.00"
)

var firstSliceOperations = []string{
	"DELETE /api/v1/api-keys/{api_key_id}",
	"DELETE /api/v1/members/{member_id}",
	"DELETE /api/v1/pricing/overrides/{pricing_override_id}",
	"GET /api/v1/api-keys",
	"GET /api/v1/auth/me",
	"GET /api/v1/customers",
	"GET /api/v1/customers/{external_id}",
	"GET /api/v1/dashboard/customers",
	"GET /api/v1/dashboard/customers/{customer_id}",
	"GET /api/v1/dashboard/decisions",
	"GET /api/v1/dashboard/decisions/stream",
	"GET /api/v1/dashboard/decisions/{decision_id}",
	"GET /api/v1/dashboard/events",
	"GET /api/v1/dashboard/features",
	"GET /api/v1/dashboard/onboarding",
	"GET /api/v1/dashboard/overview",
	"GET /api/v1/members",
	"GET /api/v1/plans",
	"GET /api/v1/plans/{plan_id}",
	"GET /api/v1/policies",
	"GET /api/v1/policies/parameter-mappings",
	"GET /api/v1/policies/{policy_id}",
	"GET /api/v1/pricing/meters",
	"GET /api/v1/pricing/model-attributes",
	"GET /api/v1/pricing/models",
	"GET /api/v1/pricing/overrides",
	"GET /api/v1/pricing/overrides/{pricing_override_id}",
	"GET /api/v1/pricing/uncosted",
	"GET /api/v1/revenue",
	"GET /api/v1/settings",
	"GET /api/v1/setup/status",
	"PATCH /api/v1/auth/me",
	"PATCH /api/v1/plans/{plan_id}",
	"PATCH /api/v1/policies/{policy_id}",
	"PATCH /api/v1/pricing/overrides/{pricing_override_id}",
	"PATCH /api/v1/settings",
	"POST /api/v1/api-keys",
	"POST /api/v1/auth/links/consume",
	"POST /api/v1/auth/links/inspect",
	"POST /api/v1/auth/login",
	"POST /api/v1/auth/logout",
	"POST /api/v1/check",
	"POST /api/v1/members",
	"POST /api/v1/members/{member_id}/reset-link",
	"POST /api/v1/plans",
	"POST /api/v1/policies",
	"POST /api/v1/policies/preview",
	"POST /api/v1/pricing/overrides",
	"POST /api/v1/pricing/quote",
	"POST /api/v1/release",
	"POST /api/v1/report",
	"POST /api/v1/reports",
	"POST /api/v1/revenue",
	"POST /api/v1/setup",
	"PUT /api/v1/customers/{external_id}",
}

func TestWriteOpenAPIDocumentsEveryOperationOfTheFirstSlice(t *testing.T) {
	var output bytes.Buffer
	if err := WriteOpenAPI(&output); err != nil {
		t.Fatalf("write openapi: %v", err)
	}
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("decode openapi document: %v", err)
	}

	var operations []string
	for path, methods := range document.Paths {
		for method := range methods {
			operations = append(operations, strings.ToUpper(method)+" "+path)
		}
	}
	slices.Sort(operations)

	if diff := cmp.Diff(firstSliceOperations, operations); diff != "" {
		t.Errorf("operations mismatch (-want +got):\n%s", diff)
	}
}

func TestServeRunsTheDecisionLoop(t *testing.T) {
	application, logs := newTestApp(t, RoleAPI)
	importCuratedCatalog(t, application)
	baseURL, cancel, served := serveInBackground(t, application, logs)
	runtimeSecret := createKey(t, application, apikeys.ScopeRuntime)
	adminSecret := createKey(t, application, apikeys.ScopeAdmin)
	events := openDecisionStream(t, baseURL, startMemberSession(t, application))

	checked := sendJSON(t, http.MethodPost, baseURL+checkPath, runtimeSecret, testCheckBody, http.StatusOK)
	if checked["outcome"] != "allow" || checked["estimated_cost"] != klingMinimumCharge {
		t.Fatalf("check = %v, want allow with estimated cost %s", checked, klingMinimumCharge)
	}
	decisionID := checked["decision_id"].(string)
	if event := readDecisionEvent(t, events); event["id"] != decisionID {
		t.Errorf("streamed decision = %v, want %s", event["id"], decisionID)
	}
	reported := sendJSON(t, http.MethodPost, baseURL+reportPath, runtimeSecret, fmt.Sprintf(testReportBodyFormat, decisionID), http.StatusAccepted)
	if reported["cost"] != klingMinimumCharge || reported["duplicate"] != false {
		t.Errorf("report = %v, want cost %s and no duplicate", reported, klingMinimumCharge)
	}
	customer := sendJSON(t, http.MethodGet, baseURL+adminCustomerPath+testExternalID, adminSecret, "", http.StatusOK)
	if costToDate := customer["signals"].(map[string]any)["cost_to_date"]; costToDate != klingMinimumCharge {
		t.Errorf("customer cost_to_date = %v, want %s", costToDate, klingMinimumCharge)
	}

	cancel()
	stopServerWithOpenStream(t, application, served)
}

func TestServeFollowsPolicyAndCustomerStateInvalidations(t *testing.T) {
	application, logs := newTestApp(t, RoleAPI)
	startServer(t, application, logs)
	waitForInvalidationSubscriber(t, application)
	customerID := customerIDOf(t, application)
	assertActivePolicies(t, application, 0)
	assertAllowance(t, application, customerID, 0)

	changePoliciesAndDefaultPlanInAnotherProcess(t, application)

	waitFor(t, "policy cache sees the new policy", func() bool {
		return len(activePolicies(t, application)) == 1
	})
	waitFor(t, "customer state cache sees the new plan", func() bool {
		return customerState(t, application, customerID).Allowance == testAllowanceNanos
	})
}

func TestReadinessWaitsForTheCountersMarker(t *testing.T) {
	application, _ := newTestApp(t, RoleAPI)
	handler := application.handler()

	before := readiness(t, handler)
	if err := application.CounterBootstrap.EnsureCountersReady(t.Context()); err != nil {
		t.Fatalf("ensure counters ready: %v", err)
	}
	after := readiness(t, handler)

	if diff := cmp.Diff(map[string]any{"status": "not_ready", "failing_checks": []any{"counters"}}, before); diff != "" {
		t.Errorf("readiness before the marker mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(map[string]any{"status": "ready", "failing_checks": []any{}}, after); diff != "" {
		t.Errorf("readiness after the marker mismatch (-want +got):\n%s", diff)
	}
}

func TestRefusedCheckRebuildsTheCounters(t *testing.T) {
	application, logs := newTestApp(t, RoleAPI)
	importCuratedCatalog(t, application)
	baseURL, _ := startServer(t, application, logs)
	runtimeSecret := createKey(t, application, apikeys.ScopeRuntime)
	deleteCountersMarker(t, application)

	refused := sendJSON(t, http.MethodPost, baseURL+checkPath, runtimeSecret, testCheckBody, http.StatusServiceUnavailable)

	if refused["code"] != "counters_unavailable" {
		t.Errorf("refused check = %v, want counters_unavailable", refused)
	}
	waitFor(t, "readyz answers 200 after the refused check", func() bool {
		status, _, _ := get(t, baseURL+"/readyz")
		return status == http.StatusOK
	})
	sendJSON(t, http.MethodPost, baseURL+checkPath, runtimeSecret, testCheckBody, http.StatusOK)
}

func TestResubscribedRequestsTheCounterRebuildWithoutWaitingForIt(t *testing.T) {
	application, logs := newTestApp(t, RoleAPI)
	startServer(t, application, logs)
	_, resubscribed := application.invalidationHandlers()
	releaseLock, err := database.AcquireAdvisoryLock(t.Context(), application.Pool, "counters_rebuild")
	if err != nil {
		t.Fatalf("acquire the rebuild lock: %v", err)
	}
	releaseLock = sync.OnceFunc(releaseLock)
	defer releaseLock()
	deleteCountersMarker(t, application)
	returned := make(chan struct{})

	go func() {
		resubscribed()
		close(returned)
	}()

	receive(t, returned)
	if countersReady(t, application) {
		t.Fatal("counters ready while another process holds the rebuild lock")
	}
	releaseLock()
	waitFor(t, "counters ready once the rebuild lock is free", func() bool {
		return countersReady(t, application)
	})
}

func TestWorkRebuildsCountersAtStartAndBeforeEachReconcile(t *testing.T) {
	application, logs := newTestApp(t, RoleWorker)
	startWorker(t, application, logs)

	if ready := countersReady(t, application); !ready {
		t.Fatal("counters not ready once the worker started")
	}
	deleteCountersMarker(t, application)
	job := insertJob(t, application, decisions.ReconcileArgs{})
	waitForJobState(t, application, job.ID, rivertype.JobStateCompleted)

	if ready := countersReady(t, application); !ready {
		t.Error("counters not ready after a counters_reconcile job")
	}
}

func TestWorkWorksAndTracesEveryDecisionJob(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previousProvider) })
	application, logs := newTestApp(t, RoleWorker)
	startWorker(t, application, logs)
	jobs := []struct {
		arguments river.JobArgs
		state     rivertype.JobState
	}{
		{arguments: decisions.ExpiryArgs{}, state: rivertype.JobStateCompleted},
		{arguments: decisions.ReconcileArgs{}, state: rivertype.JobStateCompleted},
		{arguments: decisions.RetentionArgs{}, state: rivertype.JobStateCompleted},
		{arguments: ledger.UsageEstimatesRefreshArgs{}, state: rivertype.JobStateCompleted},
		{arguments: ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, identifiers.New(), time.Now()), state: rivertype.JobStateCancelled},
		{arguments: pricing.UncostedRerateArgs{Environment: httpapi.EnvironmentTest}, state: rivertype.JobStateCompleted},
	}

	for _, job := range jobs {
		inserted := insertJob(t, application, job.arguments)
		waitForJobState(t, application, inserted.ID, job.state)
	}

	var traced []string
	for _, span := range spans.Ended() {
		traced = append(traced, span.Name())
	}
	for _, job := range jobs {
		if !slices.Contains(traced, job.arguments.Kind()) {
			t.Errorf("spans %v, want one named %s", traced, job.arguments.Kind())
		}
	}
}

func importCuratedCatalog(t *testing.T, application *App) {
	t.Helper()
	files, err := catalogfiles.Load(catalog.Files)
	if err != nil {
		t.Fatalf("load catalog files: %v", err)
	}
	if _, err := pricing.ImportCurated(t.Context(), application.Pool, files, application.Clock, application.Logger); err != nil {
		t.Fatalf("import curated catalog: %v", err)
	}
}

func createKey(t *testing.T, application *App, scope apikeys.Scope) string {
	t.Helper()
	_, secret, err := application.APIKeys.Create(t.Context(), httpapi.EnvironmentTest, testKeyName+" "+string(scope), scope, nil)
	if err != nil {
		t.Fatalf("create %s key: %v", scope, err)
	}
	return secret
}

func startMemberSession(t *testing.T, application *App) string {
	t.Helper()
	member, err := application.Installation.CreateAdmin(t.Context(), testEmail, testDisplayName, testPassword)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	sessionToken, err := application.Members.Sessions().Start(t.Context(), member.ID, sessionUserAgent, netip.MustParseAddr("127.0.0.1"))
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	return sessionToken
}

func openDecisionStream(t *testing.T, baseURL string, sessionToken string) *bufio.Reader {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+decisionStreamPath, nil)
	if err != nil {
		t.Fatalf("create stream request: %v", err)
	}
	request.AddCookie(&http.Cookie{Name: members.SessionCookieName, Value: sessionToken}) //nolint:gosec // G124: a request cookie carries no attributes.
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("open decision stream: %v", err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close decision stream: %v", err)
		}
	})
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("stream status = %d, want 200, body %s", response.StatusCode, body)
	}
	return bufio.NewReader(response.Body)
}

func readDecisionEvent(t *testing.T, events *bufio.Reader) map[string]any {
	t.Helper()
	decisionEvent := false
	for {
		line, err := events.ReadString('\n')
		if err != nil {
			t.Fatalf("read decision stream: %v", err)
		}
		line = strings.TrimSuffix(line, "\n")
		if name, isName := strings.CutPrefix(line, streamEventNamePrefix); isName {
			decisionEvent = name == "decision"
		}
		if data, isData := strings.CutPrefix(line, streamDataPrefix); isData && decisionEvent {
			var event map[string]any
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				t.Fatalf("decode decision event %q: %v", data, err)
			}
			return event
		}
	}
}

func stopServerWithOpenStream(t *testing.T, application *App, served <-chan error) {
	t.Helper()
	writer := decisions.NewStreamWriter(application.Cache, application.Logger)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	deadline := time.After(waitTimeout)
	for {
		select {
		case err := <-served:
			if err != nil {
				t.Errorf("serve with an open decision stream: %v", err)
			}
			return
		case <-ticker.C:
			writer.Append(t.Context(), httpapi.EnvironmentTest, decisions.StreamEntry{
				DecisionID:         identifiers.New(),
				CreatedAt:          time.Now(),
				CustomerID:         identifiers.New(),
				CustomerExternalID: testExternalID,
				Feature:            "text_to_video",
				RequestedModel:     "wake-model",
				Model:              "wake-model",
				Outcome:            policies.OutcomeAllow,
				Reason:             policies.ReasonNoPolicyMatched,
			})
		case <-deadline:
			t.Fatalf("serve did not stop within %s with an open decision stream", waitTimeout)
		}
	}
}

func sendJSON(t *testing.T, method string, target string, secret string, body string, status int) map[string]any {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if err != nil {
		t.Fatalf("create request %s %s: %v", method, target, err)
	}
	request.Header.Set(authorizationHeader, "Bearer "+secret)
	if body != "" {
		request.Header.Set(contentTypeHeader, jsonContentType)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body of %s %s: %v", method, target, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body of %s %s: %v", method, target, err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s status = %d, want %d, body %s", method, target, response.StatusCode, status, contents)
	}
	var decoded map[string]any
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("decode body %s: %v", contents, err)
	}
	return decoded
}

func customerIDOf(t *testing.T, application *App) uuid.UUID {
	t.Helper()
	customer, err := application.Customers.Cache().Ensure(t.Context(), httpapi.EnvironmentTest, testExternalID)
	if err != nil {
		t.Fatalf("ensure customer: %v", err)
	}
	return customer.ID
}

func changePoliciesAndDefaultPlanInAnotherProcess(t *testing.T, application *App) {
	t.Helper()
	otherPolicies := policies.NewService(application.Pool, application.Cache, catalogfiles.Catalog{}, application.Clock)
	if _, err := otherPolicies.Create(t.Context(), httpapi.EnvironmentTest, []byte(testPolicyDocument)); err != nil {
		t.Fatalf("create policy in another process: %v", err)
	}
	allowance := testAllowance
	plan, err := plans.NewService(application.Pool, application.Cache, application.Clock).Create(t.Context(), httpapi.EnvironmentTest, plans.CreateInput{Name: "Pro", Mode: plans.ModeFixedAllowance, Allowance: &allowance})
	if err != nil {
		t.Fatalf("create plan in another process: %v", err)
	}
	otherSettings := installation.NewSettingsService(application.Pool, application.Cache, application.Clock)
	if _, err := otherSettings.Update(t.Context(), httpapi.EnvironmentTest, installation.SettingsUpdate{ReplaceDefaultPlan: true, DefaultPlanID: &plan.ID}); err != nil {
		t.Fatalf("make the plan the default in another process: %v", err)
	}
}

func assertActivePolicies(t *testing.T, application *App, want int) {
	t.Helper()
	if active := activePolicies(t, application); len(active) != want {
		t.Errorf("active policies = %d, want %d", len(active), want)
	}
}

func assertAllowance(t *testing.T, application *App, customerID uuid.UUID, want money.Amount) {
	t.Helper()
	if allowance := customerState(t, application, customerID).Allowance; allowance != want {
		t.Errorf("customer allowance = %d, want %d", allowance, want)
	}
}

func activePolicies(t *testing.T, application *App) []policies.Policy {
	t.Helper()
	active, err := application.Policies.Cache().Active(t.Context(), httpapi.EnvironmentTest)
	if err != nil {
		t.Fatalf("active policies: %v", err)
	}
	return active
}

func customerState(t *testing.T, application *App, customerID uuid.UUID) signals.CustomerState {
	t.Helper()
	state, err := application.CustomerStates.Load(t.Context(), httpapi.EnvironmentTest, customerID, application.Clock.Now())
	if err != nil {
		t.Fatalf("load customer state: %v", err)
	}
	return state
}

func countersReady(t *testing.T, application *App) bool {
	t.Helper()
	ready, err := application.Counters.IsReady(t.Context())
	if err != nil {
		t.Fatalf("read counters marker: %v", err)
	}
	return ready
}

func deleteCountersMarker(t *testing.T, application *App) {
	t.Helper()
	if err := application.Cache.Redis().Del(t.Context(), application.Cache.Key(countersReadyKeyName)).Err(); err != nil {
		t.Fatalf("delete the counters marker: %v", err)
	}
}

func readiness(t *testing.T, handler http.Handler) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness %q: %v", recorder.Body.String(), err)
	}
	return body
}

func startWorker(t *testing.T, application *App, logs *lockedBuffer) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() {
		stopped <- application.Work(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		if err := receive(t, stopped); err != nil {
			t.Errorf("work: %v", err)
		}
	})
	waitFor(t, "worker.started logged", func() bool {
		return len(logs.records(t, logging.WorkerStarted)) == 1
	})
}

func insertJob(t *testing.T, application *App, arguments river.JobArgs) *rivertype.JobRow {
	t.Helper()
	inserted, err := application.Jobs.Insert(t.Context(), arguments, nil)
	if err != nil {
		t.Fatalf("insert %s job: %v", arguments.Kind(), err)
	}
	return inserted.Job
}

func waitForJobState(t *testing.T, application *App, jobID int64, state rivertype.JobState) {
	t.Helper()
	waitFor(t, "job reaches "+string(state), func() bool {
		job, err := application.Jobs.JobGet(t.Context(), jobID)
		if err != nil {
			t.Fatalf("get job %d: %v", jobID, err)
		}
		return job.State == state
	})
}
