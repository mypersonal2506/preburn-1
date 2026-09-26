package dashboard_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/policies"
)

func TestOnboardingFlagsFlipAsFixturesAreAdded(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	test, live := httpapi.EnvironmentTest, httpapi.EnvironmentLive
	var steps []dashboard.OnboardingResponse
	record := func() {
		t.Helper()
		steps = append(steps, decodeInto[dashboard.OnboardingResponse](t, harness.memberGet(t, test, onboardingPath)))
	}

	record()
	liveCustomer := harness.insertCustomer(t, live, "acme", nil, nil)
	harness.createKey(t, live, apikeys.ScopeRuntime)
	harness.storeDecision(t, allowedDecision(live, liveCustomer, checkedAt(1)))
	harness.storePlan(t, live, "Pro", "{}", "active")
	harness.storePolicy(t, live, "Route video", nil, policies.StatusActive)
	harness.insertRevenue(t, live, liveCustomer, "subscription", dollars(50), septemberStart, octoberStart, septemberStart)
	record()
	harness.createKey(t, test, apikeys.ScopeRuntime)
	record()
	customer := harness.insertCustomer(t, test, "acme", nil, nil)
	harness.storeDecision(t, allowedDecision(test, customer, checkedAt(12)))
	harness.storeDecision(t, allowedDecision(test, customer, checkedAt(9)))
	record()
	harness.storePlan(t, test, "Pro", "{}", "active")
	record()
	harness.storePolicy(t, test, "Route video", nil, policies.StatusActive)
	record()
	harness.insertRevenue(t, test, customer, "subscription", dollars(50), septemberStart, octoberStart, septemberStart)
	record()

	firstCheck := pointer(checkedAt(9))
	want := []dashboard.OnboardingResponse{
		{},
		{},
		{HasAPIKey: true},
		{HasAPIKey: true, FirstCheckAt: firstCheck},
		{HasAPIKey: true, FirstCheckAt: firstCheck, HasPlan: true},
		{HasAPIKey: true, FirstCheckAt: firstCheck, HasPlan: true, HasPolicy: true},
		{HasAPIKey: true, FirstCheckAt: firstCheck, HasPlan: true, HasPolicy: true, HasRevenue: true},
	}
	if diff := cmp.Diff(want, steps); diff != "" {
		t.Errorf("onboarding steps mismatch (-want +got):\n%s", diff)
	}
}

func TestOnboardingCountsOnlyActiveKeysPlansAndPolicies(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	test := httpapi.EnvironmentTest
	key, _, err := harness.apiKeys.Create(t.Context(), test, "Old service", apikeys.ScopeAdmin, nil)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if err := harness.apiKeys.Revoke(t.Context(), test, key.ID); err != nil {
		t.Fatalf("revoke key: %v", err)
	}
	harness.storePlan(t, test, "Legacy", "{}", "archived")
	harness.storePolicy(t, test, "Paused cap", nil, policies.StatusDisabled)
	harness.storePolicy(t, test, "Old route", nil, policies.StatusArchived)

	onboarding := decodeInto[dashboard.OnboardingResponse](t, harness.memberGet(t, test, onboardingPath))

	if diff := cmp.Diff(dashboard.OnboardingResponse{}, onboarding); diff != "" {
		t.Errorf("onboarding mismatch (-want +got):\n%s", diff)
	}
}
