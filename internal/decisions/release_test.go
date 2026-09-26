package decisions_test

import (
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/jobs/jobstest"
	"github.com/preburn/preburn/internal/ledger"
)

func TestReleaseFreesTheReservationOnce(t *testing.T) {
	harness := newReportHarness(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	decisionID := decodeIdentifier(t, identifiers.PrefixDecision, checked.DecisionID)
	customer := harness.ensureCustomer(t)
	want := decisions.ReleaseResult{DecisionID: checked.DecisionID, Status: "released"}
	wantCounter := map[string]string{
		"reserved": "0", "reserved:" + checkFeature: "0",
		"count": "1", "count:" + checkFeature: "1",
	}

	for attempt := 1; attempt <= 2; attempt++ {
		result, err := harness.reports.Release(t.Context(), httpapi.EnvironmentTest, decisionID)
		if err != nil {
			t.Fatalf("release %d: %v", attempt, err)
		}
		if diff := cmp.Diff(want, result); diff != "" {
			t.Errorf("release %d result mismatch (-want +got):\n%s", attempt, diff)
		}
		if diff := cmp.Diff(wantCounter, harness.counter(t, customer.ID)); diff != "" {
			t.Errorf("counter after release %d mismatch (-want +got):\n%s", attempt, diff)
		}
	}
	if status, _ := harness.decisionStatus(t, checked.DecisionID); status != "released" {
		t.Errorf("decision status = %s, want released", status)
	}
	if status := harness.reservationStatus(t, checked.DecisionID); status != "released" {
		t.Errorf("reservation status = %s, want released", status)
	}
}

func TestReleaseRefreshesTheRollupOfTheDecisionPeriod(t *testing.T) {
	harness := newReportHarness(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))

	if _, err := harness.reports.Release(t.Context(), httpapi.EnvironmentTest, decodeIdentifier(t, identifiers.PrefixDecision, checked.DecisionID)); err != nil {
		t.Fatalf("release: %v", err)
	}

	job := jobstest.RequireInserted(t, harness.pool, ledger.RollupRefreshArgs{}, nil)
	want := ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, harness.ensureCustomer(t).ID, checkPeriod.Start)
	if diff := cmp.Diff(want, job.Args); diff != "" {
		t.Errorf("rollup refresh job mismatch (-want +got):\n%s", diff)
	}
}

func TestReleaseLeavesAReportedDecisionSettled(t *testing.T) {
	harness := newReportHarness(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	harness.report(t, serverReport(checked.DecisionID, "6"))
	customer := harness.ensureCustomer(t)
	counterAfterReport := harness.counter(t, customer.ID)

	result, err := harness.reports.Release(t.Context(), httpapi.EnvironmentTest, decodeIdentifier(t, identifiers.PrefixDecision, checked.DecisionID))
	if err != nil {
		t.Fatalf("release: %v", err)
	}

	if result.Status != "settled" {
		t.Errorf("status = %s, want settled", result.Status)
	}
	if diff := cmp.Diff(counterAfterReport, harness.counter(t, customer.ID)); diff != "" {
		t.Errorf("counter changed by the release (-report +release):\n%s", diff)
	}
}

func TestReleaseOfAnUnknownDecisionIsNotFound(t *testing.T) {
	harness := newReportHarness(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))

	_, err := harness.reports.Release(t.Context(), httpapi.EnvironmentTest, identifiers.New())
	assertCodedError(t, err, http.StatusNotFound, "not_found")

	_, err = harness.reports.Release(t.Context(), httpapi.EnvironmentLive, decodeIdentifier(t, identifiers.PrefixDecision, checked.DecisionID))
	assertCodedError(t, err, http.StatusNotFound, "not_found")
}
