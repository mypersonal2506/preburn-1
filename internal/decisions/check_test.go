package decisions_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/jobs/jobstest"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
)

func TestCheckWithoutPoliciesAllowsAndReservesTheEstimate(t *testing.T) {
	harness := newCheckHarness(t)
	userID := "user-7"
	request := videoCheck(veoModel, "8")
	request.CustomerUserID = &userID
	request.Attributes = pricing.Attributes{"resolution": pricing.StringAttribute("720p")}

	response := harness.check(t, request)

	want := decisions.CheckResponse{
		DecisionID:      response.DecisionID,
		Outcome:         policies.OutcomeAllow,
		Reason:          policies.ReasonNoPolicyMatched,
		Provider:        falProvider,
		Model:           veoModel,
		Overrides:       map[string]policies.OverrideValue{},
		EstimatedCost:   formattedDollars(120),
		ReservedAmount:  money.FormatAmount(dollarAmount(120)),
		EstimateBasis:   decisions.EstimateBasisRequestEstimate,
		CostStatus:      pricing.CostStatusCosted,
		FallbackOutcome: policies.OutcomeAllow,
		ExpiresAt:       checkStart.Add(defaultCheckHoldTime),
		Signals:         response.Signals,
	}
	if diff := cmp.Diff(want, response); diff != "" {
		t.Errorf("response mismatch (-want +got):\n%s", diff)
	}
	if response.Signals.RequestEstimatedCost == nil || *response.Signals.RequestEstimatedCost != "1.200000000" || response.Signals.PeriodDecisionCount != 0 {
		t.Errorf("signals = %+v, want request_estimated_cost 1.200000000 and no earlier decision", response.Signals)
	}

	customer := harness.ensureCustomer(t)
	row := harness.decision(t, response)
	if row.CustomerUserID == nil {
		t.Fatal("decision has no customer user")
	}
	wantRow := decisionRow{
		CustomerID:                  customer.ID,
		CustomerUserID:              row.CustomerUserID,
		Feature:                     checkFeature,
		RequestedProvider:           falProvider,
		RequestedModel:              veoModel,
		Provider:                    falProvider,
		Model:                       veoModel,
		Attributes:                  map[string]any{"resolution": "720p"},
		Overrides:                   map[string]any{},
		Outcome:                     "allow",
		Reason:                      "no_policy_matched",
		Signals:                     row.Signals,
		RequestedEstimatedCostNanos: new(int64(dollarAmount(120))),
		EstimatedCostNanos:          new(int64(dollarAmount(120))),
		ReservedNanos:               int64(dollarAmount(120)),
		EstimateBasis:               "request_estimate",
		Status:                      "reserved",
		PeriodStart:                 checkPeriod.Start,
		PeriodEnd:                   checkPeriod.End,
		ExpiresAt:                   checkStart.Add(defaultCheckHoldTime),
		CreatedAt:                   checkStart,
	}
	if diff := cmp.Diff(wantRow, row); diff != "" {
		t.Errorf("decision row mismatch (-want +got):\n%s", diff)
	}
	if row.Signals["request_estimated_cost"] != "1.200000000" {
		t.Errorf("stored signals = %v, want the signals at check time", row.Signals)
	}
	var userExternalID string
	if err := harness.pool.QueryRow(t.Context(), "SELECT external_id FROM customer_users WHERE customer_user_id = $1", *row.CustomerUserID).Scan(&userExternalID); err != nil || userExternalID != userID {
		t.Errorf("customer user external id = %q, %v, want %s", userExternalID, err, userID)
	}

	wantCounter := map[string]string{
		"reserved": "1200000000", "reserved:" + checkFeature: "1200000000",
		"count": "1", "count:" + checkFeature: "1",
	}
	if diff := cmp.Diff(wantCounter, harness.counter(t, customer.ID)); diff != "" {
		t.Errorf("counter mismatch (-want +got):\n%s", diff)
	}

	entries := harness.streamEntries(t)
	if len(entries) != 1 {
		t.Fatalf("stream entries = %d, want 1", len(entries))
	}
	wantEntry := map[string]any{
		"decision_id":           response.DecisionID,
		"created_at":            checkStart.Format(time.RFC3339Nano),
		"customer_id":           identifiers.Encode(identifiers.PrefixCustomer, customer.ID),
		"customer_external_id":  checkCustomer,
		"customer_display_name": "",
		"feature":               checkFeature,
		"requested_model":       veoModel,
		"model":                 veoModel,
		"outcome":               "allow",
		"reason":                "no_policy_matched",
		"estimated_cost":        "1.200000000",
		"matched_policy_id":     "",
	}
	if diff := cmp.Diff(wantEntry, entries[0].Values); diff != "" {
		t.Errorf("stream entry mismatch (-want +got):\n%s", diff)
	}

	decided := labeledCounter(t, harness.registry, decisionsMetricName, map[string]string{"environment": "test", "outcome": "allow", "reason": "no_policy_matched"})
	if decided != 1 || histogramSampleCount(t, harness.registry, checkDurationMetric) != 1 {
		t.Errorf("%s = %v and %s samples = %d, want 1 and 1", decisionsMetricName, decided, checkDurationMetric, histogramSampleCount(t, harness.registry, checkDurationMetric))
	}
}

func TestCheckResolvesPoliciesByLevel(t *testing.T) {
	harness := newCheckHarness(t)
	planID := harness.insertDefaultPlan(t, nil, "{}")
	customer := harness.ensureCustomer(t)
	harness.createPolicy(t, map[string]string{
		"action": fmt.Sprintf(`{"outcome": "route", "route_chain": [{"provider": %q, "model": %q}]}`, falProvider, klingModel),
	})

	routed := harness.check(t, videoCheck(veoModel, "8"))
	if routed.Outcome != policies.OutcomeRoute || routed.Model != klingModel {
		t.Errorf("everyone route = %s to %s, want route to %s", routed.Outcome, routed.Model, klingModel)
	}

	planDeny := harness.createPolicy(t, map[string]string{
		"level":   `"plan"`,
		"plan_id": fmt.Sprintf("%q", identifiers.Encode(identifiers.PrefixPlan, planID)),
		"action":  `{"outcome": "deny"}`,
	})
	denied := harness.check(t, videoCheck(veoModel, "8"))
	if denied.Outcome != policies.OutcomeDeny || denied.Reason != policies.ReasonPolicyMatched || *denied.MatchedPolicyID != identifiers.Encode(identifiers.PrefixPolicy, planDeny.ID) {
		t.Errorf("plan deny = %s %s by %v, want deny policy_matched by the plan policy", denied.Outcome, denied.Reason, denied.MatchedPolicyID)
	}

	customerAllow := harness.createPolicy(t, map[string]string{
		"level":       `"customer"`,
		"customer_id": fmt.Sprintf("%q", identifiers.Encode(identifiers.PrefixCustomer, customer.ID)),
	})
	allowed := harness.check(t, videoCheck(veoModel, "8"))
	if allowed.Outcome != policies.OutcomeAllow || allowed.Reason != policies.ReasonPolicyMatched || *allowed.MatchedPolicyID != identifiers.Encode(identifiers.PrefixPolicy, customerAllow.ID) {
		t.Errorf("customer allow = %s %s by %v, want allow policy_matched by the customer policy", allowed.Outcome, allowed.Reason, allowed.MatchedPolicyID)
	}
	row := harness.decision(t, allowed)
	if *row.MatchedPolicyID != customerAllow.ID || *row.MatchedPolicyVersion != 1 {
		t.Errorf("stored matched policy = %v version %v, want the customer policy version 1", row.MatchedPolicyID, row.MatchedPolicyVersion)
	}
}

func TestCheckRoutePicksTheFirstPricedTarget(t *testing.T) {
	harness := newCheckHarness(t)
	routePolicy := harness.createPolicy(t, map[string]string{
		"feature": fmt.Sprintf("%q", checkFeature),
		"action": fmt.Sprintf(`{"outcome": "route", "route_chain": [{"provider": %q, "model": %q}, {"provider": %q, "model": %q}]}`,
			unpricedProvider, unpricedModel, falProvider, klingModel),
	})
	harness.createPolicy(t, map[string]string{
		"feature": fmt.Sprintf("%q", otherCheckFeature),
		"action":  fmt.Sprintf(`{"outcome": "route", "route_chain": [{"provider": %q, "model": %q}]}`, unpricedProvider, unpricedModel),
	})

	routed := harness.check(t, videoCheck(veoModel, "8"))
	want := decisions.CheckResponse{
		DecisionID:      routed.DecisionID,
		Outcome:         policies.OutcomeRoute,
		Reason:          policies.ReasonPolicyMatched,
		Provider:        falProvider,
		Model:           klingModel,
		Overrides:       map[string]policies.OverrideValue{},
		EstimatedCost:   formattedDollars(56),
		ReservedAmount:  money.FormatAmount(dollarAmount(56)),
		EstimateBasis:   decisions.EstimateBasisRequestEstimate,
		CostStatus:      pricing.CostStatusCosted,
		MatchedPolicyID: new(identifiers.Encode(identifiers.PrefixPolicy, routePolicy.ID)),
		FallbackOutcome: policies.OutcomeAllow,
		ExpiresAt:       checkStart.Add(defaultCheckHoldTime),
		Signals:         routed.Signals,
	}
	if diff := cmp.Diff(want, routed); diff != "" {
		t.Errorf("route mismatch (-want +got):\n%s", diff)
	}
	if row := harness.decision(t, routed); row.RequestedModel != veoModel || *row.RequestedEstimatedCostNanos != int64(dollarAmount(120)) {
		t.Errorf("route decision requested %s at %v, want %s at 1.20", row.RequestedModel, row.RequestedEstimatedCostNanos, veoModel)
	}

	request := videoCheck(veoModel, "8")
	request.Feature = otherCheckFeature
	exhausted := harness.check(t, request)
	if exhausted.Outcome != policies.OutcomeDeny || exhausted.Reason != policies.ReasonRouteChainExhausted ||
		exhausted.Model != veoModel || exhausted.ReservedAmount != "0.000000000" || exhausted.EstimateBasis != decisions.EstimateBasisNone {
		t.Errorf("exhausted route = %+v, want deny route_chain_exhausted on %s with nothing reserved", exhausted, veoModel)
	}
	if row := harness.decision(t, exhausted); row.Status != "unreserved" || !row.ExpiresAt.Equal(checkStart) {
		t.Errorf("exhausted decision status %s expires %s, want unreserved at %s", row.Status, row.ExpiresAt, checkStart)
	}
}

func TestCheckRouteSkipsTargetsAboveTheHardAllowance(t *testing.T) {
	harness := newCheckHarness(t)
	harness.insertDefaultPlan(t, new(dollarAmount(100)), "{}")
	harness.createPolicy(t, map[string]string{
		"when":        `{"all": [{"signal": "allowance_remaining", "operator": "gt", "value": "0"}]}`,
		"enforcement": `"hard"`,
		"action": fmt.Sprintf(`{"outcome": "route", "route_chain": [{"provider": %q, "model": %q}, {"provider": %q, "model": %q}]}`,
			falProvider, veoModel, falProvider, klingModel),
	})

	routed := harness.check(t, videoCheck(veoModel, "8"))

	if routed.Outcome != policies.OutcomeRoute || routed.Model != klingModel || routed.ReservedAmount != "0.560000000" {
		t.Errorf("route = %s to %s reserving %s, want the 0.56 kling target because 1.20 exceeds the 1.00 allowance", routed.Outcome, routed.Model, routed.ReservedAmount)
	}
}

func TestCheckCapOverridesRerateTheRequest(t *testing.T) {
	harness := newCheckHarness(t)
	capPolicy := harness.createPolicy(t, map[string]string{"action": `{"outcome": "cap", "overrides": {"duration": "5"}}`})

	capped := harness.check(t, videoCheck(klingModel, "8"))

	if capped.Outcome != policies.OutcomeCap || capped.Reason != policies.ReasonPolicyMatched || *capped.MatchedPolicyID != identifiers.Encode(identifiers.PrefixPolicy, capPolicy.ID) {
		t.Errorf("cap = %s %s, want cap policy_matched", capped.Outcome, capped.Reason)
	}
	wantOverrides := map[string]policies.OverrideValue{"duration": {Type: "string", String: "5"}}
	if diff := cmp.Diff(wantOverrides, capped.Overrides); diff != "" {
		t.Errorf("overrides mismatch (-want +got):\n%s", diff)
	}
	if *capped.EstimatedCost != "0.350000000" || capped.ReservedAmount != "0.350000000" || *capped.Signals.RequestEstimatedCost != "0.560000000" {
		t.Errorf("cap estimated %s reserved %s requested %s, want 0.35, 0.35 and 0.56", *capped.EstimatedCost, capped.ReservedAmount, *capped.Signals.RequestEstimatedCost)
	}
	if row := harness.decision(t, capped); !cmp.Equal(row.Overrides, map[string]any{"duration": "5"}) {
		t.Errorf("stored overrides = %v, want duration 5", row.Overrides)
	}

	aliased := harness.check(t, videoCheck(klingImageAlias, "8"))
	if aliased.Outcome != policies.OutcomeCap || aliased.ReservedAmount != "0.350000000" {
		t.Errorf("cap of the image-to-video alias = %s reserving %s, want cap reserving 0.35", aliased.Outcome, aliased.ReservedAmount)
	}
}

func TestCheckCapLimitDeniesOnceReached(t *testing.T) {
	harness := newCheckHarness(t)
	harness.createPolicy(t, map[string]string{
		"feature": fmt.Sprintf("%q", checkFeature),
		"action":  `{"outcome": "cap", "limit": {"kind": "count", "value": "3"}}`,
	})

	for attempt := 1; attempt <= 3; attempt++ {
		if capped := harness.check(t, videoCheck(klingModel, "5")); capped.Outcome != policies.OutcomeCap {
			t.Fatalf("check %d = %s %s, want cap", attempt, capped.Outcome, capped.Reason)
		}
	}
	denied := harness.check(t, videoCheck(klingModel, "5"))

	if denied.Outcome != policies.OutcomeDeny || denied.Reason != policies.ReasonHardLimitReached || denied.ReservedAmount != "0.000000000" {
		t.Errorf("fourth check = %s %s reserving %s, want deny hard_limit_reached reserving nothing", denied.Outcome, denied.Reason, denied.ReservedAmount)
	}
	if row := harness.decision(t, denied); row.Status != "unreserved" || row.Outcome != "deny" || row.Reason != "hard_limit_reached" {
		t.Errorf("fourth decision = %s %s %s, want deny hard_limit_reached unreserved", row.Outcome, row.Reason, row.Status)
	}
	customer := harness.ensureCustomer(t)
	if count := harness.counter(t, customer.ID)["count:"+checkFeature]; count != "3" {
		t.Errorf("feature count = %s, want 3", count)
	}
}

func TestCheckCapLimitWithoutFeatureCountsEveryFeature(t *testing.T) {
	harness := newCheckHarness(t)
	harness.createPolicy(t, map[string]string{"action": `{"outcome": "cap", "limit": {"kind": "count", "value": "3"}}`})
	imageCheck := videoCheck(klingModel, "5")
	imageCheck.Feature = otherCheckFeature

	for index, request := range []decisions.CheckRequest{videoCheck(klingModel, "5"), imageCheck, videoCheck(klingModel, "5")} {
		if capped := harness.check(t, request); capped.Outcome != policies.OutcomeCap {
			t.Fatalf("check %d = %s %s, want cap", index+1, capped.Outcome, capped.Reason)
		}
	}
	denied := harness.check(t, imageCheck)

	if denied.Outcome != policies.OutcomeDeny || denied.Reason != policies.ReasonHardLimitReached {
		t.Errorf("fourth check = %s %s, want deny hard_limit_reached once 3 checks across both features counted", denied.Outcome, denied.Reason)
	}
	counter := harness.counter(t, harness.ensureCustomer(t).ID)
	if counter["count"] != "3" || counter["count:"+otherCheckFeature] != "1" {
		t.Errorf("counter = %v, want count 3 with 1 for %s", counter, otherCheckFeature)
	}
}

func TestCheckWritesAKnownCustomerUserOnce(t *testing.T) {
	harness := newCheckHarness(t)
	userID := "user-7"
	request := videoCheck(veoModel, "8")
	request.CustomerUserID = &userID

	first := harness.decision(t, harness.check(t, request))
	second := harness.decision(t, harness.check(t, request))

	if inserts := harness.userInserts.Load(); inserts != 1 || *second.CustomerUserID != *first.CustomerUserID {
		t.Errorf("customer user inserts = %d, users %s and %s, want one insert and the same user", inserts, *first.CustomerUserID, *second.CustomerUserID)
	}
}

func TestCheckHardAllowanceHoldsUnderConcurrentChecks(t *testing.T) {
	harness := newCheckHarness(t)
	harness.insertDefaultPlan(t, new(dollarAmount(100)), "{}")
	harness.createPolicy(t, map[string]string{
		"when":        `{"all": [{"signal": "allowance_remaining", "operator": "gt", "value": "0"}]}`,
		"enforcement": `"hard"`,
	})
	customer := harness.ensureCustomer(t)
	request := videoCheck(klingModel, "5")
	request.UsageCeiling = map[string]string{outputSeconds: "5"}

	var waitGroup sync.WaitGroup
	responses := make([]decisions.CheckResponse, 10)
	for index := range responses {
		waitGroup.Go(func() {
			response, err := harness.service.Check(t.Context(), httpapi.EnvironmentTest, request)
			if err != nil {
				t.Errorf("check %d: %v", index, err)
			}
			responses[index] = response
		})
	}
	waitGroup.Wait()

	outcomes := map[string]int{}
	for _, response := range responses {
		outcomes[string(response.Outcome)+" "+string(response.Reason)]++
		if response.Outcome == policies.OutcomeAllow && response.EstimateBasis != decisions.EstimateBasisCeiling {
			t.Errorf("allowed hard check reserved by %s, want ceiling", response.EstimateBasis)
		}
	}
	if diff := cmp.Diff(map[string]int{"allow policy_matched": 2, "deny hard_limit_reached": 8}, outcomes); diff != "" {
		t.Errorf("outcomes mismatch (-want +got):\n%s", diff)
	}
	if reserved := harness.counter(t, customer.ID)["reserved"]; reserved != "700000000" {
		t.Errorf("reserved = %s, want 700000000 within the 1.00 allowance", reserved)
	}
}

func TestCheckUncostedRequests(t *testing.T) {
	harness := newCheckHarness(t)
	request := decisions.CheckRequest{
		CustomerID:    checkCustomer,
		Feature:       checkFeature,
		Provider:      unpricedProvider,
		Model:         unpricedModel,
		UsageEstimate: map[string]string{outputSeconds: "8"},
	}

	allowed := harness.check(t, request)
	if allowed.Outcome != policies.OutcomeAllow || allowed.Reason != policies.ReasonUncostedAllowed || allowed.CostStatus != pricing.CostStatusUncosted ||
		allowed.EstimatedCost != nil || allowed.ReservedAmount != "0.000000000" || allowed.EstimateBasis != decisions.EstimateBasisNone {
		t.Errorf("uncosted without policy = %+v, want allow uncosted_allowed reserving nothing", allowed)
	}
	if row := harness.decision(t, allowed); row.Status != "reserved" || row.EstimatedCostNanos != nil || row.RequestedEstimatedCostNanos != nil {
		t.Errorf("uncosted decision status %s costs %v %v, want reserved without costs", row.Status, row.EstimatedCostNanos, row.RequestedEstimatedCostNanos)
	}

	harness.createPolicy(t, map[string]string{"on_uncosted": `"deny"`})
	denied := harness.check(t, request)
	if denied.Outcome != policies.OutcomeDeny || denied.Reason != policies.ReasonUncostedDenied || denied.MatchedPolicyID == nil {
		t.Errorf("uncosted with on_uncosted deny = %s %s, want deny uncosted_denied", denied.Outcome, denied.Reason)
	}
}

func TestCheckCreatesUnknownCustomerWithTheDefaultPlanAllowance(t *testing.T) {
	harness := newCheckHarness(t)
	harness.insertDefaultPlan(t, new(dollarAmount(500)), "{}")

	response := harness.check(t, videoCheck(veoModel, "8"))

	if response.Signals.CostAllowance != "5.000000000" || response.Signals.AllowanceRemaining != "5.000000000" {
		t.Errorf("signals = %+v, want the default plan's 5.00 allowance", response.Signals)
	}
	customer := harness.ensureCustomer(t)
	if row := harness.decision(t, response); row.CustomerID != customer.ID {
		t.Errorf("decision customer = %s, want the created customer %s", row.CustomerID, customer.ID)
	}
}

func TestCheckEstimateBasis(t *testing.T) {
	harness := newCheckHarness(t)
	tests := []struct {
		name         string
		sampleCount  int
		ceiling      map[string]string
		wantBasis    decisions.EstimateBasis
		wantReserved string
	}{
		{name: "p95 with 50 samples", sampleCount: 50, ceiling: map[string]string{outputSeconds: "10"}, wantBasis: decisions.EstimateBasisP95, wantReserved: "0.900000000"},
		{name: "ceiling with 49 samples", sampleCount: 49, ceiling: map[string]string{outputSeconds: "10"}, wantBasis: decisions.EstimateBasisCeiling, wantReserved: "1.500000000"},
		{name: "estimate without a ceiling", sampleCount: 49, wantBasis: decisions.EstimateBasisRequestEstimate, wantReserved: "1.200000000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness.setUsageEstimate(t, falProvider, veoModel, money.Quantity(6_000_000), test.sampleCount)
			request := videoCheck(veoModel, "8")
			request.UsageCeiling = test.ceiling

			response := harness.check(t, request)

			if response.EstimateBasis != test.wantBasis || response.ReservedAmount != test.wantReserved || *response.EstimatedCost != "1.200000000" {
				t.Errorf("basis %s reserved %s estimated %s, want %s reserving %s and estimated 1.20", response.EstimateBasis, response.ReservedAmount, *response.EstimatedCost, test.wantBasis, test.wantReserved)
			}
		})
	}
}

func TestCheckDenyIncrementsOnlyTheCounts(t *testing.T) {
	harness := newCheckHarness(t)
	harness.createPolicy(t, map[string]string{"action": `{"outcome": "deny"}`})

	denied := harness.check(t, videoCheck(veoModel, "8"))

	if denied.Outcome != policies.OutcomeDeny || denied.ReservedAmount != "0.000000000" || denied.EstimateBasis != decisions.EstimateBasisNone ||
		!denied.ExpiresAt.Equal(checkStart) || *denied.EstimatedCost != "1.200000000" {
		t.Errorf("deny = %+v, want nothing reserved, expiring now, estimated 1.20", denied)
	}
	customer := harness.ensureCustomer(t)
	if diff := cmp.Diff(map[string]string{"count": "1", "count:" + checkFeature: "1"}, harness.counter(t, customer.ID)); diff != "" {
		t.Errorf("counter mismatch (-want +got):\n%s", diff)
	}
	if row := harness.decision(t, denied); row.Status != "unreserved" || row.ReservedNanos != 0 {
		t.Errorf("deny decision status %s reserved %d, want unreserved 0", row.Status, row.ReservedNanos)
	}
	job := jobstest.RequireInserted(t, harness.pool, ledger.RollupRefreshArgs{}, nil)
	want := ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, customer.ID, checkPeriod.Start)
	if diff := cmp.Diff(want, job.Args); diff != "" {
		t.Errorf("rollup refresh job mismatch (-want +got):\n%s", diff)
	}
}

func TestCheckHoldTimeComesFromThePlan(t *testing.T) {
	harness := newCheckHarness(t)
	harness.insertDefaultPlan(t, nil, fmt.Sprintf(`{%q: 30}`, checkFeature))

	held := harness.check(t, videoCheck(veoModel, "8"))
	request := videoCheck(veoModel, "8")
	request.Feature = otherCheckFeature
	defaulted := harness.check(t, request)

	if !held.ExpiresAt.Equal(checkStart.Add(30*time.Second)) || !defaulted.ExpiresAt.Equal(checkStart.Add(defaultCheckHoldTime)) {
		t.Errorf("expiries %s and %s, want 30 seconds for %s and 10 minutes otherwise", held.ExpiresAt, defaulted.ExpiresAt, checkFeature)
	}
}

func TestCheckAnswersCountersUnavailableWithinTheDeadline(t *testing.T) {
	harness := newCheckHarness(t)
	harness.ensureCustomer(t)
	harness.check(t, videoCheck(veoModel, "8"))
	harness.cache.Redis().AddHook(unreachableRedisHook{})

	started := time.Now()
	_, err := harness.service.Check(t.Context(), httpapi.EnvironmentTest, videoCheck(veoModel, "8"))
	elapsed := time.Since(started)

	assertCheckProblem(t, err, "counters_unavailable")
	if elapsed >= 200*time.Millisecond {
		t.Errorf("check took %s, want under 200ms", elapsed)
	}
	if count := harness.decisionCount(t); count != 1 {
		t.Errorf("decisions = %d, want only the first", count)
	}
}

func TestCheckAnswersDatabaseUnavailable(t *testing.T) {
	harness := newCheckHarness(t)
	harness.pool.Close()

	_, err := harness.service.Check(t.Context(), httpapi.EnvironmentTest, videoCheck(veoModel, "8"))

	assertCheckProblem(t, err, "database_unavailable")
}

func TestCheckOfADisconnectedClientOnPostgresLogsNoError(t *testing.T) {
	harness := newCheckHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := harness.service.Check(ctx, httpapi.EnvironmentTest, videoCheck(veoModel, "8"))

	assertCanceledCheck(t, harness, err)
}

func TestCheckOfADisconnectedClientOnRedisLogsNoError(t *testing.T) {
	harness := newCheckHarness(t)
	harness.ensureCustomer(t)
	harness.check(t, videoCheck(veoModel, "8"))
	harness.checkLogs.Reset()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	harness.cache.Redis().AddHook(cancelingRedisHook{cancel: cancel})

	_, err := harness.service.Check(ctx, httpapi.EnvironmentTest, videoCheck(veoModel, "8"))

	assertCanceledCheck(t, harness, err)
}

func TestCheckReleasesTheReservationOfAnUnstoredDecision(t *testing.T) {
	harness := newCheckHarness(t)
	customer := harness.ensureCustomer(t)
	if _, err := harness.pool.Exec(t.Context(), "ALTER TABLE decisions RENAME TO decisions_unavailable"); err != nil {
		t.Fatalf("rename decisions: %v", err)
	}

	_, err := harness.service.Check(t.Context(), httpapi.EnvironmentTest, videoCheck(veoModel, "8"))

	assertCheckProblem(t, err, "database_unavailable")
	counter := harness.counter(t, customer.ID)
	if counter["reserved"] != "0" || counter["reserved:"+checkFeature] != "0" {
		t.Errorf("counter = %v, want the reservation released", counter)
	}
	expiring, err := harness.cache.Redis().ZCard(t.Context(), harness.cache.Key("reservations_expiring")).Result()
	if err != nil || expiring != 0 {
		t.Errorf("reservations_expiring holds %d members, %v, want none", expiring, err)
	}
}

func TestCheckRoute(t *testing.T) {
	harness := newCheckHarness(t)
	runtimeKey := harness.createRuntimeKey(t)

	recorder := harness.post(t, runtimeKey, `{"customer_id": "acme", "feature": "text_to_video", "provider": "fal_ai", "model": "fal-ai/veo3.1/fast", "usage_estimate": {"output_seconds": "8"}}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"outcome":"allow"`) {
		t.Errorf("check = %d %s, want 200 allow", recorder.Code, recorder.Body.String())
	}

	tests := []struct {
		name      string
		body      string
		locations []string
	}{
		{
			name:      "route candidates",
			body:      `{"customer_id": "acme", "feature": "text_to_video", "provider": "fal_ai", "model": "fal-ai/veo3.1/fast", "route_candidates": []}`,
			locations: []string{"body.route_candidates"},
		},
		{
			name: "invalid fields",
			body: fmt.Sprintf(`{"customer_id": "acme", "feature": "Text", "provider": "", "model": "m", "attributes": {%s}, "usage_estimate": {"parsecs": "1"}, "usage_ceiling": {"output_seconds": "-1"}}`,
				manyAttributes(33)),
			locations: []string{"body.feature", "body.provider", "body.attributes", "body.usage_estimate", "body.usage_ceiling.output_seconds"},
		},
		{
			name:      "attribute outside the vocabulary",
			body:      `{"customer_id": "acme", "feature": "text_to_video", "provider": "fal_ai", "model": "m", "attributes": {"resolution": "999p"}}`,
			locations: []string{"body.attributes.resolution"},
		},
		{
			name:      "customer id",
			body:      `{"customer_id": "acme corp", "feature": "text_to_video", "provider": "fal_ai", "model": "m"}`,
			locations: []string{"body.customer_id"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.post(t, runtimeKey, test.body)
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422, body %s", recorder.Code, recorder.Body.String())
			}
			for _, location := range test.locations {
				if !strings.Contains(recorder.Body.String(), `"location":"`+location+`"`) {
					t.Errorf("body %s has no error at %s", recorder.Body.String(), location)
				}
			}
			if got := strings.Count(recorder.Body.String(), `"location"`); got != len(test.locations) {
				t.Errorf("errors = %d, want %d: %s", got, len(test.locations), recorder.Body.String())
			}
		})
	}
	if count := harness.decisionCount(t); count != 1 {
		t.Errorf("decisions = %d, want only the valid check", count)
	}
}

func TestCheckRouteDocumentsOverridesByAttributeName(t *testing.T) {
	harness := newCheckHarness(t)
	recorder := httptest.NewRecorder()

	harness.handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/openapi.json", nil))

	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode openapi document: %v", err)
	}
	description := document.Components.Schemas["CheckResponse"].Properties["overrides"].Description
	for _, wanted := range []string{"by Preburn attribute name", "GET /api/v1/policies/parameter-mappings"} {
		if !strings.Contains(description, wanted) {
			t.Errorf("overrides description = %q, want it to contain %q", description, wanted)
		}
	}
}

func TestNewCheckServiceRejectsDuplicateMetrics(t *testing.T) {
	harness := newCheckHarness(t)
	_, err := decisions.NewCheckService(decisions.CheckDependencies{Registry: harness.registry})
	if err == nil {
		t.Error("NewCheckService registered its metrics twice, want an error")
	}
}
