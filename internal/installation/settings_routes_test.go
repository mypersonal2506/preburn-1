package installation_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
)

func TestSettingsRouteReturnsDefaults(t *testing.T) {
	t.Parallel()
	harness := newSettingsHarness(t)

	body := harness.settingsBody(t, httpapi.EnvironmentTest)

	want := map[string]any{
		"installation_name":            "Preburn",
		"default_plan_id":              nil,
		"stripe_customer_metadata_key": "preburn_customer_id",
	}
	if diff := cmp.Diff(want, body); diff != "" {
		t.Errorf("body mismatch (-want +got):\n%s", diff)
	}
}

func TestSettingsRouteSetsDefaultPlanPerEnvironment(t *testing.T) {
	t.Parallel()
	harness := newSettingsHarness(t)
	testPlan := harness.createPlan(t, httpapi.EnvironmentTest, "Free")
	livePlan := harness.createPlan(t, httpapi.EnvironmentLive, "Free")

	recorder := harness.memberRequest(t, http.MethodPatch, httpapi.EnvironmentTest, `{"default_plan_id":"`+encodedPlanID(testPlan)+`"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	if defaultPlanID := decodeBody(t, recorder)["default_plan_id"]; defaultPlanID != encodedPlanID(testPlan) {
		t.Errorf("default_plan_id in the response = %v, want %s", defaultPlanID, encodedPlanID(testPlan))
	}
	if defaultPlanID := harness.settingsBody(t, httpapi.EnvironmentTest)["default_plan_id"]; defaultPlanID != encodedPlanID(testPlan) {
		t.Errorf("test default_plan_id = %v, want %s", defaultPlanID, encodedPlanID(testPlan))
	}
	if defaultPlanID := harness.settingsBody(t, httpapi.EnvironmentLive)["default_plan_id"]; defaultPlanID != nil {
		t.Errorf("live default_plan_id before setting it = %v, want null", defaultPlanID)
	}
	liveRecorder := harness.memberRequest(t, http.MethodPatch, httpapi.EnvironmentLive, `{"default_plan_id":"`+encodedPlanID(livePlan)+`"}`)
	if liveRecorder.Code != http.StatusOK {
		t.Fatalf("live status = %d, want 200, body %s", liveRecorder.Code, liveRecorder.Body.String())
	}
	if defaultPlanID := harness.settingsBody(t, httpapi.EnvironmentLive)["default_plan_id"]; defaultPlanID != encodedPlanID(livePlan) {
		t.Errorf("live default_plan_id = %v, want %s", defaultPlanID, encodedPlanID(livePlan))
	}
	if defaultPlanID := harness.settingsBody(t, httpapi.EnvironmentTest)["default_plan_id"]; defaultPlanID != encodedPlanID(testPlan) {
		t.Errorf("test default_plan_id after setting live = %v, want %s", defaultPlanID, encodedPlanID(testPlan))
	}
}

func TestSettingsRouteClearsDefaultPlan(t *testing.T) {
	t.Parallel()
	harness := newSettingsHarness(t)
	plan := harness.createPlan(t, httpapi.EnvironmentTest, "Free")
	if recorder := harness.memberRequest(t, http.MethodPatch, httpapi.EnvironmentTest, `{"default_plan_id":"`+encodedPlanID(plan)+`"}`); recorder.Code != http.StatusOK {
		t.Fatalf("set default plan = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}

	kept := decodeBody(t, harness.memberRequest(t, http.MethodPatch, httpapi.EnvironmentTest, `{"stripe_customer_metadata_key":"account_id"}`))
	cleared := decodeBody(t, harness.memberRequest(t, http.MethodPatch, httpapi.EnvironmentTest, `{"default_plan_id":null}`))

	if kept["default_plan_id"] != encodedPlanID(plan) {
		t.Errorf("default_plan_id after an update without it = %v, want %s", kept["default_plan_id"], encodedPlanID(plan))
	}
	if cleared["default_plan_id"] != nil || cleared["stripe_customer_metadata_key"] != "account_id" {
		t.Errorf("settings after clearing the default plan = %v, want no default plan and the account_id key", cleared)
	}
}

func TestSettingsRouteRejectsPlanOutsideEnvironment(t *testing.T) {
	t.Parallel()
	harness := newSettingsHarness(t)
	livePlan := harness.createPlan(t, httpapi.EnvironmentLive, "Free")
	archivedPlan := harness.createPlan(t, httpapi.EnvironmentTest, "Legacy")
	if _, err := harness.pool.Exec(t.Context(), "UPDATE plans SET status = 'archived' WHERE plan_id = $1", archivedPlan.ID); err != nil {
		t.Fatalf("archive plan: %v", err)
	}
	tests := []struct {
		name   string
		planID string
	}{
		{name: "plan of the other environment", planID: encodedPlanID(livePlan)},
		{name: "archived plan", planID: encodedPlanID(archivedPlan)},
		{name: "unknown plan", planID: identifiers.Encode(identifiers.PrefixPlan, identifiers.New())},
		{name: "malformed plan id", planID: "pln_unknown"},
		{name: "id of another kind", planID: identifiers.Encode(identifiers.PrefixCustomer, livePlan.ID)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, http.MethodPatch, httpapi.EnvironmentTest, `{"default_plan_id":"`+test.planID+`"}`)

			assertProblem(t, recorder, http.StatusUnprocessableEntity, "plan_not_found")
		})
	}
	if defaultPlanID := harness.settingsBody(t, httpapi.EnvironmentTest)["default_plan_id"]; defaultPlanID != nil {
		t.Errorf("test default_plan_id after rejected updates = %v, want null", defaultPlanID)
	}
}

func TestSettingsRouteUpdatesNameAndMetadataKey(t *testing.T) {
	t.Parallel()
	harness := newSettingsHarness(t)

	recorder := harness.memberRequest(t, http.MethodPatch, httpapi.EnvironmentLive, `{"installation_name":"Acme AI","stripe_customer_metadata_key":"acme_customer"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	wantLive := map[string]any{
		"installation_name":            "Acme AI",
		"default_plan_id":              nil,
		"stripe_customer_metadata_key": "acme_customer",
	}
	if diff := cmp.Diff(wantLive, harness.settingsBody(t, httpapi.EnvironmentLive)); diff != "" {
		t.Errorf("live settings mismatch (-want +got):\n%s", diff)
	}
	wantTest := map[string]any{
		"installation_name":            "Acme AI",
		"default_plan_id":              nil,
		"stripe_customer_metadata_key": "preburn_customer_id",
	}
	if diff := cmp.Diff(wantTest, harness.settingsBody(t, httpapi.EnvironmentTest)); diff != "" {
		t.Errorf("test settings mismatch (-want +got):\n%s", diff)
	}
}

func TestSettingsRouteValidatesBody(t *testing.T) {
	t.Parallel()
	harness := newSettingsHarness(t)
	tests := []struct {
		name      string
		body      string
		locations []string
	}{
		{name: "empty name", body: `{"installation_name":""}`, locations: []string{"body.installation_name"}},
		{name: "blank name", body: `{"installation_name":"   "}`, locations: []string{"body.installation_name"}},
		{name: "long name", body: `{"installation_name":"` + strings.Repeat("n", 81) + `"}`, locations: []string{"body.installation_name"}},
		{name: "metadata key with a hyphen", body: `{"stripe_customer_metadata_key":"customer-id"}`, locations: []string{"body.stripe_customer_metadata_key"}},
		{name: "metadata key starting with a digit", body: `{"stripe_customer_metadata_key":"1customer"}`, locations: []string{"body.stripe_customer_metadata_key"}},
		{name: "long metadata key", body: `{"stripe_customer_metadata_key":"` + strings.Repeat("k", 41) + `"}`, locations: []string{"body.stripe_customer_metadata_key"}},
		{
			name:      "both fields",
			body:      `{"installation_name":"","stripe_customer_metadata_key":"Customer"}`,
			locations: []string{"body.installation_name", "body.stripe_customer_metadata_key"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, http.MethodPatch, httpapi.EnvironmentTest, test.body)

			assertSettingsLocations(t, recorder, test.locations...)
		})
	}
	if name := harness.settingsBody(t, httpapi.EnvironmentTest)["installation_name"]; name != "Preburn" {
		t.Errorf("installation_name after rejected updates = %v, want Preburn", name)
	}
}

func TestSettingsRoutesNeedMemberSession(t *testing.T) {
	t.Parallel()
	harness := newSettingsHarness(t)
	_, secret, err := harness.apiKeys.Create(t.Context(), httpapi.EnvironmentLive, "Operations", apikeys.ScopeAdmin, nil)
	if err != nil {
		t.Fatalf("create admin key: %v", err)
	}
	requests := []struct {
		method string
		body   string
	}{
		{method: http.MethodGet},
		{method: http.MethodPatch, body: `{"installation_name":"Acme AI"}`},
	}
	for _, test := range requests {
		request := newRequest(t, test.method, settingsPath, test.body)
		request.Header.Set(settingsAuthorizationHeader, "Bearer "+secret)

		assertProblem(t, harness.serve(request), http.StatusForbidden, "scope_forbidden")
	}
	if name := harness.settingsBody(t, httpapi.EnvironmentLive)["installation_name"]; name != "Preburn" {
		t.Errorf("installation_name after a rejected update = %v, want Preburn", name)
	}
}
