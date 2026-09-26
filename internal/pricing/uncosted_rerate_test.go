package pricing_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
)

const secondStandaloneBody = `{"provider":"acme","model":"acme-video-3","meter":"output_seconds","unit_price":"0.50","unit_quantity":1}`

func TestOverrideChangesEnqueueOneUncostedRerate(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	created := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)
	harness.createOverride(t, httpapi.EnvironmentTest, secondStandaloneBody)
	harness.createOverride(t, httpapi.EnvironmentLive, standaloneBody)
	harness.assertAvailableRerateJobs(t, "creates", map[string]int{"test": 1, "live": 1})

	harness.completeRerateJobs(t)
	changed := harness.patchOverride(t, created["id"], `{"unit_price":"0.75"}`)
	harness.assertAvailableRerateJobs(t, "price change", map[string]int{"test": 1})

	harness.completeRerateJobs(t)
	scheduledEnd := harness.clock.Now().Add(time.Hour).Format(time.RFC3339)
	harness.patchOverride(t, changed["id"], `{"effective_to":"`+scheduledEnd+`"}`)
	harness.assertAvailableRerateJobs(t, "end change", map[string]int{"test": 1})

	harness.completeRerateJobs(t)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, overridePath(changed["id"]), httpapi.EnvironmentTest, ""), http.StatusNoContent)
	harness.assertAvailableRerateJobs(t, "end", map[string]int{"test": 1})

	harness.completeRerateJobs(t)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, overridePath(changed["id"]), httpapi.EnvironmentTest, ""), http.StatusNoContent)
	harness.assertAvailableRerateJobs(t, "end of an ended override", map[string]int{})
}

func TestOverrideChangeRollsBackWhenItsRerateCannotBeEnqueued(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)
	harness.rejectRerateJobs(t)

	assertStatus(t, harness.memberRequest(t, http.MethodPost, overridesPath, httpapi.EnvironmentTest, secondStandaloneBody), http.StatusInternalServerError)
	assertStatus(t, harness.memberRequest(t, http.MethodPatch, overridePath(created["id"]), httpapi.EnvironmentTest, `{"unit_price":"0.75"}`), http.StatusInternalServerError)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, overridePath(created["id"]), httpapi.EnvironmentTest, ""), http.StatusInternalServerError)

	if count := harness.overrideCount(t); count != 1 {
		t.Errorf("overrides = %d, want only the one created before", count)
	}
	fetched := decodeBody(t, harness.memberRequest(t, http.MethodGet, overridePath(created["id"]), httpapi.EnvironmentTest, ""))
	want := map[string]any{"unit_price": "0.500000000", "effective_to": nil}
	got := map[string]any{"unit_price": fetched["unit_price"], "effective_to": fetched["effective_to"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("override after the failed changes mismatch (-want +got):\n%s", diff)
	}
}

func TestEndingAnOverrideAtItsCreationInstantLeavesAnEmptyWindow(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)

	recorder := harness.memberRequest(t, http.MethodDelete, overridePath(created["id"]), httpapi.EnvironmentTest, "")

	assertStatus(t, recorder, http.StatusNoContent)
	fetched := decodeBody(t, harness.memberRequest(t, http.MethodGet, overridePath(created["id"]), httpapi.EnvironmentTest, ""))
	if fetched["effective_to"] != fetched["effective_from"] {
		t.Errorf("effective_to = %v, want effective_from %v", fetched["effective_to"], fetched["effective_from"])
	}
	if items := pageItems(t, decodeBody(t, harness.memberRequest(t, http.MethodGet, overridesPath, httpapi.EnvironmentTest, ""))); len(items) != 0 {
		t.Errorf("open overrides = %v, want none", items)
	}
	assertUnknownModelCost(t, harness.service.RuleSets(), harness.clock.Now(), nil)
}

func (harness *harness) patchOverride(t *testing.T, exposedID any, body string) map[string]any {
	t.Helper()
	recorder := harness.memberRequest(t, http.MethodPatch, overridePath(exposedID), httpapi.EnvironmentTest, body)
	assertStatus(t, recorder, http.StatusOK)
	return decodeBody(t, recorder)
}

func (harness *harness) assertAvailableRerateJobs(t *testing.T, after string, want map[string]int) {
	t.Helper()
	rows, err := harness.pool.Query(t.Context(),
		`SELECT args ->> 'environment', count(*)
		FROM river_job
		WHERE kind = 'uncosted_rerate' AND state = 'available'
		GROUP BY args ->> 'environment'`)
	if err != nil {
		t.Fatalf("select uncosted rerate jobs: %v", err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var environment string
		var count int
		if err := rows.Scan(&environment, &count); err != nil {
			t.Fatalf("scan uncosted rerate jobs: %v", err)
		}
		got[environment] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read uncosted rerate jobs: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("available uncosted_rerate jobs after %s mismatch (-want +got):\n%s", after, diff)
	}
}

func (harness *harness) completeRerateJobs(t *testing.T) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		"UPDATE river_job SET state = 'completed', attempt = 1, attempted_at = now(), finalized_at = now() WHERE kind = 'uncosted_rerate'")
	if err != nil {
		t.Fatalf("complete uncosted rerate jobs: %v", err)
	}
}

func (harness *harness) rejectRerateJobs(t *testing.T) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		"ALTER TABLE river_job ADD CONSTRAINT river_job_uncosted_rerate_rejected CHECK (kind <> 'uncosted_rerate') NOT VALID")
	if err != nil {
		t.Fatalf("reject uncosted rerate jobs: %v", err)
	}
}

func (harness *harness) overrideCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM pricing_overrides").Scan(&count); err != nil {
		t.Fatalf("count overrides: %v", err)
	}
	return count
}
