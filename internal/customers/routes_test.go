package customers_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
)

func TestUpsertRouteAdmitsRuntimeAndAdminKeys(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	for _, scope := range []apikeys.Scope{apikeys.ScopeRuntime, apikeys.ScopeAdmin} {
		t.Run(string(scope), func(t *testing.T) {
			secret := harness.createKey(t, httpapi.EnvironmentTest, scope)

			recorder := harness.putCustomer(t, secret, "customer-"+string(scope), `{}`)

			assertStatus(t, recorder, http.StatusOK)
		})
	}
	assertProblem(t, harness.putCustomer(t, "", testExternalID, `{}`), http.StatusUnauthorized, "authentication_required")
}

func TestUpsertRouteCreatesThenReplacesCustomer(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	secret := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeRuntime)
	planID := identifiers.Encode(identifiers.PrefixPlan, harness.createPlan(t, httpapi.EnvironmentLive, "Pro", "active"))

	created := harness.putCustomer(t, secret, testExternalID, fmt.Sprintf(
		`{"display_name":"Acme","plan_id":%q,"metadata":{"crm_id":12345678901234567890,"region":"eu","tags":["a","b"]}}`, planID))
	harness.clock.Advance(time.Minute)
	replaced := harness.putCustomer(t, secret, testExternalID, `{"display_name":null}`)

	assertStatus(t, created, http.StatusOK)
	createdBody := decodeBody(t, created)
	wantCreated := map[string]any{
		"id":           createdBody["id"],
		"external_id":  testExternalID,
		"display_name": "Acme",
		"plan_id":      planID,
		"metadata":     map[string]any{"crm_id": 12345678901234567890.0, "region": "eu", "tags": []any{"a", "b"}},
		"status":       "active",
		"created_at":   testStart.Format(time.RFC3339),
		"updated_at":   testStart.Format(time.RFC3339),
	}
	if diff := cmp.Diff(wantCreated, createdBody); diff != "" {
		t.Errorf("created body mismatch (-want +got):\n%s", diff)
	}
	if !strings.Contains(created.Body.String(), `"crm_id":12345678901234567890`) {
		t.Errorf("body %s does not keep every digit of crm_id", created.Body.String())
	}
	assertStatus(t, replaced, http.StatusOK)
	wantReplaced := map[string]any{
		"id":           createdBody["id"],
		"external_id":  testExternalID,
		"display_name": nil,
		"plan_id":      nil,
		"metadata":     map[string]any{},
		"status":       "active",
		"created_at":   testStart.Format(time.RFC3339),
		"updated_at":   testStart.Add(time.Minute).Format(time.RFC3339),
	}
	if diff := cmp.Diff(wantReplaced, decodeBody(t, replaced)); diff != "" {
		t.Errorf("replaced body mismatch (-want +got):\n%s", diff)
	}
}

func TestUpsertRouteStoresMetadataWithSurrogatePairs(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)

	recorder := harness.putCustomer(t, secret, testExternalID, `{"metadata":{"mood":"\ud83d\ude00","path":"C:\\u0000"}}`)

	assertStatus(t, recorder, http.StatusOK)
	want := map[string]any{"mood": "\U0001F600", "path": `C:\u0000`}
	if diff := cmp.Diff(want, decodeBody(t, recorder)["metadata"]); diff != "" {
		t.Errorf("metadata mismatch (-want +got):\n%s", diff)
	}
}

func TestUpsertRouteRejectsInvalidRequests(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	livePlanID := identifiers.Encode(identifiers.PrefixPlan, harness.createPlan(t, httpapi.EnvironmentLive, "Pro", "active"))
	tests := []struct {
		name           string
		externalIDPath string
		body           string
		status         int
		code           string
		locations      []string
	}{
		{name: "space in external id", externalIDPath: "acme%20corp", body: `{}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"path.external_id"}},
		{name: "129 character external id", externalIDPath: strings.Repeat("a", 129), body: `{}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"path.external_id"}},
		{name: "unknown plan", externalIDPath: testExternalID, body: fmt.Sprintf(`{"plan_id":%q}`, identifiers.Encode(identifiers.PrefixPlan, identifiers.New())), status: http.StatusUnprocessableEntity, code: "plan_not_found"},
		{name: "plan of the live environment", externalIDPath: testExternalID, body: fmt.Sprintf(`{"plan_id":%q}`, livePlanID), status: http.StatusUnprocessableEntity, code: "plan_not_found"},
		{name: "malformed plan id", externalIDPath: testExternalID, body: `{"plan_id":"pln_unknown"}`, status: http.StatusUnprocessableEntity, code: "plan_not_found"},
		{name: "id of another entity as plan id", externalIDPath: testExternalID, body: fmt.Sprintf(`{"plan_id":%q}`, identifiers.Encode(identifiers.PrefixCustomer, identifiers.New())), status: http.StatusUnprocessableEntity, code: "plan_not_found"},
		{name: "51 metadata keys", externalIDPath: testExternalID, body: `{"metadata":` + metadataWithKeys(51) + `}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"body.metadata"}},
		{name: "4097 bytes of metadata", externalIDPath: testExternalID, body: `{"metadata":` + metadataOfBytes(4097) + `}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"body.metadata"}},
		{name: "metadata that is not an object", externalIDPath: testExternalID, body: `{"metadata":["gold"]}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"body.metadata"}},
		{name: "unknown field", externalIDPath: testExternalID, body: `{"email":"sam@example.com"}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"body.email"}},
		{name: "NUL in a metadata value", externalIDPath: testExternalID, body: `{"metadata":{"note":"a\u0000b"}}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"body.metadata"}},
		{name: "NUL in a metadata key", externalIDPath: testExternalID, body: `{"metadata":{"note\u0000":"a"}}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"body.metadata"}},
		{name: "NUL in nested metadata", externalIDPath: testExternalID, body: `{"metadata":{"notes":[{"text":"\u0000"}]}}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"body.metadata"}},
		{name: "lone high surrogate in metadata", externalIDPath: testExternalID, body: `{"metadata":{"note":"\ud800"}}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"body.metadata"}},
		{name: "lone low surrogate in metadata", externalIDPath: testExternalID, body: `{"metadata":{"note":"a\udc00b"}}`, status: http.StatusUnprocessableEntity, code: "validation_failed", locations: []string{"body.metadata"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.putCustomer(t, secret, test.externalIDPath, test.body)

			assertProblem(t, recorder, test.status, test.code, test.locations...)
		})
	}
	if count := harness.testCustomerCount(t, testExternalID); count != 0 {
		t.Errorf("customer rows = %d after rejected requests, want 0", count)
	}
}
