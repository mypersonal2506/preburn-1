package revenue_test

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/revenue"
)

const recordBody = `{"customer_id":"acme","kind":"subscription","amount":"30.00","period_start":"2026-09-01T00:00:00Z","period_end":"2026-10-01T00:00:00Z","source_reference":"in_1Q2w3E4r5T6y-line-1"}`

func TestRecordRouteAdmitsRuntimeAndAdminKeys(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	for _, scope := range []apikeys.Scope{apikeys.ScopeRuntime, apikeys.ScopeAdmin} {
		t.Run(string(scope), func(t *testing.T) {
			secret := harness.createKey(t, httpapi.EnvironmentTest, scope)
			body := strings.Replace(recordBody, testSourceReference, "line-"+string(scope), 1)

			recorder := harness.bearerRequest(t, http.MethodPost, secret, body)

			assertStatus(t, recorder, http.StatusCreated)
		})
	}
}

func TestRecordRouteReturnsCreatedThenDuplicate(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	secret := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeRuntime)

	created := harness.bearerRequest(t, http.MethodPost, secret, recordBody)
	harness.clock.Advance(time.Minute)
	repeated := harness.bearerRequest(t, http.MethodPost, secret, recordBody)

	assertStatus(t, created, http.StatusCreated)
	entry := decodeBody(t, created)
	id, isString := entry["id"].(string)
	if !isString || !strings.HasPrefix(id, "rev_") {
		t.Fatalf("id = %v, want a rev_ id", entry["id"])
	}
	want := map[string]any{
		"id":               id,
		"customer_id":      testExternalID,
		"kind":             "subscription",
		"amount":           "30.000000000",
		"period_start":     "2026-09-01T00:00:00Z",
		"period_end":       "2026-10-01T00:00:00Z",
		"source":           "api",
		"source_reference": testSourceReference,
		"occurred_at":      "2026-09-26T10:00:00Z",
		"created_at":       "2026-09-26T10:00:00Z",
		"duplicate":        false,
	}
	if diff := cmp.Diff(want, entry); diff != "" {
		t.Errorf("created entry mismatch (-want +got):\n%s", diff)
	}
	assertStatus(t, repeated, http.StatusOK)
	want["duplicate"] = true
	if diff := cmp.Diff(want, decodeBody(t, repeated)); diff != "" {
		t.Errorf("repeated entry mismatch (-want +got):\n%s", diff)
	}
}

func TestRecordRouteDocumentsTheDuplicateAnswer(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/openapi.json", nil)
	recorder := httptest.NewRecorder()

	harness.handler.ServeHTTP(recorder, request)

	assertStatus(t, recorder, http.StatusOK)
	responses := decodeBody(t, recorder)["paths"].(map[string]any)["/api/v1/revenue"].(map[string]any)["post"].(map[string]any)["responses"].(map[string]any)
	entrySchema := map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/RecordedRevenueEntryResponse"}}}
	for _, status := range []string{"200", "201"} {
		response, declared := responses[status].(map[string]any)
		if !declared {
			t.Errorf("responses %v, want %s declared", slices.Sorted(maps.Keys(responses)), status)
			continue
		}
		if diff := cmp.Diff(entrySchema, response["content"]); diff != "" {
			t.Errorf("%s content mismatch (-want +got):\n%s", status, diff)
		}
	}
	if _, declared := responses["default"]; !declared {
		t.Error("responses declare no default problem")
	}
}

func TestRecordRouteRejectsInvalidRequests(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	tests := []struct {
		name      string
		old       string
		new       string
		locations []string
	}{
		{name: "negative amount", old: `"30.00"`, new: `"-30.00"`, locations: []string{"body.amount"}},
		{name: "malformed amount", old: `"30.00"`, new: `"30,00"`, locations: []string{"body.amount"}},
		{name: "period end before start", old: `"2026-10-01T00:00:00Z"`, new: `"2026-08-31T00:00:00Z"`, locations: []string{"body.period_end"}},
		{name: "unknown kind", old: `"subscription"`, new: `"tip"`, locations: []string{"body.kind"}},
		{name: "invalid customer id", old: `"acme"`, new: `"acme corp"`, locations: []string{"body.customer_id"}},
		{name: "empty source reference", old: `"` + testSourceReference + `"`, new: `""`, locations: []string{"body.source_reference"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Replace(recordBody, test.old, test.new, 1)

			recorder := harness.bearerRequest(t, http.MethodPost, secret, body)

			assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed", test.locations...)
		})
	}
	if entries := harness.count(t, "revenue_entries"); entries != 0 {
		t.Errorf("revenue entries = %d after rejected requests, want 0", entries)
	}
}

func TestListRouteNeedsAdminScope(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	entry, _ := harness.record(t, httpapi.EnvironmentLive, revenue.SourceAPI, subscription(testExternalID, testSourceReference, dollars(30), septemberStart, octoberStart))

	assertProblem(t, harness.bearerRequest(t, http.MethodGet, harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeRuntime), ""),
		http.StatusForbidden, "scope_forbidden")

	recorder := harness.bearerRequest(t, http.MethodGet, harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeAdmin), "")
	assertStatus(t, recorder, http.StatusOK)
	items := pageItems(t, decodeBody(t, recorder))
	if len(items) != 1 || items[0]["id"] != identifiers.Encode(identifiers.PrefixRevenueEntry, entry.ID) {
		t.Errorf("items = %v, want the live entry %s", items, entry.ID)
	}
}

func TestListRouteFiltersAndPages(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	var subscriptionIDs []string
	for _, reference := range []string{"first", "second"} {
		entry, _ := harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, subscription(testExternalID, reference, dollars(30), augustStart, septemberStart))
		subscriptionIDs = append(subscriptionIDs, identifiers.Encode(identifiers.PrefixRevenueEntry, entry.ID))
		harness.clock.Advance(time.Minute)
	}
	harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, subscription("cedar", "cedar", dollars(30), augustStart, septemberStart))
	query := fmt.Sprintf("%s?customer_id=%s&kind=subscription&limit=1", revenuePath, testExternalID)

	firstPage := decodeBody(t, harness.memberRequest(t, query, httpapi.EnvironmentTest))
	cursor, isString := firstPage["next_cursor"].(string)
	if !isString {
		t.Fatalf("first page next_cursor = %v, want a cursor", firstPage["next_cursor"])
	}
	secondPage := decodeBody(t, harness.memberRequest(t, query+"&cursor="+cursor, httpapi.EnvironmentTest))
	assertProblem(t, harness.memberRequest(t, query+"&cursor="+cursor, httpapi.EnvironmentLive), http.StatusUnprocessableEntity, "invalid_cursor")

	got := []any{pageItems(t, firstPage)[0]["id"], pageItems(t, secondPage)[0]["id"], secondPage["next_cursor"]}
	if diff := cmp.Diff([]any{subscriptionIDs[1], subscriptionIDs[0], nil}, got); diff != "" {
		t.Errorf("pages mismatch (-want +got):\n%s", diff)
	}
	assertProblem(t, harness.memberRequest(t, revenuePath+"?kind=tip", httpapi.EnvironmentTest), http.StatusUnprocessableEntity, "validation_failed", "query.kind")
}
