package dashboard_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
)

type customerListFixture struct {
	pro     uuid.UUID
	starter uuid.UUID
	alpha   uuid.UUID
}

func seedCustomerList(t *testing.T, harness *harness) customerListFixture {
	t.Helper()
	test := httpapi.EnvironmentTest
	fixture := customerListFixture{
		pro:     harness.insertPlan(t, test, "Pro", 4000, nil),
		starter: harness.insertPlan(t, test, "Starter", 0, amount(10)),
	}
	type listed struct {
		externalID  string
		displayName *string
		planID      uuid.UUID
		revenue     float64
		settled     float64
	}
	for _, customer := range []listed{
		{externalID: "alpha", displayName: pointer("Alpha Studio"), planID: fixture.pro, revenue: 100, settled: 30},
		{externalID: "bravo", planID: fixture.pro, revenue: 100, settled: 50},
		{externalID: "charlie", planID: fixture.pro, revenue: 50, settled: 45},
		{externalID: "delta", planID: fixture.pro, settled: 5},
		{externalID: "echo", planID: fixture.starter, settled: 2},
	} {
		customerID := harness.insertCustomer(t, test, customer.externalID, customer.displayName, &customer.planID)
		harness.insertRollup(t, test, customerID, septemberStart, octoberStart, dollars(customer.revenue))
		harness.setSettled(t, test, customerID, dollars(customer.settled))
		if customer.externalID == "alpha" {
			fixture.alpha = customerID
		}
	}
	harness.insertCustomerWithStatus(t, test, "foxtrot", nil, &fixture.pro, "archived")
	harness.insertCustomer(t, httpapi.EnvironmentLive, "golf", nil, nil)
	return fixture
}

func listExternalIDs(t *testing.T, harness *harness, query string) ([]string, *string) {
	t.Helper()
	page := decodeInto[httpapi.PageBody[dashboard.CustomerMarginResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, customersPath+query))
	externalIDs := make([]string, 0, len(page.Items))
	for _, customer := range page.Items {
		externalIDs = append(externalIDs, customer.ExternalID)
	}
	return externalIDs, page.NextCursor
}

func TestCustomerListSortsWithRevenueLessCustomersLast(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	seedCustomerList(t, harness)

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "default margin ascending", query: "", want: []string{"charlie", "bravo", "alpha", "delta", "echo"}},
		{name: "margin descending", query: "?sort=margin&direction=descending", want: []string{"alpha", "bravo", "charlie", "delta", "echo"}},
		{name: "pace ascending", query: "?sort=pace", want: []string{"alpha", "bravo", "charlie", "echo", "delta"}},
		{name: "pace descending", query: "?sort=pace&direction=descending", want: []string{"charlie", "bravo", "alpha", "delta", "echo"}},
		{name: "cost ascending", query: "?sort=cost", want: []string{"alpha", "charlie", "bravo", "echo", "delta"}},
		{name: "cost descending", query: "?sort=cost&direction=descending", want: []string{"bravo", "charlie", "alpha", "delta", "echo"}},
		{name: "revenue ascending", query: "?sort=revenue", want: []string{"charlie", "alpha", "bravo", "delta", "echo"}},
		{name: "revenue descending", query: "?sort=revenue&direction=descending", want: []string{"alpha", "bravo", "charlie", "delta", "echo"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, nextCursor := listExternalIDs(t, harness, test.query)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("order mismatch (-want +got):\n%s", diff)
			}
			if nextCursor != nil {
				t.Errorf("next_cursor = %s, want null", *nextCursor)
			}
		})
	}
}

func TestCustomerListFiltersAndSearches(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	fixture := seedCustomerList(t, harness)
	starterID := identifiers.Encode(identifiers.PrefixPlan, fixture.starter)

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "paying", query: "?revenue_filter=paying", want: []string{"charlie", "bravo", "alpha"}},
		{name: "free", query: "?revenue_filter=free", want: []string{"delta", "echo"}},
		{name: "plan", query: "?plan_id=" + starterID, want: []string{"echo"}},
		{name: "unknown plan", query: "?plan_id=" + identifiers.Encode(identifiers.PrefixPlan, identifiers.New()), want: []string{}},
		{name: "display name", query: "?search=studio", want: []string{"alpha"}},
		{name: "external id", query: "?search=HAR", want: []string{"charlie"}},
		{name: "paying search", query: "?revenue_filter=paying&search=a", want: []string{"charlie", "bravo", "alpha"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := listExternalIDs(t, harness, test.query)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("customers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCustomerListReturnsMarginsAndPace(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	fixture := seedCustomerList(t, harness)

	page := decodeInto[httpapi.PageBody[dashboard.CustomerMarginResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, customersPath+"?search=alpha"))

	want := []dashboard.CustomerMarginResponse{{
		ID:           encodedCustomer(fixture.alpha),
		ExternalID:   "alpha",
		DisplayName:  pointer("Alpha Studio"),
		PlanID:       encodedPlan(fixture.pro),
		PlanName:     pointer("Pro"),
		TargetMargin: pointer("0.4000"),
		Revenue:      "100.000000000",
		Cost:         "30.000000000",
		Margin:       pointer("0.7000"),
		Pace:         "0.9677",
		PeriodStart:  septemberStart,
		PeriodEnd:    octoberStart,
	}}
	if diff := cmp.Diff(want, page.Items); diff != "" {
		t.Errorf("customer mismatch (-want +got):\n%s", diff)
	}
	delta := decodeInto[httpapi.PageBody[dashboard.CustomerMarginResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, customersPath+"?search=delta"))
	if delta.Items[0].Margin != nil || delta.Items[0].Pace != "inf" {
		t.Errorf("delta margin=%v pace=%s, want null margin and inf pace", delta.Items[0].Margin, delta.Items[0].Pace)
	}
}

func TestCustomerListPagesInSortOrder(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	seedCustomerList(t, harness)

	var pages [][]string
	query := "?limit=2"
	for {
		externalIDs, nextCursor := listExternalIDs(t, harness, query)
		pages = append(pages, externalIDs)
		if nextCursor == nil {
			break
		}
		query = "?limit=2&cursor=" + url.QueryEscape(*nextCursor)
	}

	want := [][]string{{"charlie", "bravo"}, {"alpha", "delta"}, {"echo"}}
	if diff := cmp.Diff(want, pages); diff != "" {
		t.Errorf("pages mismatch (-want +got):\n%s", diff)
	}
}

func TestCustomerListRejectsInvalidQuery(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	seedCustomerList(t, harness)
	_, marginCursor := listExternalIDs(t, harness, "?limit=1")
	longSearch := make([]byte, 201)
	for index := range longSearch {
		longSearch[index] = 'a'
	}

	for _, test := range []struct {
		query     string
		code      string
		locations []string
	}{
		{query: "?sort=name", code: "validation_failed", locations: []string{"query.sort"}},
		{query: "?direction=up", code: "validation_failed", locations: []string{"query.direction"}},
		{query: "?revenue_filter=rich", code: "validation_failed", locations: []string{"query.revenue_filter"}},
		{query: "?plan_id=plan-1", code: "validation_failed", locations: []string{"query.plan_id"}},
		{query: "?search=" + string(longSearch), code: "validation_failed", locations: []string{"query.search"}},
		{query: "?limit=500", code: "validation_failed", locations: []string{"query.limit"}},
		{query: "?sort=pace&cursor=" + url.QueryEscape(*marginCursor), code: "invalid_cursor"},
		{query: "?cursor=bogus", code: "invalid_cursor"},
	} {
		assertProblem(t, harness.memberGet(t, httpapi.EnvironmentTest, customersPath+test.query), http.StatusUnprocessableEntity, test.code, test.locations...)
	}
	assertProblem(t, harness.memberGet(t, httpapi.EnvironmentLive, customersPath+"?limit=1&cursor="+url.QueryEscape(*marginCursor)), http.StatusUnprocessableEntity, "invalid_cursor")
}

func TestCustomerDetailSpansPeriodsWithUsageAndDecisions(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	test := httpapi.EnvironmentTest
	pro := harness.insertPlan(t, test, "Pro", 4000, nil)
	acme := harness.insertCustomer(t, test, "acme", pointer("Acme Inc"), &pro)
	harness.insertRevenue(t, test, acme, "subscription", dollars(100), augustStart, septemberStart, augustStart)
	harness.insertRevenue(t, test, acme, "subscription", dollars(100), septemberStart, octoberStart, septemberStart)
	entry := func(feature, model string, cost *money.Amount, periodStart, periodEnd time.Time) ledgerFixture {
		return ledgerFixture{
			environment: test, customerID: acme, feature: feature, model: model, cost: cost,
			periodStart: periodStart, periodEnd: periodEnd, occurredAt: periodStart.Add(time.Hour),
		}
	}
	harness.insertLedgerEntry(t, entry(textToVideo, veoModel, amount(70), augustStart, septemberStart))
	harness.insertLedgerEntry(t, entry(textToVideo, veoModel, amount(30), septemberStart, octoberStart))
	harness.insertLedgerEntry(t, entry(textToVideo, klingModel, amount(10), septemberStart, octoberStart))
	harness.insertLedgerEntry(t, entry(textToImage, fluxModel, amount(5), septemberStart, octoberStart))
	harness.insertLedgerEntry(t, entry(textToImage, fluxModel, nil, septemberStart, octoberStart))
	policyID := harness.insertPolicy(t, test, "Route video", 1, augustStart, augustStart)
	var decisionIDs []uuid.UUID
	for index := range 12 {
		decisionIDs = append(decisionIDs, harness.insertDecision(t, decisionFixture{
			environment: test, customerID: acme, outcome: "route", reason: "policy_matched", matchedPolicyID: &policyID,
			requestedCost: amount(5), estimatedCost: amount(1.5),
			periodStart: septemberStart, periodEnd: octoberStart, createdAt: septemberStart.Add(time.Duration(index) * time.Hour),
		}))
	}
	harness.refreshRollups(t, test, acme, augustStart)
	harness.refreshRollups(t, test, acme, septemberStart)
	harness.setSettled(t, test, acme, dollars(45))

	detail := decodeInto[dashboard.CustomerDetailResponse](t, harness.memberGet(t, test, customersPath+"/"+encodedCustomer(acme)))

	wantHistory := []dashboard.CustomerPeriodResponse{
		{
			PeriodStart: septemberStart, PeriodEnd: octoberStart, Revenue: "100.000000000", Cost: "45.000000000", Margin: pointer("0.5500"),
			UncostedCount: 1, DecisionCounts: dashboard.DecisionCountsResponse{Route: 12},
		},
		{
			PeriodStart: augustStart, PeriodEnd: septemberStart, Revenue: "100.000000000", Cost: "70.000000000", Margin: pointer("0.3000"),
			DecisionCounts: dashboard.DecisionCountsResponse{},
		},
	}
	wantUsage := []dashboard.CustomerUsageResponse{
		{Feature: textToVideo, Provider: falProvider, Model: veoModel, RequestCount: 1, Cost: "30.000000000"},
		{Feature: textToVideo, Provider: falProvider, Model: klingModel, RequestCount: 1, Cost: "10.000000000"},
		{Feature: textToImage, Provider: falProvider, Model: fluxModel, RequestCount: 2, Cost: "5.000000000", UncostedCount: 1},
	}
	if detail.ID != encodedCustomer(acme) || detail.ExternalID != "acme" || *detail.DisplayName != "Acme Inc" || detail.Status != customers.Status("active") {
		t.Errorf("customer = %s %s %v %s, want acme", detail.ID, detail.ExternalID, detail.DisplayName, detail.Status)
	}
	if *detail.PlanID != *encodedPlan(pro) || *detail.PlanName != "Pro" || *detail.TargetMargin != "0.4000" {
		t.Errorf("plan = %v %v %v, want Pro at 0.4000", detail.PlanID, detail.PlanName, detail.TargetMargin)
	}
	if !detail.PeriodStart.Equal(septemberStart) || !detail.PeriodEnd.Equal(octoberStart) {
		t.Errorf("period = %s to %s, want September", detail.PeriodStart, detail.PeriodEnd)
	}
	if detail.Signals.PeriodRevenueNet != "100.000000000" || detail.Signals.CostToDate != "45.000000000" || detail.Signals.Pace != "1.4516" {
		t.Errorf("signals = %+v, want revenue 100, cost 45 and pace 1.4516", detail.Signals)
	}
	if diff := cmp.Diff(wantHistory, detail.History); diff != "" {
		t.Errorf("history mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantUsage, detail.Usage); diff != "" {
		t.Errorf("usage mismatch (-want +got):\n%s", diff)
	}
	if len(detail.RecentDecisions) != 10 {
		t.Fatalf("recent decisions = %d, want 10", len(detail.RecentDecisions))
	}
	wantNewest := dashboard.CustomerDecisionResponse{
		ID:                identifiers.Encode(identifiers.PrefixDecision, decisionIDs[11]),
		Feature:           textToVideo,
		RequestedProvider: falProvider,
		RequestedModel:    veoModel,
		Provider:          falProvider,
		Model:             klingModel,
		Outcome:           policies.OutcomeRoute,
		Reason:            policies.ReasonPolicyMatched,
		MatchedPolicyID:   pointer(encodedPolicy(policyID)),
		EstimatedCost:     pointer("1.500000000"),
		Status:            "settled",
		CreatedAt:         septemberStart.Add(11 * time.Hour),
	}
	if diff := cmp.Diff(wantNewest, detail.RecentDecisions[0]); diff != "" {
		t.Errorf("newest decision mismatch (-want +got):\n%s", diff)
	}
	oldestListed := identifiers.Encode(identifiers.PrefixDecision, decisionIDs[2])
	if detail.RecentDecisions[9].ID != oldestListed {
		t.Errorf("tenth decision = %s, want %s", detail.RecentDecisions[9].ID, oldestListed)
	}
}

func TestCustomerDetailStaysInEnvironment(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	liveCustomer := harness.insertCustomer(t, httpapi.EnvironmentLive, "acme", nil, nil)

	assertProblem(t, harness.memberGet(t, httpapi.EnvironmentTest, customersPath+"/"+encodedCustomer(liveCustomer)), http.StatusNotFound, "not_found")
	assertProblem(t, harness.memberGet(t, httpapi.EnvironmentTest, customersPath+"/cust_bogus"), http.StatusNotFound, "not_found")
	detail := decodeInto[dashboard.CustomerDetailResponse](t, harness.memberGet(t, httpapi.EnvironmentLive, customersPath+"/"+encodedCustomer(liveCustomer)))
	if detail.PlanID != nil || detail.History == nil || detail.Usage == nil || detail.RecentDecisions == nil {
		t.Errorf("detail = %+v, want no plan and empty lists", detail)
	}
}
