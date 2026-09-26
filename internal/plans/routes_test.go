package plans_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
)

func TestCreateRouteInEachMode(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	tests := []struct {
		name string
		body string
		want map[string]any
	}{
		{
			name: "margin target",
			body: `{"name":"Creator","mode":"margin_target","target_margin":"0.40"}`,
			want: map[string]any{
				"name":           "Creator",
				"mode":           "margin_target",
				"target_margin":  "0.4000",
				"allowance":      nil,
				"hold_times":     map[string]any{},
				"status":         "active",
				"customer_count": float64(0),
				"created_at":     testStart.Format(time.RFC3339),
			},
		},
		{
			name: "fixed allowance",
			body: `{"name":"Free","mode":"fixed_allowance","allowance":"2.00","hold_times":{"text_to_video":900}}`,
			want: map[string]any{
				"name":           "Free",
				"mode":           "fixed_allowance",
				"target_margin":  "0.0000",
				"allowance":      "2.000000000",
				"hold_times":     map[string]any{"text_to_video": float64(900)},
				"status":         "active",
				"customer_count": float64(0),
				"created_at":     testStart.Format(time.RFC3339),
			},
		},
		{
			name: "fixed allowance with a stored target margin",
			body: `{"name":"Team","mode":"fixed_allowance","allowance":"0","target_margin":"0.5"}`,
			want: map[string]any{
				"name":           "Team",
				"mode":           "fixed_allowance",
				"target_margin":  "0.5000",
				"allowance":      "0.000000000",
				"hold_times":     map[string]any{},
				"status":         "active",
				"customer_count": float64(0),
				"created_at":     testStart.Format(time.RFC3339),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, http.MethodPost, plansPath, httpapi.EnvironmentTest, test.body)

			assertStatus(t, recorder, http.StatusCreated)
			body := decodeBody(t, recorder)
			planID, isString := body["id"].(string)
			if !isString {
				t.Fatalf("id = %v, want a plan id", body["id"])
			}
			if _, err := identifiers.Decode(identifiers.PrefixPlan, planID); err != nil {
				t.Errorf("id %s is not a plan id: %v", planID, err)
			}
			test.want["id"] = planID
			if diff := cmp.Diff(test.want, body); diff != "" {
				t.Errorf("body mismatch (-want +got):\n%s", diff)
			}
			fetched := harness.memberRequest(t, http.MethodGet, plansPath+"/"+planID, httpapi.EnvironmentTest, "")
			assertStatus(t, fetched, http.StatusOK)
			if diff := cmp.Diff(test.want, decodeBody(t, fetched)); diff != "" {
				t.Errorf("fetched body mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCreateRouteRejectsDuplicateName(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	body := `{"name":"Creator","mode":"margin_target","target_margin":"0.40"}`
	assertStatus(t, harness.memberRequest(t, http.MethodPost, plansPath, httpapi.EnvironmentTest, body), http.StatusCreated)

	recorder := harness.memberRequest(t, http.MethodPost, plansPath, httpapi.EnvironmentTest, body)

	assertProblem(t, recorder, http.StatusConflict, "plan_name_taken")
	assertStatus(t, harness.memberRequest(t, http.MethodPost, plansPath, httpapi.EnvironmentLive, body), http.StatusCreated)
}

func TestCreateRouteValidatesBody(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	tests := []struct {
		name      string
		body      string
		locations []string
	}{
		{name: "target margin of one", body: `{"name":"Plan","mode":"margin_target","target_margin":"1"}`, locations: []string{"body.target_margin"}},
		{name: "target margin above maximum", body: `{"name":"Plan","mode":"margin_target","target_margin":"1.0000"}`, locations: []string{"body.target_margin"}},
		{name: "target margin with five decimals", body: `{"name":"Plan","mode":"margin_target","target_margin":"0.99999"}`, locations: []string{"body.target_margin"}},
		{name: "negative target margin", body: `{"name":"Plan","mode":"margin_target","target_margin":"-0.1"}`, locations: []string{"body.target_margin"}},
		{name: "target margin that is not a number", body: `{"name":"Plan","mode":"margin_target","target_margin":"forty"}`, locations: []string{"body.target_margin"}},
		{name: "margin target without target margin", body: `{"name":"Plan","mode":"margin_target"}`, locations: []string{"body.target_margin"}},
		{name: "margin target with allowance", body: `{"name":"Plan","mode":"margin_target","target_margin":"0.4","allowance":"2"}`, locations: []string{"body.allowance"}},
		{name: "fixed allowance without allowance", body: `{"name":"Plan","mode":"fixed_allowance"}`, locations: []string{"body.allowance"}},
		{name: "negative allowance", body: `{"name":"Plan","mode":"fixed_allowance","allowance":"-1"}`, locations: []string{"body.allowance"}},
		{name: "empty name", body: `{"name":"","mode":"fixed_allowance","allowance":"1"}`, locations: []string{"body.name"}},
		{name: "long name", body: `{"name":"` + strings.Repeat("n", 81) + `","mode":"fixed_allowance","allowance":"1"}`, locations: []string{"body.name"}},
		{name: "unknown mode", body: `{"name":"Plan","mode":"free"}`, locations: []string{"body.mode"}},
		{name: "short hold time", body: `{"name":"Plan","mode":"fixed_allowance","allowance":"1","hold_times":{"text_to_video":29}}`, locations: []string{"body.hold_times.text_to_video"}},
		{name: "long hold time", body: `{"name":"Plan","mode":"fixed_allowance","allowance":"1","hold_times":{"text_to_video":86401}}`, locations: []string{"body.hold_times.text_to_video"}},
		{name: "hold time feature outside the pattern", body: `{"name":"Plan","mode":"fixed_allowance","allowance":"1","hold_times":{"Text to video":600}}`, locations: []string{"body.hold_times"}},
		{name: "hold time that is not an integer", body: `{"name":"Plan","mode":"fixed_allowance","allowance":"1","hold_times":{"<b>secret-value</b>":"a"}}`, locations: []string{"body.hold_times"}},
		{name: "NUL in a hold time feature", body: `{"name":"Plan","mode":"fixed_allowance","allowance":"1","hold_times":{"chat\u0000":600}}`, locations: []string{"body.hold_times"}},
		{
			name:      "every field",
			body:      `{"name":"","mode":"margin_target","target_margin":"2","allowance":"1","hold_times":{"chat":10}}`,
			locations: []string{"body.name", "body.target_margin", "body.allowance", "body.hold_times.chat"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, http.MethodPost, plansPath, httpapi.EnvironmentTest, test.body)

			assertLocations(t, assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed"), test.locations...)
		})
	}
	listed := decodeBody(t, harness.memberRequest(t, http.MethodGet, plansPath, httpapi.EnvironmentTest, ""))
	if items := listed["items"]; !cmp.Equal(items, []any{}) {
		t.Errorf("plans after rejected creates = %v, want none", items)
	}
}

func TestListRouteReturnsCustomerCounts(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	defaultPlan := harness.createPlan(t, httpapi.EnvironmentTest, "Free")
	creatorPlan := harness.createPlan(t, httpapi.EnvironmentTest, "Creator")
	emptyPlan := harness.createPlan(t, httpapi.EnvironmentTest, "Studio")
	livePlan := harness.createPlan(t, httpapi.EnvironmentLive, "Creator")
	harness.setDefaultPlan(t, httpapi.EnvironmentTest, defaultPlan.ID)
	harness.insertCustomer(t, httpapi.EnvironmentTest, "assigned-free", &defaultPlan.ID)
	harness.insertCustomer(t, httpapi.EnvironmentTest, "unassigned-1", nil)
	harness.insertCustomer(t, httpapi.EnvironmentTest, "unassigned-2", nil)
	harness.insertCustomer(t, httpapi.EnvironmentTest, "creator-1", &creatorPlan.ID)
	harness.insertCustomer(t, httpapi.EnvironmentTest, "creator-2", &creatorPlan.ID)
	harness.insertCustomer(t, httpapi.EnvironmentLive, "live-creator", &livePlan.ID)
	harness.insertCustomer(t, httpapi.EnvironmentLive, "live-unassigned", nil)

	firstPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, plansPath+"?limit=2", httpapi.EnvironmentTest, ""))
	nextCursor, isString := firstPage["next_cursor"].(string)
	if !isString {
		t.Fatalf("first page next_cursor = %v, want a cursor", firstPage["next_cursor"])
	}
	secondPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, plansPath+"?limit=2&cursor="+nextCursor, httpapi.EnvironmentTest, ""))
	livePage := decodeBody(t, harness.memberRequest(t, http.MethodGet, plansPath, httpapi.EnvironmentLive, ""))
	assertProblem(t, harness.memberRequest(t, http.MethodGet, plansPath+"?limit=2&cursor="+nextCursor, httpapi.EnvironmentLive, ""), http.StatusUnprocessableEntity, "invalid_cursor")

	wantCounts := []map[string]any{
		{"id": identifiers.Encode(identifiers.PrefixPlan, emptyPlan.ID), "customer_count": float64(0)},
		{"id": identifiers.Encode(identifiers.PrefixPlan, creatorPlan.ID), "customer_count": float64(2)},
		{"id": identifiers.Encode(identifiers.PrefixPlan, defaultPlan.ID), "customer_count": float64(3)},
	}
	gotItems := append(pageItems(t, firstPage), pageItems(t, secondPage)...)
	if diff := cmp.Diff(wantCounts, idsAndCounts(gotItems)); diff != "" {
		t.Errorf("test plans mismatch (-want +got):\n%s", diff)
	}
	if secondPage["next_cursor"] != nil {
		t.Errorf("second page next_cursor = %v, want null", secondPage["next_cursor"])
	}
	wantLive := []map[string]any{{"id": identifiers.Encode(identifiers.PrefixPlan, livePlan.ID), "customer_count": float64(1)}}
	if diff := cmp.Diff(wantLive, idsAndCounts(pageItems(t, livePage))); diff != "" {
		t.Errorf("live plans mismatch (-want +got):\n%s", diff)
	}
	fetched := decodeBody(t, harness.memberRequest(t, http.MethodGet, planPath(defaultPlan.ID), httpapi.EnvironmentTest, ""))
	if fetched["customer_count"] != float64(3) {
		t.Errorf("customer_count of the default plan = %v, want 3", fetched["customer_count"])
	}
}

func TestUpdateRouteArchivesPlans(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	defaultPlan := harness.createPlan(t, httpapi.EnvironmentTest, "Free")
	otherPlan := harness.createPlan(t, httpapi.EnvironmentTest, "Creator")
	harness.setDefaultPlan(t, httpapi.EnvironmentTest, defaultPlan.ID)

	recorder := harness.memberRequest(t, http.MethodPatch, planPath(defaultPlan.ID), httpapi.EnvironmentTest, `{"status":"archived"}`)

	assertProblem(t, recorder, http.StatusConflict, "plan_is_default")
	if status := harness.planStatus(t, defaultPlan.ID); status != "active" {
		t.Errorf("status of the default plan = %s, want active", status)
	}
	archived := harness.memberRequest(t, http.MethodPatch, planPath(otherPlan.ID), httpapi.EnvironmentTest, `{"status":"archived"}`)
	assertStatus(t, archived, http.StatusOK)
	if status := decodeBody(t, archived)["status"]; status != "archived" {
		t.Errorf("status after archiving = %v, want archived", status)
	}
	restored := harness.memberRequest(t, http.MethodPatch, planPath(otherPlan.ID), httpapi.EnvironmentTest, `{"status":"active"}`)
	assertStatus(t, restored, http.StatusOK)
	if status := decodeBody(t, restored)["status"]; status != "active" {
		t.Errorf("status after restoring = %v, want active", status)
	}
}

func TestUpdateRouteSwitchesMode(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	plan := harness.createPlan(t, httpapi.EnvironmentTest, "Creator")
	path := planPath(plan.ID)
	steps := []struct {
		name          string
		body          string
		wantMode      string
		wantMargin    string
		wantAllowance any
	}{
		{name: "switch to fixed allowance", body: `{"mode":"fixed_allowance","allowance":"3.50"}`, wantMode: "fixed_allowance", wantMargin: "0.4000", wantAllowance: "3.500000000"},
		{name: "change allowance", body: `{"allowance":"4"}`, wantMode: "fixed_allowance", wantMargin: "0.4000", wantAllowance: "4.000000000"},
		{name: "change stored target margin", body: `{"target_margin":"0.55"}`, wantMode: "fixed_allowance", wantMargin: "0.5500", wantAllowance: "4.000000000"},
		{name: "switch back to margin target", body: `{"mode":"margin_target"}`, wantMode: "margin_target", wantMargin: "0.5500", wantAllowance: nil},
	}
	for _, step := range steps {
		recorder := harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, step.body)

		assertStatus(t, recorder, http.StatusOK)
		body := decodeBody(t, recorder)
		got := []any{body["mode"], body["target_margin"], body["allowance"]}
		if diff := cmp.Diff([]any{step.wantMode, step.wantMargin, step.wantAllowance}, got); diff != "" {
			t.Errorf("%s: mode, target margin and allowance mismatch (-want +got):\n%s", step.name, diff)
		}
	}
	rejected := []struct {
		name string
		body string
	}{
		{name: "fixed allowance without allowance", body: `{"mode":"fixed_allowance"}`},
		{name: "allowance in margin target mode", body: `{"allowance":"1"}`},
	}
	for _, test := range rejected {
		recorder := harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, test.body)

		assertLocations(t, assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed"), "body.allowance")
	}
}

func TestUpdateRouteRenamesAndReplacesHoldTimes(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	harness.createPlan(t, httpapi.EnvironmentTest, "Free")
	plan := harness.createPlan(t, httpapi.EnvironmentTest, "Creator")
	path := planPath(plan.ID)

	assertProblem(t, harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, `{"name":"Free"}`), http.StatusConflict, "plan_name_taken")

	renamed := harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, `{"name":"Studio","hold_times":{"text_to_video":1800,"chat":30}}`)
	assertStatus(t, renamed, http.StatusOK)
	renamedBody := decodeBody(t, renamed)
	if renamedBody["name"] != "Studio" {
		t.Errorf("name = %v, want Studio", renamedBody["name"])
	}
	if diff := cmp.Diff(map[string]any{"text_to_video": float64(1800), "chat": float64(30)}, renamedBody["hold_times"]); diff != "" {
		t.Errorf("hold_times mismatch (-want +got):\n%s", diff)
	}
	replaced := decodeBody(t, harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, `{"hold_times":{"chat":86400}}`))
	if diff := cmp.Diff(map[string]any{"chat": float64(86400)}, replaced["hold_times"]); diff != "" {
		t.Errorf("replaced hold_times mismatch (-want +got):\n%s", diff)
	}
	cleared := decodeBody(t, harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, `{"hold_times":{}}`))
	if diff := cmp.Diff(map[string]any{}, cleared["hold_times"]); diff != "" {
		t.Errorf("cleared hold_times mismatch (-want +got):\n%s", diff)
	}
	kept := decodeBody(t, harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, `{}`))
	if kept["name"] != "Studio" || kept["target_margin"] != "0.4000" {
		t.Errorf("empty update = %v, want the plan unchanged", kept)
	}
}

func TestPlanRoutesStayInEnvironment(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	livePlan := harness.createPlan(t, httpapi.EnvironmentLive, "Creator")
	tests := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "get a plan of the other environment", method: http.MethodGet, target: planPath(livePlan.ID)},
		{name: "update a plan of the other environment", method: http.MethodPatch, target: planPath(livePlan.ID), body: `{"name":"Studio"}`},
		{name: "get an unknown plan", method: http.MethodGet, target: planPath(identifiers.New())},
		{name: "get a malformed plan id", method: http.MethodGet, target: plansPath + "/pln_unknown"},
		{name: "update a malformed plan id", method: http.MethodPatch, target: plansPath + "/key_01jbvagescfn78y0938nkrkayd", body: `{"name":"Studio"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, test.method, test.target, httpapi.EnvironmentTest, test.body)

			assertProblem(t, recorder, http.StatusNotFound, "not_found")
		})
	}
	if name := decodeBody(t, harness.memberRequest(t, http.MethodGet, planPath(livePlan.ID), httpapi.EnvironmentLive, ""))["name"]; name != "Creator" {
		t.Errorf("live plan name = %v, want Creator", name)
	}
}

func TestPlanRoutesNeedAdminScope(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	livePlan := harness.createPlan(t, httpapi.EnvironmentLive, "Creator")
	_, runtimeSecret, err := harness.apiKeys.Create(t.Context(), httpapi.EnvironmentLive, "Checkout service", apikeys.ScopeRuntime, nil)
	if err != nil {
		t.Fatalf("create runtime key: %v", err)
	}
	_, adminSecret, err := harness.apiKeys.Create(t.Context(), httpapi.EnvironmentLive, "Operations", apikeys.ScopeAdmin, nil)
	if err != nil {
		t.Fatalf("create admin key: %v", err)
	}

	assertProblem(t, harness.bearerRequest(t, plansPath, runtimeSecret), http.StatusForbidden, "scope_forbidden")

	recorder := harness.bearerRequest(t, plansPath, adminSecret)
	assertStatus(t, recorder, http.StatusOK)
	wantItems := []map[string]any{{"id": identifiers.Encode(identifiers.PrefixPlan, livePlan.ID), "customer_count": float64(0)}}
	if diff := cmp.Diff(wantItems, idsAndCounts(pageItems(t, decodeBody(t, recorder)))); diff != "" {
		t.Errorf("plans listed with the live admin key mismatch (-want +got):\n%s", diff)
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
		item, isObject := entry.(map[string]any)
		if !isObject {
			t.Fatalf("item %v is not an object", entry)
		}
		items = append(items, item)
	}
	return items
}

func idsAndCounts(items []map[string]any) []map[string]any {
	summaries := make([]map[string]any, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, map[string]any{"id": item["id"], "customer_count": item["customer_count"]})
	}
	return summaries
}
