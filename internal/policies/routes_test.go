package policies_test

import (
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
)

const (
	routeChainWithAlias = `{"outcome": "route", "route_chain": [{"provider": "fal_ai", "model": "fal-ai/veo3.1/fast/image-to-video"}, {"provider": "fal_ai", "model": "fal-ai/veo3.1/lite"}], "overrides": {"duration": "4s"}}`
	policyInvalidType   = "https://github.com/preburn/preburn/blob/main/docs/errors.md#policy_invalid"
)

func TestCreateRouteReturnsVersionOne(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	created := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"status": omitted}))

	policyID, isString := created["id"].(string)
	if !isString {
		t.Fatalf("id = %v, want a policy id", created["id"])
	}
	if _, err := identifiers.Decode(identifiers.PrefixPolicy, policyID); err != nil {
		t.Errorf("id %s is not a policy id: %v", policyID, err)
	}
	want := map[string]any{
		"id":             policyID,
		"name":           "Slow down heavy video",
		"level":          "everyone",
		"plan_id":        nil,
		"customer_id":    nil,
		"feature":        nil,
		"when":           map[string]any{"all": []any{map[string]any{"signal": "pace", "operator": "gt", "value": "1.5000"}}},
		"action":         map[string]any{"outcome": "deny", "route_chain": nil, "overrides": nil, "limit": nil},
		"enforcement":    "soft",
		"on_unreachable": "allow",
		"on_uncosted":    "allow",
		"status":         "active",
		"version":        float64(1),
		"created_at":     serviceStart.Format(time.RFC3339),
		"updated_at":     serviceStart.Format(time.RFC3339),
	}
	if diff := cmp.Diff(want, created); diff != "" {
		t.Errorf("created body mismatch (-want +got):\n%s", diff)
	}
	fetched := harness.memberRequest(t, http.MethodGet, policyPath(policyID), httpapi.EnvironmentTest, "")
	assertStatus(t, fetched, http.StatusOK)
	if diff := cmp.Diff(want, decodeBody(t, fetched)); diff != "" {
		t.Errorf("fetched body mismatch (-want +got):\n%s", diff)
	}
}

func TestUpdateRouteIncrementsVersion(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(nil))
	path := policyPath(created["id"])
	steps := []struct {
		name    string
		changes string
		want    map[string]any
	}{
		{
			name:    "rename and scope to a feature",
			changes: `{"name": "Deny heavy video", "feature": "text_to_video"}`,
			want:    map[string]any{"name": "Deny heavy video", "feature": "text_to_video", "status": "active"},
		},
		{
			name:    "replace the conditions and disable",
			changes: `{"when": {"any": [{"signal": "cost_to_date", "operator": "gte", "value": "25"}]}, "status": "disabled"}`,
			want:    map[string]any{"name": "Deny heavy video", "feature": "text_to_video", "status": "disabled"},
		},
		{
			name:    "clear the feature",
			changes: `{"feature": null}`,
			want:    map[string]any{"name": "Deny heavy video", "feature": nil, "status": "disabled"},
		},
	}
	for index, step := range steps {
		harness.clock.Advance(time.Minute)

		recorder := harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, step.changes)

		assertStatus(t, recorder, http.StatusOK)
		body := decodeBody(t, recorder)
		step.want["version"] = float64(index + 2)
		step.want["created_at"] = serviceStart.Format(time.RFC3339)
		step.want["updated_at"] = harness.clock.Now().Format(time.RFC3339)
		got := map[string]any{}
		for key := range step.want {
			got[key] = body[key]
		}
		if diff := cmp.Diff(step.want, got); diff != "" {
			t.Errorf("%s: body mismatch (-want +got):\n%s", step.name, diff)
		}
	}
	fetched := decodeBody(t, harness.memberRequest(t, http.MethodGet, path, httpapi.EnvironmentTest, ""))
	wantWhen := map[string]any{"any": []any{map[string]any{"signal": "cost_to_date", "operator": "gte", "value": "25.000000000"}}}
	if diff := cmp.Diff(wantWhen, fetched["when"]); diff != "" {
		t.Errorf("stored when mismatch (-want +got):\n%s", diff)
	}
	if fetched["version"] != float64(4) {
		t.Errorf("stored version = %v, want 4", fetched["version"])
	}
}

func TestCreateRouteRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	liveCustomerID := identifiers.Encode(identifiers.PrefixCustomer, harness.insertCustomer(t, httpapi.EnvironmentLive, nil, "active"))
	tests := []struct {
		name      string
		body      string
		locations []string
	}{
		{name: "array body", body: `[]`, locations: []string{"body"}},
		{name: "null body", body: `null`, locations: []string{"body"}},
		{name: "empty name", body: documentJSON(map[string]string{"name": `""`}), locations: []string{"body.name"}},
		{
			name:      "decode problems",
			body:      documentJSON(map[string]string{"colour": `"red"`, "level": omitted, "when": `{"all": [{"signal": "pace", "operator": "gt", "value": "fast"}]}`}),
			locations: []string{"body.colour", "body.level", "body.when.all[0].value"},
		},
		{
			name:      "validation problems",
			body:      documentJSON(map[string]string{"name": `""`, "action": `{"outcome": "cap"}`}),
			locations: []string{"body.name", "body.action"},
		},
		{name: "customer of the other environment", body: documentJSON(map[string]string{"level": `"customer"`, "customer_id": quoted(liveCustomerID)}), locations: []string{"body.customer_id"}},
		{
			name:      "condition groups four deep",
			body:      documentJSON(map[string]string{"when": `{"all": [{"all": [{"all": [{"all": []}]}]}]}`}),
			locations: []string{"body.when.all[0].all[0].all[0]"},
		},
		{
			name:      "route override a target lacks",
			body:      documentJSON(map[string]string{"action": `{"outcome": "route", "route_chain": [{"provider": "fal_ai", "model": "fal-ai/veo3.1/fast"}, {"provider": "deepgram", "model": "nova-3"}], "overrides": {"duration": "4s"}}`}),
			locations: []string{"body.action.overrides.duration"},
		},
		{
			name:      "NUL in a route target",
			body:      documentJSON(map[string]string{"action": `{"outcome": "route", "route_chain": [{"provider": "fal_ai", "model": "fal-ai/veo3.1\u0000"}]}`}),
			locations: []string{"body.action.route_chain[0].model"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, http.MethodPost, policiesPath, httpapi.EnvironmentTest, test.body)

			problem := assertProblem(t, recorder, http.StatusUnprocessableEntity, "policy_invalid")
			if diff := cmp.Diff(test.locations, problemLocations(t, problem)); diff != "" {
				t.Errorf("locations mismatch (-want +got):\n%s", diff)
			}
			if problem["type"] != policyInvalidType || problem["detail"] != "policy document is invalid" {
				t.Errorf("type and detail = %v and %v, want %s and the policy_invalid detail", problem["type"], problem["detail"], policyInvalidType)
			}
		})
	}
	listed := decodeBody(t, harness.memberRequest(t, http.MethodGet, policiesPath, httpapi.EnvironmentTest, ""))
	if items := listed["items"]; !cmp.Equal(items, []any{}) {
		t.Errorf("policies after rejected creates = %v, want none", items)
	}
}

func TestPlanLevelPoliciesNeedAnActivePlan(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	activePlanID := identifiers.Encode(identifiers.PrefixPlan, harness.insertPlan(t, httpapi.EnvironmentTest, dollars(10)))
	archivedPlanID := identifiers.Encode(identifiers.PrefixPlan, harness.insertArchivedPlan(t, httpapi.EnvironmentTest))
	livePlanID := identifiers.Encode(identifiers.PrefixPlan, harness.insertPlan(t, httpapi.EnvironmentLive, dollars(10)))
	unknownPlanID := identifiers.Encode(identifiers.PrefixPlan, identifiers.New())
	disabledCustomerID := identifiers.Encode(identifiers.PrefixCustomer, harness.insertCustomer(t, httpapi.EnvironmentTest, nil, "disabled"))
	planPolicy := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"level": `"plan"`, "plan_id": quoted(activePlanID)}))

	for _, planID := range []string{archivedPlanID, livePlanID, unknownPlanID} {
		draft := documentJSON(map[string]string{"level": `"plan"`, "plan_id": quoted(planID)})
		assertProblem(t, harness.memberRequest(t, http.MethodPost, policiesPath, httpapi.EnvironmentTest, draft), http.StatusUnprocessableEntity, "plan_not_found")
		assertProblem(t, harness.memberRequest(t, http.MethodPost, previewPath, httpapi.EnvironmentTest, draft), http.StatusUnprocessableEntity, "plan_not_found")
		switched := harness.memberRequest(t, http.MethodPatch, policyPath(planPolicy["id"]), httpapi.EnvironmentTest, `{"plan_id": `+quoted(planID)+`}`)
		assertProblem(t, switched, http.StatusUnprocessableEntity, "plan_not_found")
	}
	harness.archivePlan(t, activePlanID)
	kept := harness.memberRequest(t, http.MethodPatch, policyPath(planPolicy["id"]), httpapi.EnvironmentTest, `{"status": "disabled"}`)
	assertStatus(t, kept, http.StatusOK)
	harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"level": `"customer"`, "customer_id": quoted(disabledCustomerID)}))
}

func TestCreateRouteRejectsBodiesThatAreNotJSON(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	recorder := harness.memberRequest(t, http.MethodPost, policiesPath, httpapi.EnvironmentTest, `{"name": `)

	problem := assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed")
	if diff := cmp.Diff([]string{"body"}, problemLocations(t, problem)); diff != "" {
		t.Errorf("locations mismatch (-want +got):\n%s", diff)
	}
}

func TestUpdateRouteRejectsInvalidChanges(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(nil))
	path := policyPath(created["id"])
	tests := []struct {
		name      string
		changes   string
		locations []string
	}{
		{name: "array body", changes: `[]`, locations: []string{"body"}},
		{name: "empty any group", changes: `{"when": {"any": []}}`, locations: []string{"body.when.any"}},
		{name: "server assigned field", changes: `{"version": 3}`, locations: []string{"body.version"}},
		{name: "plan level without a plan", changes: `{"level": "plan"}`, locations: []string{"body.plan_id"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, test.changes)

			problem := assertProblem(t, recorder, http.StatusUnprocessableEntity, "policy_invalid")
			if diff := cmp.Diff(test.locations, problemLocations(t, problem)); diff != "" {
				t.Errorf("locations mismatch (-want +got):\n%s", diff)
			}
		})
	}
	fetched := decodeBody(t, harness.memberRequest(t, http.MethodGet, path, httpapi.EnvironmentTest, ""))
	if diff := cmp.Diff(created, fetched); diff != "" {
		t.Errorf("policy after rejected updates mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateRouteStoresRouteTargetAliasesAsTheirModel(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	document := documentJSON(map[string]string{"action": routeChainWithAlias})

	withoutAlias := harness.memberRequest(t, http.MethodPost, policiesPath, httpapi.EnvironmentTest, document)
	problem := assertProblem(t, withoutAlias, http.StatusUnprocessableEntity, "policy_invalid")
	if diff := cmp.Diff([]string{"body.action.overrides.duration"}, problemLocations(t, problem)); diff != "" {
		t.Errorf("locations without the alias mismatch (-want +got):\n%s", diff)
	}
	harness.insertModelAlias(t, "fal_ai", "fal-ai/veo3.1/fast/image-to-video", "fal-ai/veo3.1/fast")

	created := harness.createPolicy(t, httpapi.EnvironmentTest, document)

	wantAction := map[string]any{
		"outcome": "route",
		"route_chain": []any{
			map[string]any{"provider": "fal_ai", "model": "fal-ai/veo3.1/fast"},
			map[string]any{"provider": "fal_ai", "model": "fal-ai/veo3.1/lite"},
		},
		"overrides": map[string]any{"duration": "4s"},
		"limit":     nil,
	}
	if diff := cmp.Diff(wantAction, created["action"]); diff != "" {
		t.Errorf("action mismatch (-want +got):\n%s", diff)
	}
}

func TestPolicyRoutesStayInEnvironment(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	livePolicy := harness.createPolicy(t, httpapi.EnvironmentLive, documentJSON(nil))
	tests := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "get a policy of the other environment", method: http.MethodGet, target: policyPath(livePolicy["id"])},
		{name: "update a policy of the other environment", method: http.MethodPatch, target: policyPath(livePolicy["id"]), body: `{"name": "Other"}`},
		{name: "get an unknown policy", method: http.MethodGet, target: policyPath(identifiers.Encode(identifiers.PrefixPolicy, identifiers.New()))},
		{name: "get a malformed policy id", method: http.MethodGet, target: policiesPath + "/pol_unknown"},
		{name: "update with a plan id", method: http.MethodPatch, target: policyPath(identifiers.Encode(identifiers.PrefixPlan, identifiers.New())), body: `{"name": "Other"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, test.method, test.target, httpapi.EnvironmentTest, test.body)

			assertProblem(t, recorder, http.StatusNotFound, "not_found")
		})
	}
	fetched := decodeBody(t, harness.memberRequest(t, http.MethodGet, policyPath(livePolicy["id"]), httpapi.EnvironmentLive, ""))
	if fetched["name"] != livePolicy["name"] || fetched["version"] != float64(1) {
		t.Errorf("live policy = %v, want it unchanged", fetched)
	}
}

func TestListRouteFiltersByStatusAndPages(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	oldest := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"name": `"Oldest"`}))
	harness.clock.Advance(time.Minute)
	disabled := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"name": `"Disabled"`, "status": `"disabled"`}))
	harness.clock.Advance(time.Minute)
	newest := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"name": `"Newest"`}))
	harness.createPolicy(t, httpapi.EnvironmentLive, documentJSON(nil))

	firstPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, policiesPath+"?limit=2", httpapi.EnvironmentTest, ""))
	nextCursor, isString := firstPage["next_cursor"].(string)
	if !isString {
		t.Fatalf("first page next_cursor = %v, want a cursor", firstPage["next_cursor"])
	}
	secondPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, policiesPath+"?limit=2&cursor="+nextCursor, httpapi.EnvironmentTest, ""))
	assertProblem(t, harness.memberRequest(t, http.MethodGet, policiesPath+"?limit=2&cursor="+nextCursor, httpapi.EnvironmentLive, ""), http.StatusUnprocessableEntity, "invalid_cursor")
	disabledPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, policiesPath+"?status=disabled", httpapi.EnvironmentTest, ""))
	archivedPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, policiesPath+"?status=archived", httpapi.EnvironmentTest, ""))

	pages := map[string][]any{
		"all":      append(policyIDs(t, firstPage), policyIDs(t, secondPage)...),
		"disabled": policyIDs(t, disabledPage),
		"archived": policyIDs(t, archivedPage),
	}
	want := map[string][]any{
		"all":      {newest["id"], disabled["id"], oldest["id"]},
		"disabled": {disabled["id"]},
		"archived": {},
	}
	if diff := cmp.Diff(want, pages); diff != "" {
		t.Errorf("listed policies mismatch (-want +got):\n%s", diff)
	}
	if secondPage["next_cursor"] != nil {
		t.Errorf("second page next_cursor = %v, want null", secondPage["next_cursor"])
	}
	unknownStatus := harness.memberRequest(t, http.MethodGet, policiesPath+"?status=paused", httpapi.EnvironmentTest, "")
	if diff := cmp.Diff([]string{"query.status"}, problemLocations(t, assertProblem(t, unknownStatus, http.StatusUnprocessableEntity, "validation_failed"))); diff != "" {
		t.Errorf("unknown status locations mismatch (-want +got):\n%s", diff)
	}
}

func TestListRouteFiltersByPlan(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	planID := identifiers.Encode(identifiers.PrefixPlan, harness.insertPlan(t, httpapi.EnvironmentTest, dollars(10)))
	otherPlanID := identifiers.Encode(identifiers.PrefixPlan, harness.insertPlan(t, httpapi.EnvironmentTest, dollars(10)))
	livePlanID := identifiers.Encode(identifiers.PrefixPlan, harness.insertPlan(t, httpapi.EnvironmentLive, dollars(10)))
	older := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"level": `"plan"`, "plan_id": quoted(planID)}))
	harness.clock.Advance(time.Minute)
	newer := harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"level": `"plan"`, "plan_id": quoted(planID), "status": `"disabled"`}))
	harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(map[string]string{"level": `"plan"`, "plan_id": quoted(otherPlanID)}))
	harness.createPolicy(t, httpapi.EnvironmentTest, documentJSON(nil))
	harness.createPolicy(t, httpapi.EnvironmentLive, documentJSON(map[string]string{"level": `"plan"`, "plan_id": quoted(livePlanID)}))
	list := func(query string) map[string]any {
		return decodeBody(t, harness.memberRequest(t, http.MethodGet, policiesPath+query, httpapi.EnvironmentTest, ""))
	}

	firstPage := list("?limit=1&plan_id=" + planID)
	nextCursor, isString := firstPage["next_cursor"].(string)
	if !isString {
		t.Fatalf("first page next_cursor = %v, want a cursor", firstPage["next_cursor"])
	}
	secondPage := list("?limit=1&plan_id=" + planID + "&cursor=" + nextCursor)

	pages := map[string][]any{
		"plan":                          append(policyIDs(t, firstPage), policyIDs(t, secondPage)...),
		"plan and status":               policyIDs(t, list("?plan_id="+planID+"&status=active")),
		"plan of the other environment": policyIDs(t, list("?plan_id="+livePlanID)),
		"unknown plan":                  policyIDs(t, list("?plan_id="+identifiers.Encode(identifiers.PrefixPlan, identifiers.New()))),
	}
	want := map[string][]any{
		"plan":                          {newer["id"], older["id"]},
		"plan and status":               {older["id"]},
		"plan of the other environment": {},
		"unknown plan":                  {},
	}
	if diff := cmp.Diff(want, pages); diff != "" {
		t.Errorf("listed policies mismatch (-want +got):\n%s", diff)
	}
	if secondPage["next_cursor"] != nil {
		t.Errorf("second page next_cursor = %v, want null", secondPage["next_cursor"])
	}
	for _, malformed := range []string{"pln_unknown", identifiers.Encode(identifiers.PrefixCustomer, identifiers.New())} {
		recorder := harness.memberRequest(t, http.MethodGet, policiesPath+"?plan_id="+malformed, httpapi.EnvironmentTest, "")
		if diff := cmp.Diff([]string{"query.plan_id"}, problemLocations(t, assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed"))); diff != "" {
			t.Errorf("plan_id %s locations mismatch (-want +got):\n%s", malformed, diff)
		}
	}
}

func TestPolicyChangesPublishTheInvalidation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	subscription := subscribeInvalidationMessages(t, harness.cache)

	created := harness.createPolicy(t, httpapi.EnvironmentLive, documentJSON(nil))
	want := cache.Invalidation{Kind: cache.InvalidationKindPolicies, Environment: string(httpapi.EnvironmentLive), ID: created["id"].(string)}
	assertInvalidation(t, subscription, want)
	updated := harness.memberRequest(t, http.MethodPatch, policyPath(created["id"]), httpapi.EnvironmentLive, `{"status": "archived"}`)
	assertStatus(t, updated, http.StatusOK)
	assertInvalidation(t, subscription, want)
}

func TestParameterMappingsRouteServesTheCatalogFile(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	recorder := harness.memberRequest(t, http.MethodGet, parameterMappingsPath, httpapi.EnvironmentTest, "")

	assertStatus(t, recorder, http.StatusOK)
	models := pageModels(t, decodeBody(t, recorder))
	wantModels := map[string]any{
		"fal_ai fal-ai/veo3.1/fast": map[string]any{
			"duration": map[string]any{
				"provider_parameter": "duration",
				"value_type":         "string",
				"allowed_values":     []any{"4s", "6s", "8s"},
				"minimum":            nil,
				"maximum":            nil,
				"effect":             "sets",
				"meter":              "output_seconds",
				"quantities":         map[string]any{"4s": "4", "6s": "6", "8s": "8"},
			},
			"resolution": map[string]any{
				"provider_parameter": "resolution",
				"value_type":         "string",
				"allowed_values":     []any{"720p", "1080p", "4k"},
				"minimum":            nil,
				"maximum":            nil,
				"effect":             "prices",
				"meter":              "output_seconds",
				"quantities":         nil,
			},
			"audio": map[string]any{
				"provider_parameter": "generate_audio",
				"value_type":         "boolean",
				"allowed_values":     []any{},
				"minimum":            nil,
				"maximum":            nil,
				"effect":             "prices",
				"meter":              "output_seconds",
				"quantities":         nil,
			},
		},
		"runwayml gen4.5": map[string]any{
			"duration": map[string]any{
				"provider_parameter": "duration",
				"value_type":         "integer",
				"allowed_values":     []any{},
				"minimum":            float64(2),
				"maximum":            float64(10),
				"effect":             "sets",
				"meter":              "output_seconds",
				"quantities":         nil,
			},
		},
		"runwayml veo3.1_fast duration": map[string]any{
			"allowed_values": []any{float64(4), float64(6), float64(8)},
		},
		"deepgram nova-3": map[string]any{},
	}
	gotModels := map[string]any{
		"fal_ai fal-ai/veo3.1/fast":     models["fal_ai fal-ai/veo3.1/fast"],
		"runwayml gen4.5":               models["runwayml gen4.5"],
		"runwayml veo3.1_fast duration": map[string]any{"allowed_values": jsonPath(t, models["runwayml veo3.1_fast"], "duration", "allowed_values")},
		"deepgram nova-3":               models["deepgram nova-3"],
	}
	if diff := cmp.Diff(wantModels, gotModels); diff != "" {
		t.Errorf("parameter mappings mismatch (-want +got):\n%s", diff)
	}
	if len(models) != len(harness.files.ParameterMappings) {
		t.Errorf("models = %d, want %d", len(models), len(harness.files.ParameterMappings))
	}
}

func TestPolicyRoutesNeedAdminScope(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	livePolicy := harness.createPolicy(t, httpapi.EnvironmentLive, documentJSON(nil))
	_, runtimeSecret, err := harness.apiKeys.Create(t.Context(), httpapi.EnvironmentLive, "Checkout service", apikeys.ScopeRuntime, nil)
	if err != nil {
		t.Fatalf("create runtime key: %v", err)
	}
	_, adminSecret, err := harness.apiKeys.Create(t.Context(), httpapi.EnvironmentLive, "Operations", apikeys.ScopeAdmin, nil)
	if err != nil {
		t.Fatalf("create admin key: %v", err)
	}

	assertProblem(t, harness.bearerRequest(t, policiesPath, runtimeSecret), http.StatusForbidden, "scope_forbidden")

	recorder := harness.bearerRequest(t, policiesPath, adminSecret)
	assertStatus(t, recorder, http.StatusOK)
	if diff := cmp.Diff([]any{livePolicy["id"]}, policyIDs(t, decodeBody(t, recorder))); diff != "" {
		t.Errorf("policies listed with the live admin key mismatch (-want +got):\n%s", diff)
	}
}

func TestOpenAPIDocumentsPolicyBodies(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	recorder := harness.memberRequest(t, http.MethodGet, "/api/v1/openapi.json", httpapi.EnvironmentTest, "")

	assertStatus(t, recorder, http.StatusOK)
	document := decodeBody(t, recorder)
	createSchema := jsonPath(t, document, "paths", "/api/v1/policies", "post", "requestBody", "content", "application/json", "schema")
	updateSchema := jsonPath(t, document, "paths", "/api/v1/policies/{policy_id}", "patch", "requestBody", "content", "application/json", "schema")
	wantRequired := []any{"name", "level", "when", "action", "enforcement", "on_unreachable", "on_uncosted"}
	if diff := cmp.Diff(wantRequired, jsonPath(t, createSchema, "required")); diff != "" {
		t.Errorf("create required fields mismatch (-want +got):\n%s", diff)
	}
	if required, found := updateSchema.(map[string]any)["required"]; found {
		t.Errorf("update required fields = %v, want none", required)
	}
	componentReferences := map[string]any{
		"when":   jsonPath(t, createSchema, "properties", "when", "$ref"),
		"action": jsonPath(t, updateSchema, "properties", "action", "$ref"),
		"member": jsonPath(t, document, "components", "schemas", "PolicyConditionGroup", "properties", "any", "items", "oneOf"),
	}
	wantReferences := map[string]any{
		"when":   "#/components/schemas/PolicyConditionGroup",
		"action": "#/components/schemas/PolicyAction",
		"member": []any{
			map[string]any{"$ref": "#/components/schemas/PolicyCondition"},
			map[string]any{"$ref": "#/components/schemas/PolicyConditionGroup"},
		},
	}
	if diff := cmp.Diff(wantReferences, componentReferences); diff != "" {
		t.Errorf("component references mismatch (-want +got):\n%s", diff)
	}
	conditionFields := sortedKeys(jsonPath(t, document, "components", "schemas", "PolicyCondition", "properties"))
	if diff := cmp.Diff([]any{"operator", "signal", "value"}, conditionFields); diff != "" {
		t.Errorf("condition fields mismatch (-want +got):\n%s", diff)
	}
	outcomes := jsonPath(t, document, "components", "schemas", "PolicyAction", "properties", "outcome", "enum")
	if diff := cmp.Diff([]any{"allow", "route", "cap", "deny"}, outcomes); diff != "" {
		t.Errorf("action outcomes mismatch (-want +got):\n%s", diff)
	}
}

func policyIDs(t *testing.T, page map[string]any) []any {
	t.Helper()
	identifiers := []any{}
	for _, item := range pageItems(t, page) {
		identifiers = append(identifiers, item["id"])
	}
	return identifiers
}

func pageModels(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	listed, isList := body["models"].([]any)
	if !isList {
		t.Fatalf("body %v has no models list", body)
	}
	models := map[string]any{}
	var order []string
	for _, entry := range listed {
		model, isObject := entry.(map[string]any)
		if !isObject {
			t.Fatalf("model %v is not an object", entry)
		}
		key := model["provider"].(string) + " " + model["model"].(string)
		order = append(order, key)
		models[key] = model["parameters"]
	}
	if !slices.IsSorted(order) {
		t.Errorf("models are not ordered by provider and model: %v", order)
	}
	return models
}

func jsonPath(t *testing.T, value any, keys ...string) any {
	t.Helper()
	for _, key := range keys {
		switch typed := value.(type) {
		case map[string]any:
			next, found := typed[key]
			if !found {
				t.Fatalf("no %q in %v", key, strings.Join(keys, "."))
			}
			value = next
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index >= len(typed) {
				t.Fatalf("no item %q in %v", key, strings.Join(keys, "."))
			}
			value = typed[index]
		default:
			t.Fatalf("cannot descend into %T at %q", value, key)
		}
	}
	return value
}

func sortedKeys(value any) []any {
	keys := []any{}
	for _, key := range slices.Sorted(maps.Keys(value.(map[string]any))) {
		keys = append(keys, key)
	}
	return keys
}
