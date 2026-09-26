package apikeys_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
)

func TestCreateRouteReturnsSecretOnce(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	recorder := harness.memberRequest(t, http.MethodPost, keysPath, httpapi.EnvironmentLive, `{"name":"Checkout service","scope":"admin"}`)

	assertStatus(t, recorder, http.StatusCreated)
	body := decodeBody(t, recorder)
	secret, isString := body["secret"].(string)
	if !isString || !secretPattern.MatchString(secret) {
		t.Fatalf("secret = %v, want a pb_live_admin_ secret", body["secret"])
	}
	want := map[string]any{
		"id":               body["id"],
		"name":             testName,
		"scope":            "admin",
		"secret_last_four": secret[len(secret)-4:],
		"status":           "active",
		"last_used_at":     nil,
		"created_at":       testStart.Format(time.RFC3339),
		"secret":           secret,
	}
	if diff := cmp.Diff(want, body); diff != "" {
		t.Errorf("body mismatch (-want +got):\n%s", diff)
	}
	apiKeyID, err := identifiers.Decode(identifiers.PrefixAPIKey, body["id"].(string))
	if err != nil {
		t.Fatalf("decode id: %v", err)
	}
	var createdBy uuid.UUID
	if err := harness.pool.QueryRow(t.Context(), "SELECT created_by_member_id FROM api_keys WHERE api_key_id = $1", apiKeyID).Scan(&createdBy); err != nil {
		t.Fatalf("read created_by_member_id: %v", err)
	}
	if createdBy != harness.memberID {
		t.Errorf("created_by_member_id = %s, want the signed-in member %s", createdBy, harness.memberID)
	}
	listed := decodeBody(t, harness.memberRequest(t, http.MethodGet, keysPath, httpapi.EnvironmentLive, ""))
	if item := listed["items"].([]any)[0].(map[string]any); item["id"] != body["id"] || item["secret"] != nil {
		t.Errorf("listed key = %v, want the created key without its secret", item)
	}
	assertStatus(t, harness.bearerRequest(t, adminProbe, secret), http.StatusOK)
}

func TestCreateRouteValidatesBody(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	tests := []struct {
		name     string
		body     string
		location string
	}{
		{name: "empty name", body: `{"name":"","scope":"runtime"}`, location: "body.name"},
		{name: "unknown scope", body: `{"name":"Checkout service","scope":"owner"}`, location: "body.scope"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, http.MethodPost, keysPath, httpapi.EnvironmentTest, test.body)

			assertLocations(t, assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed"), test.location)
		})
	}
}

func TestListRoutePagesWithinEnvironment(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	oldest, _ := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	harness.clock.Advance(time.Second)
	newest, _ := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)
	harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeAdmin)

	firstPage := harness.memberRequest(t, http.MethodGet, keysPath+"?limit=1", httpapi.EnvironmentTest, "")
	assertStatus(t, firstPage, http.StatusOK)
	firstBody := decodeBody(t, firstPage)
	secondPage := harness.memberRequest(t, http.MethodGet, keysPath+"?limit=1&cursor="+firstBody["next_cursor"].(string), httpapi.EnvironmentTest, "")
	assertStatus(t, secondPage, http.StatusOK)
	secondBody := decodeBody(t, secondPage)

	pages := [][]any{firstBody["items"].([]any), secondBody["items"].([]any)}
	var listed []string
	for _, items := range pages {
		for _, item := range items {
			listed = append(listed, item.(map[string]any)["id"].(string))
		}
	}
	want := []string{identifiers.Encode(identifiers.PrefixAPIKey, newest.ID), identifiers.Encode(identifiers.PrefixAPIKey, oldest.ID)}
	if diff := cmp.Diff(want, listed); diff != "" {
		t.Errorf("listed ids mismatch (-want +got):\n%s", diff)
	}
	if secondBody["next_cursor"] != nil {
		t.Errorf("next_cursor = %v on the last page, want null", secondBody["next_cursor"])
	}
	assertProblem(t, harness.memberRequest(t, http.MethodGet, keysPath+"?limit=101", httpapi.EnvironmentTest, ""), http.StatusUnprocessableEntity, "validation_failed")
	assertProblem(t, harness.memberRequest(t, http.MethodGet, keysPath+"?cursor=not-a-cursor", httpapi.EnvironmentTest, ""), http.StatusUnprocessableEntity, "invalid_cursor")
	assertProblem(t, harness.memberRequest(t, http.MethodGet, keysPath+"?limit=1&cursor="+firstBody["next_cursor"].(string), httpapi.EnvironmentLive, ""), http.StatusUnprocessableEntity, "invalid_cursor")
}

func TestRevokeRouteFindsOnlyKeysOfEnvironment(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	liveKey, liveSecret := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeRuntime)
	liveKeyPath := keysPath + "/" + identifiers.Encode(identifiers.PrefixAPIKey, liveKey.ID)
	tests := []struct {
		name        string
		target      string
		environment httpapi.Environment
	}{
		{name: "other environment", target: liveKeyPath, environment: httpapi.EnvironmentTest},
		{name: "unknown id", target: keysPath + "/" + identifiers.Encode(identifiers.PrefixAPIKey, identifiers.New()), environment: httpapi.EnvironmentLive},
		{name: "malformed id", target: keysPath + "/key_unknown", environment: httpapi.EnvironmentLive},
		{name: "id of another entity", target: keysPath + "/" + identifiers.Encode(identifiers.PrefixMember, liveKey.ID), environment: httpapi.EnvironmentLive},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertProblem(t, harness.memberRequest(t, http.MethodDelete, test.target, test.environment, ""), http.StatusNotFound, "not_found")
		})
	}
	assertStatus(t, harness.bearerRequest(t, runtimeProbe, liveSecret), http.StatusOK)

	assertStatus(t, harness.memberRequest(t, http.MethodDelete, liveKeyPath, httpapi.EnvironmentLive, ""), http.StatusNoContent)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, liveKeyPath, httpapi.EnvironmentLive, ""), http.StatusNoContent)

	listed := decodeBody(t, harness.memberRequest(t, http.MethodGet, keysPath, httpapi.EnvironmentLive, ""))
	if status := listed["items"].([]any)[0].(map[string]any)["status"]; status != "disabled" {
		t.Errorf("status after revoke = %v, want disabled", status)
	}
}
