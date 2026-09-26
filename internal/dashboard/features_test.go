package dashboard_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/policies"
)

func TestFeaturesUniteEverySourceWithItsSources(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	test, live := httpapi.EnvironmentTest, httpapi.EnvironmentLive
	harness.insertUsageEstimate(t, test, textToVideo)
	harness.storePolicy(t, test, "Cap images", pointer(textToImage), policies.StatusActive)
	harness.storePolicy(t, test, "Paused speech", pointer("speech"), policies.StatusDisabled)
	harness.storePolicy(t, test, "Everything", nil, policies.StatusActive)
	harness.storePlan(t, test, "Pro", `{"text_to_video": 30, "voice_clone": 60}`, "active")
	harness.storePlan(t, test, "Legacy", `{"legacy_feature": 10}`, "archived")
	customer := harness.insertCustomer(t, test, "acme", nil, nil)
	for feature, createdAt := range map[string]time.Time{
		"chat":        testNow.AddDate(0, 0, -30),
		"old_feature": testNow.AddDate(0, 0, -30).Add(-time.Microsecond),
		textToVideo:   checkedAt(15),
	} {
		decision := allowedDecision(test, customer, createdAt)
		decision.feature = feature
		harness.storeDecision(t, decision)
	}
	liveCustomer := harness.insertCustomer(t, live, "acme", nil, nil)
	harness.insertUsageEstimate(t, live, "live_estimate")
	harness.storePolicy(t, live, "Live cap", pointer("live_policy"), policies.StatusActive)
	liveDecision := allowedDecision(live, liveCustomer, checkedAt(15))
	liveDecision.feature = "live_decision"
	harness.storeDecision(t, liveDecision)

	page := decodeInto[httpapi.PageBody[dashboard.KnownFeatureResponse]](t, harness.memberGet(t, test, featuresPath))

	want := []dashboard.KnownFeatureResponse{
		{Feature: "chat", Sources: []dashboard.FeatureSource{dashboard.FeatureSourceDecisions}},
		{Feature: textToImage, Sources: []dashboard.FeatureSource{dashboard.FeatureSourcePolicies}},
		{
			Feature: textToVideo,
			Sources: []dashboard.FeatureSource{dashboard.FeatureSourceDecisions, dashboard.FeatureSourcePlanHoldTimes, dashboard.FeatureSourceUsageEstimates},
		},
		{Feature: "voice_clone", Sources: []dashboard.FeatureSource{dashboard.FeatureSourcePlanHoldTimes}},
	}
	if diff := cmp.Diff(want, page.Items); diff != "" {
		t.Errorf("features mismatch (-want +got):\n%s", diff)
	}
	if page.NextCursor != nil {
		t.Errorf("next_cursor = %s, want null", *page.NextCursor)
	}
}

func TestFeaturesOfEmptyEnvironmentIsEmptyList(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)

	page := decodeInto[httpapi.PageBody[dashboard.KnownFeatureResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, featuresPath))

	if page.Items == nil || len(page.Items) != 0 {
		t.Errorf("features = %v, want an empty list", page.Items)
	}
}
