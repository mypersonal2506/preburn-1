package dashboard_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
	"github.com/preburn/preburn/internal/signals"
)

type decisionListFixture struct {
	acme       uuid.UUID
	cedar      uuid.UUID
	routeVideo uuid.UUID
	lossStop   uuid.UUID
	allowAcme  uuid.UUID
	routeAcme  uuid.UUID
	denyCedar  uuid.UUID
	capCedar   uuid.UUID
	firstTie   uuid.UUID
	secondTie  uuid.UUID
	liveAcme   uuid.UUID
}

var capSignals = signals.Response{
	PeriodRevenueNet:     "50.000000000",
	CostAllowance:        "30.000000000",
	CostToDate:           "12.000000000",
	Reserved:             "1.000000000",
	ElapsedFraction:      "0.4000",
	AllowanceRemaining:   "17.000000000",
	Pace:                 "1.0000",
	ProjectedMargin:      "0.4000",
	RequestEstimatedCost: pointer("2.500000000"),
	PeriodDecisionCount:  9,
	Features: map[string]signals.FeatureResponse{
		textToVideo: {CostToDate: "12.000000000", Reserved: "1.000000000", PeriodDecisionCount: 9},
	},
}

func checkedAt(dayOfMonth int) time.Time {
	return day(time.September, dayOfMonth).Add(10 * time.Hour)
}

func seedDecisionList(t *testing.T, harness *harness) decisionListFixture {
	t.Helper()
	test := httpapi.EnvironmentTest
	fixture := decisionListFixture{
		acme:  harness.insertCustomer(t, test, "acme", pointer("Acme Inc"), nil),
		cedar: harness.insertCustomer(t, test, "cedar", nil, nil),
	}
	fixture.routeVideo = harness.storePolicy(t, test, "Route video", nil, policies.StatusActive)
	fixture.lossStop = harness.storePolicy(t, test, "Hard loss stop", nil, policies.StatusActive)

	fixture.allowAcme = harness.storeDecision(t, allowedDecision(test, fixture.acme, checkedAt(10)))

	routed := allowedDecision(test, fixture.acme, checkedAt(11))
	routed.model, routed.outcome, routed.reason, routed.matchedPolicyID = klingPro, policies.OutcomeRoute, policies.ReasonPolicyMatched, &fixture.routeVideo
	routed.estimatedCost, routed.reserved, routed.status = amount(1.5), dollars(1.5), "settled"
	fixture.routeAcme = harness.storeDecision(t, routed)

	denied := allowedDecision(test, fixture.cedar, checkedAt(12))
	denied.feature, denied.outcome, denied.reason, denied.matchedPolicyID = textToImage, policies.OutcomeDeny, policies.ReasonPolicyMatched, &fixture.lossStop
	denied.reserved, denied.status, denied.expiresAt = 0, "unreserved", checkedAt(12)
	fixture.denyCedar = harness.storeDecision(t, denied)

	capped := allowedDecision(test, fixture.cedar, checkedAt(13))
	capped.outcome, capped.reason, capped.matchedPolicyID, capped.matchedPolicyVersion = policies.OutcomeCap, policies.ReasonPolicyMatched, &fixture.routeVideo, pointer(2)
	capped.attributes, capped.overrides = `{"audio":true,"resolution":"1080p"}`, `{"duration":5}`
	capped.customerUserID = pointer(harness.insertCustomerUser(t, test, fixture.cedar, "user-7"))
	capped.signals, capped.requestedCost, capped.estimatedCost, capped.reserved = capSignals, amount(4), amount(2.5), dollars(2.5)
	capped.status, capped.settledAt = "settled", pointer(checkedAt(13).Add(5*time.Minute))
	fixture.capCedar = harness.storeDecision(t, capped)

	fixture.firstTie = harness.storeDecision(t, allowedDecision(test, fixture.acme, checkedAt(14)))
	fixture.secondTie = harness.storeDecision(t, allowedDecision(test, fixture.acme, checkedAt(14)))

	liveCustomer := harness.insertCustomer(t, httpapi.EnvironmentLive, "acme", nil, nil)
	fixture.liveAcme = harness.storeDecision(t, allowedDecision(httpapi.EnvironmentLive, liveCustomer, checkedAt(15)))
	return fixture
}

func decisionIDs(page httpapi.PageBody[dashboard.DecisionResponse]) []string {
	listed := []string{}
	for _, decision := range page.Items {
		listed = append(listed, decision.ID)
	}
	return listed
}

func encodedDecisions(decisionIDs ...uuid.UUID) []string {
	encoded := make([]string, len(decisionIDs))
	for index, decisionID := range decisionIDs {
		encoded[index] = encodedDecision(decisionID)
	}
	return encoded
}

func TestDecisionListFiltersNewestFirst(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	fixture := seedDecisionList(t, harness)

	cases := []struct {
		name  string
		query string
		want  []uuid.UUID
	}{
		{
			name: "every decision of the environment",
			want: []uuid.UUID{fixture.secondTie, fixture.firstTie, fixture.capCedar, fixture.denyCedar, fixture.routeAcme, fixture.allowAcme},
		},
		{name: "outcome", query: "?outcome=deny", want: []uuid.UUID{fixture.denyCedar}},
		{
			name:  "customer",
			query: "?customer_id=" + encodedCustomer(fixture.acme),
			want:  []uuid.UUID{fixture.secondTie, fixture.firstTie, fixture.routeAcme, fixture.allowAcme},
		},
		{name: "feature", query: "?feature=" + textToImage, want: []uuid.UUID{fixture.denyCedar}},
		{name: "policy", query: "?policy_id=" + encodedPolicy(fixture.routeVideo), want: []uuid.UUID{fixture.capCedar, fixture.routeAcme}},
		{name: "customer and outcome", query: "?outcome=cap&customer_id=" + encodedCustomer(fixture.cedar), want: []uuid.UUID{fixture.capCedar}},
		{name: "customer of another environment", query: "?customer_id=" + encodedCustomer(identifiers.New()), want: []uuid.UUID{}},
	}
	for _, testCase := range cases {
		page := decodeInto[httpapi.PageBody[dashboard.DecisionResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, decisionsPath+testCase.query))

		if diff := cmp.Diff(encodedDecisions(testCase.want...), decisionIDs(page)); diff != "" {
			t.Errorf("%s: decisions mismatch (-want +got):\n%s", testCase.name, diff)
		}
		if page.NextCursor != nil {
			t.Errorf("%s: next_cursor = %s, want null", testCase.name, *page.NextCursor)
		}
	}
}

func TestDecisionListPagesThroughTies(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	fixture := seedDecisionList(t, harness)

	var pages [][]string
	target := decisionsPath + "?limit=2"
	for range 3 {
		page := decodeInto[httpapi.PageBody[dashboard.DecisionResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, target))
		pages = append(pages, decisionIDs(page))
		if page.NextCursor == nil {
			break
		}
		target = decisionsPath + "?limit=2&cursor=" + *page.NextCursor
	}

	want := [][]string{
		encodedDecisions(fixture.secondTie, fixture.firstTie),
		encodedDecisions(fixture.capCedar, fixture.denyCedar),
		encodedDecisions(fixture.routeAcme, fixture.allowAcme),
	}
	if diff := cmp.Diff(want, pages); diff != "" {
		t.Errorf("pages mismatch (-want +got):\n%s", diff)
	}
}

func TestDecisionListItemNamesCustomerAndServedModel(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	fixture := seedDecisionList(t, harness)

	page := decodeInto[httpapi.PageBody[dashboard.DecisionResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, decisionsPath+"?outcome=route"))

	want := []dashboard.DecisionResponse{{
		CustomerDecisionResponse: dashboard.CustomerDecisionResponse{
			ID:                encodedDecision(fixture.routeAcme),
			Feature:           textToVideo,
			RequestedProvider: falProvider,
			RequestedModel:    veoModel,
			Provider:          falProvider,
			Model:             klingPro,
			Outcome:           policies.OutcomeRoute,
			Reason:            policies.ReasonPolicyMatched,
			MatchedPolicyID:   pointer(encodedPolicy(fixture.routeVideo)),
			EstimatedCost:     pointer("1.500000000"),
			Status:            "settled",
			CreatedAt:         checkedAt(11),
		},
		CustomerID:          encodedCustomer(fixture.acme),
		CustomerExternalID:  "acme",
		CustomerDisplayName: pointer("Acme Inc"),
	}}
	if diff := cmp.Diff(want, page.Items); diff != "" {
		t.Errorf("routed decision mismatch (-want +got):\n%s", diff)
	}
}

func TestDecisionListRejectsInvalidQuery(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	otherListing, err := httpapi.NewCursor[struct{}]("dashboard_customers").Encode(httpapi.EnvironmentTest, struct{}{})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	seedDecisionList(t, harness)
	testPage := decodeInto[httpapi.PageBody[dashboard.DecisionResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, decisionsPath+"?limit=1"))

	cases := []struct {
		query    string
		code     string
		location []string
	}{
		{query: "?outcome=maybe", code: "validation_failed", location: []string{"query.outcome"}},
		{query: "?customer_id=acme", code: "validation_failed", location: []string{"query.customer_id"}},
		{query: "?policy_id=" + encodedCustomer(identifiers.New()), code: "validation_failed", location: []string{"query.policy_id"}},
		{query: "?limit=101", code: "validation_failed", location: []string{"query.limit"}},
		{query: "?cursor=bogus", code: "invalid_cursor"},
		{query: "?cursor=" + otherListing, code: "invalid_cursor"},
	}
	for _, testCase := range cases {
		assertProblem(t, harness.memberGet(t, httpapi.EnvironmentTest, decisionsPath+testCase.query), http.StatusUnprocessableEntity, testCase.code, testCase.location...)
	}
	assertProblem(t, harness.memberGet(t, httpapi.EnvironmentLive, decisionsPath+"?limit=1&cursor="+*testPage.NextCursor), http.StatusUnprocessableEntity, "invalid_cursor")
}

func TestDecisionDetailHasSignalsLifecycleAndLedgerEntries(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	fixture := seedDecisionList(t, harness)
	test := httpapi.EnvironmentTest
	usage := func(customerID uuid.UUID, cost float64, correctionOf *uuid.UUID) ledgerFixture {
		entry := ledgerFixture{
			environment: test, customerID: customerID, feature: textToVideo, model: veoModel,
			periodStart: septemberStart, periodEnd: octoberStart, correctionOf: correctionOf, occurredAt: checkedAt(13).Add(2 * time.Minute),
		}
		if cost > 0 {
			entry.cost = amount(cost)
		}
		return entry
	}
	reported := harness.insertLedgerEntry(t, usage(fixture.cedar, 0, nil))
	harness.stampLedgerEntry(t, reported, &fixture.capCedar, checkedAt(13).Add(5*time.Minute))
	correction := harness.insertLedgerEntry(t, usage(fixture.cedar, 2.4, &reported))
	harness.stampLedgerEntry(t, correction, nil, checkedAt(15))
	otherDecisionEntry := harness.insertLedgerEntry(t, usage(fixture.cedar, 1, nil))
	harness.stampLedgerEntry(t, otherDecisionEntry, &fixture.denyCedar, checkedAt(13))
	harness.insertLedgerEntry(t, usage(fixture.cedar, 3, nil))

	detail := decodeInto[dashboard.DecisionDetailResponse](t, harness.memberGet(t, test, decisionsPath+"/"+encodedDecision(fixture.capCedar)))

	want := dashboard.DecisionDetailResponse{
		DecisionResponse: dashboard.DecisionResponse{
			CustomerDecisionResponse: dashboard.CustomerDecisionResponse{
				ID:                encodedDecision(fixture.capCedar),
				Feature:           textToVideo,
				RequestedProvider: falProvider,
				RequestedModel:    veoModel,
				Provider:          falProvider,
				Model:             veoModel,
				Outcome:           policies.OutcomeCap,
				Reason:            policies.ReasonPolicyMatched,
				MatchedPolicyID:   pointer(encodedPolicy(fixture.routeVideo)),
				EstimatedCost:     pointer("2.500000000"),
				Status:            "settled",
				CreatedAt:         checkedAt(13),
			},
			CustomerID:         encodedCustomer(fixture.cedar),
			CustomerExternalID: "cedar",
		},
		CustomerUserExternalID: pointer("user-7"),
		Attributes:             pricing.Attributes{"audio": pricing.BooleanAttribute(true), "resolution": pricing.StringAttribute("1080p")},
		Overrides:              pricing.Attributes{"duration": pricing.IntegerAttribute(5)},
		MatchedPolicyVersion:   pointer(int64(2)),
		RequestedEstimatedCost: pointer("4.000000000"),
		ReservedAmount:         "2.500000000",
		EstimateBasis:          decisions.EstimateBasisCeiling,
		Signals:                capSignals,
		PeriodStart:            septemberStart,
		PeriodEnd:              octoberStart,
		ExpiresAt:              checkedAt(13).Add(10 * time.Minute),
		SettledAt:              pointer(checkedAt(13).Add(5 * time.Minute)),
		LedgerEntries: []dashboard.DecisionLedgerEntryResponse{
			{
				ID: encodedLedgerEntry(reported), Provider: falProvider, Model: veoModel, Usage: map[string]string{"output_seconds": "8"},
				CostStatus: pricing.CostStatusUncosted, OccurredAt: checkedAt(13).Add(2 * time.Minute), CreatedAt: checkedAt(13).Add(5 * time.Minute),
			},
			{
				ID: encodedLedgerEntry(correction), Provider: falProvider, Model: veoModel, Usage: map[string]string{"output_seconds": "8"},
				Cost: pointer("2.400000000"), CostStatus: pricing.CostStatusCosted, CorrectionOf: pointer(encodedLedgerEntry(reported)),
				OccurredAt: checkedAt(13).Add(2 * time.Minute), CreatedAt: checkedAt(15),
			},
		},
	}
	sameAttribute := cmp.Comparer(func(left, right pricing.AttributeValue) bool { return left == right })
	if diff := cmp.Diff(want, detail, sameAttribute); diff != "" {
		t.Errorf("decision detail mismatch (-want +got):\n%s", diff)
	}
}

func TestDecisionDetailOfUnreportedDecisionHasNoLedgerEntries(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	fixture := seedDecisionList(t, harness)

	detail := decodeInto[dashboard.DecisionDetailResponse](t, harness.memberGet(t, httpapi.EnvironmentTest, decisionsPath+"/"+encodedDecision(fixture.allowAcme)))

	if detail.LedgerEntries == nil || len(detail.LedgerEntries) != 0 || detail.SettledAt != nil || detail.CustomerUserExternalID != nil {
		t.Errorf("ledger_entries=%v settled_at=%v customer_user_external_id=%v, want an empty list and nulls",
			detail.LedgerEntries, detail.SettledAt, detail.CustomerUserExternalID)
	}
	if len(detail.Attributes) != 0 || len(detail.Overrides) != 0 || detail.MatchedPolicyVersion != nil {
		t.Errorf("attributes=%v overrides=%v matched_policy_version=%v, want empty maps and null", detail.Attributes, detail.Overrides, detail.MatchedPolicyVersion)
	}
}

func TestDecisionDetailIsNotFoundOutsideItsEnvironment(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	fixture := seedDecisionList(t, harness)

	for _, decisionID := range []string{encodedDecision(fixture.liveAcme), encodedDecision(identifiers.New()), "dec_bogus", encodedCustomer(fixture.acme)} {
		assertProblem(t, harness.memberGet(t, httpapi.EnvironmentTest, decisionsPath+"/"+decisionID), http.StatusNotFound, "not_found")
	}
	assertStatus(t, harness.memberGet(t, httpapi.EnvironmentLive, decisionsPath+"/"+encodedDecision(fixture.liveAcme)), http.StatusOK)
}

func TestDecisionDetailDocumentsOverridesByAttributeName(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)

	document := decodeInto[openAPIDocument](t, harness.serve(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/openapi.json", nil)))

	description, _ := document.Components.Schemas["DecisionDetailResponse"].Properties["overrides"]["description"].(string)
	for _, wanted := range []string{"by Preburn attribute name", "GET /api/v1/policies/parameter-mappings"} {
		if !strings.Contains(description, wanted) {
			t.Errorf("overrides description = %q, want it to contain %q", description, wanted)
		}
	}
}

func TestDecisionActivityRoutesAdmitOnlySessions(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	fixture := seedDecisionList(t, harness)
	runtimeSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	adminSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)

	for _, target := range []string{decisionsPath, decisionsPath + "/" + encodedDecision(fixture.allowAcme), eventsPath, onboardingPath, featuresPath} {
		assertProblem(t, harness.bearerGet(t, runtimeSecret, target), http.StatusForbidden, "scope_forbidden")
		assertProblem(t, harness.bearerGet(t, adminSecret, target), http.StatusForbidden, "scope_forbidden")
		assertStatus(t, harness.memberGet(t, httpapi.EnvironmentTest, target), http.StatusOK)
	}
}
