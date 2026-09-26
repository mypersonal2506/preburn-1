package dashboard_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/policies"
)

type overviewFixture struct {
	pro           uuid.UUID
	starter       uuid.UUID
	team          uuid.UUID
	acme          uuid.UUID
	cedar         uuid.UUID
	lumber        uuid.UUID
	tallOak       uuid.UUID
	lossStop      uuid.UUID
	routeVideo    uuid.UUID
	capAudio      uuid.UUID
	oldPolicies   []uuid.UUID
	lossStopAt    time.Time
	routeVideoAt  time.Time
	lossStopSince time.Time
}

type dailyValues struct {
	revenue float64
	cost    float64
}

type overviewSummary struct {
	Revenue        string
	Cost           string
	Margin         *string
	TargetMargin   *string
	PlanMargins    []dashboard.PlanMarginResponse
	LossCustomers  []string
	CostAvoided    dashboard.CostAvoidedResponse
	DecisionCounts dashboard.DecisionCountsResponse
	UncostedCount  int64
	Attention      dashboard.AttentionResponse
	DailyCount     int
	FirstDay       string
	LastDay        string
}

type openAPIDocument struct {
	Components struct {
		Schemas map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
}

func seedOverview(t *testing.T, harness *harness) overviewFixture {
	t.Helper()
	test := httpapi.EnvironmentTest
	fixture := overviewFixture{
		pro:          harness.insertPlan(t, test, "Pro", 4000, nil),
		starter:      harness.insertPlan(t, test, "Starter", 0, amount(5)),
		team:         harness.insertPlan(t, test, "Team", 6000, nil),
		lossStopAt:   day(time.September, 10),
		routeVideoAt: day(time.September, 7),
	}
	fixture.acme = harness.insertCustomer(t, test, "acme", pointer("Acme Inc"), &fixture.pro)
	fixture.cedar = harness.insertCustomer(t, test, "cedar", nil, &fixture.team)
	fixture.lumber = harness.insertCustomer(t, test, "lumber", nil, &fixture.starter)
	fixture.tallOak = harness.insertCustomer(t, test, "tall-oak", nil, nil)

	fixture.lossStopSince = day(time.August, 1)
	fixture.lossStop = harness.insertPolicy(t, test, "Hard loss stop", 3, fixture.lossStopSince, day(time.September, 12))
	fixture.routeVideo = harness.insertPolicy(t, test, "Route video", 1, day(time.September, 3), day(time.September, 3))
	fixture.capAudio = harness.insertPolicy(t, test, "Cap audio", 2, day(time.July, 1), day(time.July, 2))
	for index := range 3 {
		changedAt := day(time.June, index+1)
		fixture.oldPolicies = append(fixture.oldPolicies, harness.insertPolicy(t, test, "Old policy "+string(rune('A'+index)), 1, changedAt, changedAt))
	}
	harness.insertPolicy(t, httpapi.EnvironmentLive, "Live policy", 1, day(time.September, 15), day(time.September, 15))

	september := func(customerID uuid.UUID, cost *money.Amount, occurredAt time.Time) ledgerFixture {
		return ledgerFixture{
			environment: test, customerID: customerID, feature: textToVideo, model: veoModel, cost: cost,
			periodStart: septemberStart, periodEnd: octoberStart, occurredAt: occurredAt,
		}
	}
	august := func(customerID uuid.UUID, cost *money.Amount, occurredAt time.Time) ledgerFixture {
		entry := september(customerID, cost, occurredAt)
		entry.periodStart, entry.periodEnd = augustStart, septemberStart
		return entry
	}

	harness.insertRevenue(t, test, fixture.acme, "subscription", dollars(100), septemberStart, octoberStart, septemberStart)
	harness.insertRevenue(t, test, fixture.acme, "refund", dollars(10), septemberStart, octoberStart, day(time.September, 5))
	harness.insertLedgerEntry(t, september(fixture.acme, amount(30), day(time.September, 2)))
	harness.insertLedgerEntry(t, september(fixture.acme, amount(20), day(time.September, 10)))
	harness.insertLedgerEntry(t, september(fixture.acme, nil, day(time.September, 11)))
	corrected := harness.insertLedgerEntry(t, september(fixture.acme, nil, day(time.September, 12)))
	correction := september(fixture.acme, amount(4), day(time.September, 12))
	correction.correctionOf = &corrected
	harness.insertLedgerEntry(t, correction)
	harness.insertRevenue(t, test, fixture.acme, "subscription", dollars(100), augustStart, septemberStart, augustStart)
	harness.insertLedgerEntry(t, august(fixture.acme, amount(70), day(time.August, 10)))
	harness.insertLedgerEntry(t, august(fixture.acme, amount(5), day(time.August, 20)))

	harness.insertRevenue(t, test, fixture.cedar, "subscription", dollars(50), septemberStart, octoberStart, septemberStart)
	harness.insertLedgerEntry(t, september(fixture.cedar, amount(40), day(time.September, 3)))
	harness.insertRevenue(t, test, fixture.cedar, "subscription", dollars(50), juneStart, julyStart, juneStart)
	june := september(fixture.cedar, amount(10), day(time.June, 5))
	june.periodStart, june.periodEnd = juneStart, julyStart
	harness.insertLedgerEntry(t, june)

	harness.insertLedgerEntry(t, september(fixture.lumber, amount(3), day(time.September, 4)))

	tallOakDay := day(time.September, 6)
	harness.insertRevenue(t, test, fixture.tallOak, "adjustment", dollars(10), tallOakDay, tallOakDay, tallOakDay)
	harness.insertLedgerEntry(t, september(fixture.tallOak, amount(12), day(time.September, 7)))

	decision := func(customerID uuid.UUID, outcome string, policyID *uuid.UUID, requested, estimated *money.Amount, createdAt time.Time) decisionFixture {
		reason := "policy_matched"
		if policyID == nil {
			reason = "no_policy_matched"
		}
		return decisionFixture{
			environment: test, customerID: customerID, outcome: outcome, reason: reason, matchedPolicyID: policyID,
			requestedCost: requested, estimatedCost: estimated,
			periodStart: septemberStart, periodEnd: octoberStart, createdAt: createdAt,
		}
	}
	harness.insertDecision(t, decision(fixture.acme, "allow", nil, amount(1), amount(1), day(time.September, 2)))
	harness.insertDecision(t, decision(fixture.acme, "route", &fixture.routeVideo, amount(5), amount(1.5), day(time.September, 9)))
	harness.insertDecision(t, decision(fixture.acme, "deny", &fixture.lossStop, amount(2), nil, fixture.lossStopAt))
	harness.insertDecision(t, decision(fixture.cedar, "cap", &fixture.routeVideo, amount(4), amount(1), day(time.September, 5)))
	harness.insertDecision(t, decision(fixture.cedar, "route", &fixture.routeVideo, amount(1), amount(2), day(time.September, 6)))
	harness.insertDecision(t, decision(fixture.cedar, "route", &fixture.routeVideo, nil, nil, fixture.routeVideoAt))
	augustDeny := decision(fixture.acme, "deny", &fixture.lossStop, amount(7), nil, day(time.August, 20))
	augustDeny.periodStart, augustDeny.periodEnd = augustStart, septemberStart
	harness.insertDecision(t, augustDeny)

	liveCustomer := harness.insertCustomer(t, httpapi.EnvironmentLive, "acme", nil, nil)
	harness.insertRevenue(t, httpapi.EnvironmentLive, liveCustomer, "subscription", dollars(1000), septemberStart, octoberStart, septemberStart)
	liveEntry := september(liveCustomer, amount(1), day(time.September, 2))
	liveEntry.environment = httpapi.EnvironmentLive
	harness.insertLedgerEntry(t, liveEntry)
	liveDeny := decision(liveCustomer, "deny", nil, amount(100), nil, day(time.September, 10))
	liveDeny.environment = httpapi.EnvironmentLive
	harness.insertDecision(t, liveDeny)

	harness.refreshRollups(t, test, fixture.acme, augustStart)
	harness.refreshRollups(t, test, fixture.acme, septemberStart)
	harness.refreshRollups(t, test, fixture.cedar, juneStart)
	harness.refreshRollups(t, test, fixture.cedar, septemberStart)
	harness.refreshRollups(t, test, fixture.lumber, septemberStart)
	harness.refreshRollups(t, test, fixture.tallOak, septemberStart)
	harness.refreshRollups(t, httpapi.EnvironmentLive, liveCustomer, septemberStart)

	harness.setSettled(t, test, fixture.acme, dollars(60))
	harness.setSettled(t, test, fixture.cedar, dollars(15))
	harness.setSettled(t, test, fixture.lumber, dollars(3))
	harness.setSettled(t, test, fixture.tallOak, dollars(12))
	harness.setDroppedReports(t, test, day(time.September, 16), 3)
	harness.setDroppedReports(t, test, day(time.September, 10), 2)
	harness.setDroppedReports(t, test, day(time.September, 9), 5)
	harness.setDroppedReports(t, httpapi.EnvironmentLive, day(time.September, 16), 50)
	return fixture
}

func dailySeries(first time.Time, count int, values map[time.Time]dailyValues) []dashboard.DailyTotalsResponse {
	series := make([]dashboard.DailyTotalsResponse, count)
	for index := range count {
		date := first.AddDate(0, 0, index)
		series[index] = dashboard.DailyTotalsResponse{
			Date:    date.Format(time.DateOnly),
			Revenue: money.FormatAmount(dollars(values[date].revenue)),
			Cost:    money.FormatAmount(dollars(values[date].cost)),
		}
	}
	return series
}

func summarizeOverview(overview dashboard.OverviewResponse) overviewSummary {
	summary := overviewSummary{
		Revenue:        overview.Revenue,
		Cost:           overview.Cost,
		Margin:         overview.Margin,
		TargetMargin:   overview.TargetMargin,
		PlanMargins:    overview.PlanMargins,
		CostAvoided:    overview.CostAvoided,
		DecisionCounts: overview.DecisionCounts,
		UncostedCount:  overview.UncostedCount,
		Attention:      overview.Attention,
		DailyCount:     len(overview.Daily),
		FirstDay:       overview.Daily[0].Date,
		LastDay:        overview.Daily[len(overview.Daily)-1].Date,
	}
	for _, customer := range overview.LossCustomers {
		summary.LossCustomers = append(summary.LossCustomers, customer.ExternalID+" "+customer.Margin)
	}
	return summary
}

func planMargin(planID uuid.UUID, name string, mode plans.Mode, target *string, revenue, cost string, margin *string, belowTarget bool) dashboard.PlanMarginResponse {
	return dashboard.PlanMarginResponse{
		PlanID:        encodedPlan(planID),
		Name:          &name,
		Mode:          &mode,
		TargetMargin:  target,
		CustomerCount: 1,
		Revenue:       revenue,
		Cost:          cost,
		Margin:        margin,
		BelowTarget:   belowTarget,
	}
}

func TestOverviewOfCurrentPeriods(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	fixture := seedOverview(t, harness)

	overview := decodeInto[dashboard.OverviewResponse](t, harness.memberGet(t, httpapi.EnvironmentTest, overviewPath))

	want := dashboard.OverviewResponse{
		Period:       dashboard.OverviewPeriodCurrent,
		Revenue:      "150.000000000",
		Cost:         "109.000000000",
		Margin:       pointer("0.2733"),
		TargetMargin: pointer("0.4714"),
		Daily: dailySeries(septemberStart, 16, map[time.Time]dailyValues{
			day(time.September, 1):  {revenue: 150},
			day(time.September, 2):  {cost: 30},
			day(time.September, 3):  {cost: 40},
			day(time.September, 4):  {cost: 3},
			day(time.September, 5):  {revenue: -10},
			day(time.September, 6):  {revenue: 10},
			day(time.September, 7):  {cost: 12},
			day(time.September, 10): {cost: 20},
			day(time.September, 12): {cost: 4},
		}),
		PlanMargins: []dashboard.PlanMarginResponse{
			planMargin(fixture.pro, "Pro", plans.ModeMarginTarget, pointer("0.4000"), "90.000000000", "54.000000000", pointer("0.4000"), false),
			planMargin(fixture.starter, "Starter", plans.ModeFixedAllowance, nil, "0.000000000", "3.000000000", nil, false),
			planMargin(fixture.team, "Team", plans.ModeMarginTarget, pointer("0.6000"), "50.000000000", "40.000000000", pointer("0.2000"), true),
			{CustomerCount: 1, Revenue: "10.000000000", Cost: "12.000000000", Margin: pointer("-0.2000")},
		},
		LossCustomers: []dashboard.LossCustomerResponse{
			{
				ID: encodedCustomer(fixture.tallOak), ExternalID: "tall-oak",
				Revenue: "10.000000000", Cost: "12.000000000", Margin: "-0.2000",
			},
		},
		CostAvoided: dashboard.CostAvoidedResponse{
			Total: "8.500000000", Denied: "2.000000000", Routed: "3.500000000", Capped: "3.000000000", ChangedDecisionCount: 5,
		},
		DecisionCounts: dashboard.DecisionCountsResponse{Allow: 1, Route: 3, Cap: 1, Deny: 1},
		UncostedCount:  1,
		Attention:      dashboard.AttentionResponse{PlansBelowTarget: 1, CustomersAbovePace: 2, DroppedReports: 5, UncostedRequests: 1},
		PolicyChanges: []dashboard.PolicyChangeResponse{
			{
				ID: encodedPolicy(fixture.lossStop), Name: "Hard loss stop", Status: policies.StatusActive, Version: 3,
				Change: dashboard.PolicyChangeUpdated, CreatedAt: fixture.lossStopSince, UpdatedAt: day(time.September, 12),
			},
			{
				ID: encodedPolicy(fixture.routeVideo), Name: "Route video", Status: policies.StatusActive, Version: 1,
				Change: dashboard.PolicyChangeCreated, CreatedAt: day(time.September, 3), UpdatedAt: day(time.September, 3),
			},
			{
				ID: encodedPolicy(fixture.capAudio), Name: "Cap audio", Status: policies.StatusActive, Version: 2,
				Change: dashboard.PolicyChangeUpdated, CreatedAt: day(time.July, 1), UpdatedAt: day(time.July, 2),
			},
			{
				ID: encodedPolicy(fixture.oldPolicies[2]), Name: "Old policy C", Status: policies.StatusActive, Version: 1,
				Change: dashboard.PolicyChangeCreated, CreatedAt: day(time.June, 3), UpdatedAt: day(time.June, 3),
			},
			{
				ID: encodedPolicy(fixture.oldPolicies[1]), Name: "Old policy B", Status: policies.StatusActive, Version: 1,
				Change: dashboard.PolicyChangeCreated, CreatedAt: day(time.June, 2), UpdatedAt: day(time.June, 2),
			},
		},
	}
	if diff := cmp.Diff(want, overview); diff != "" {
		t.Errorf("current overview mismatch (-want +got):\n%s", diff)
	}
}

func TestOverviewOfPreviousPeriods(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	fixture := seedOverview(t, harness)

	overview := decodeInto[dashboard.OverviewResponse](t, harness.memberGet(t, httpapi.EnvironmentTest, overviewPath+"?period=previous"))

	want := overviewSummary{
		Revenue:      "100.000000000",
		Cost:         "75.000000000",
		Margin:       pointer("0.2500"),
		TargetMargin: pointer("0.4000"),
		PlanMargins: []dashboard.PlanMarginResponse{
			planMargin(fixture.pro, "Pro", plans.ModeMarginTarget, pointer("0.4000"), "100.000000000", "75.000000000", pointer("0.2500"), true),
		},
		CostAvoided:    dashboard.CostAvoidedResponse{Total: "7.000000000", Denied: "7.000000000", Routed: "0.000000000", Capped: "0.000000000", ChangedDecisionCount: 1},
		DecisionCounts: dashboard.DecisionCountsResponse{Deny: 1},
		Attention:      dashboard.AttentionResponse{PlansBelowTarget: 1, CustomersAbovePace: 2, DroppedReports: 5},
		DailyCount:     31,
		FirstDay:       "2026-08-01",
		LastDay:        "2026-08-31",
	}
	if overview.Period != dashboard.OverviewPeriodPrevious {
		t.Errorf("period = %s, want previous", overview.Period)
	}
	if diff := cmp.Diff(want, summarizeOverview(overview)); diff != "" {
		t.Errorf("previous overview mismatch (-want +got):\n%s", diff)
	}
	wantDaily := dailySeries(augustStart, 31, map[time.Time]dailyValues{
		day(time.August, 1):  {revenue: 100},
		day(time.August, 10): {cost: 70},
		day(time.August, 20): {cost: 5},
	})
	if diff := cmp.Diff(wantDaily, overview.Daily); diff != "" {
		t.Errorf("previous daily mismatch (-want +got):\n%s", diff)
	}
}

func TestOverviewOfLastThirtyDays(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	fixture := seedOverview(t, harness)

	overview := decodeInto[dashboard.OverviewResponse](t, harness.memberGet(t, httpapi.EnvironmentTest, overviewPath+"?period=last_30_days"))

	want := overviewSummary{
		Revenue:      "150.000000000",
		Cost:         "114.000000000",
		Margin:       pointer("0.2400"),
		TargetMargin: pointer("0.4714"),
		PlanMargins: []dashboard.PlanMarginResponse{
			planMargin(fixture.pro, "Pro", plans.ModeMarginTarget, pointer("0.4000"), "90.000000000", "59.000000000", pointer("0.3444"), true),
			planMargin(fixture.starter, "Starter", plans.ModeFixedAllowance, nil, "0.000000000", "3.000000000", nil, false),
			planMargin(fixture.team, "Team", plans.ModeMarginTarget, pointer("0.6000"), "50.000000000", "40.000000000", pointer("0.2000"), true),
			{CustomerCount: 1, Revenue: "10.000000000", Cost: "12.000000000", Margin: pointer("-0.2000")},
		},
		LossCustomers: []string{"tall-oak -0.2000"},
		CostAvoided: dashboard.CostAvoidedResponse{
			Total: "15.500000000", Denied: "9.000000000", Routed: "3.500000000", Capped: "3.000000000", ChangedDecisionCount: 6,
		},
		DecisionCounts: dashboard.DecisionCountsResponse{Allow: 1, Route: 3, Cap: 1, Deny: 2},
		UncostedCount:  1,
		Attention:      dashboard.AttentionResponse{PlansBelowTarget: 2, CustomersAbovePace: 2, DroppedReports: 5, UncostedRequests: 1},
		DailyCount:     30,
		FirstDay:       "2026-08-18",
		LastDay:        "2026-09-16",
	}
	if diff := cmp.Diff(want, summarizeOverview(overview)); diff != "" {
		t.Errorf("last 30 days overview mismatch (-want +got):\n%s", diff)
	}
	augustTwentieth := overview.Daily[2]
	if augustTwentieth.Date != "2026-08-20" || augustTwentieth.Cost != "5.000000000" {
		t.Errorf("daily entry %+v, want 2026-08-20 with a cost of 5", augustTwentieth)
	}
}

func TestOverviewLossCustomersAreTheLowestNegativeMargins(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	test := httpapi.EnvironmentTest
	seed := func(customerCosts map[string]float64) {
		t.Helper()
		for externalID, cost := range customerCosts {
			customerID := harness.insertCustomer(t, test, externalID, nil, nil)
			harness.insertRevenue(t, test, customerID, "subscription", dollars(10), septemberStart, octoberStart, septemberStart)
			harness.insertLedgerEntry(t, ledgerFixture{
				environment: test, customerID: customerID, feature: textToVideo, model: veoModel, cost: amount(cost),
				periodStart: septemberStart, periodEnd: octoberStart, occurredAt: day(time.September, 2),
			})
			harness.refreshRollups(t, test, customerID, septemberStart)
		}
	}
	lossCustomers := func() []string {
		t.Helper()
		return summarizeOverview(decodeInto[dashboard.OverviewResponse](t, harness.memberGet(t, test, overviewPath))).LossCustomers
	}

	seed(map[string]float64{"loss-10": 11, "loss-20": 12, "loss-30": 13, "break-even": 10, "profitable": 5})
	withThreeLosses := lossCustomers()
	seed(map[string]float64{"loss-40": 14, "loss-50": 15, "loss-60": 16})
	withSixLosses := lossCustomers()

	if diff := cmp.Diff([]string{"loss-30 -0.3000", "loss-20 -0.2000", "loss-10 -0.1000"}, withThreeLosses); diff != "" {
		t.Errorf("loss customers of three losses mismatch (-want +got):\n%s", diff)
	}
	wantSix := []string{"loss-60 -0.6000", "loss-50 -0.5000", "loss-40 -0.4000", "loss-30 -0.3000", "loss-20 -0.2000"}
	if diff := cmp.Diff(wantSix, withSixLosses); diff != "" {
		t.Errorf("loss customers of six losses mismatch (-want +got):\n%s", diff)
	}
}

func TestOverviewWithoutDataHasEmptySeriesAndNullMargins(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	for _, period := range []dashboard.OverviewPeriod{dashboard.OverviewPeriodCurrent, dashboard.OverviewPeriodPrevious, dashboard.OverviewPeriodLast30Days} {
		overview := decodeInto[dashboard.OverviewResponse](t, harness.memberGet(t, httpapi.EnvironmentTest, overviewPath+"?period="+string(period)))

		if overview.Revenue != "0.000000000" || overview.Margin != nil || overview.TargetMargin != nil {
			t.Errorf("%s revenue=%s margin=%v target=%v, want zero and null", period, overview.Revenue, overview.Margin, overview.TargetMargin)
		}
		if overview.PlanMargins == nil || overview.LossCustomers == nil || overview.PolicyChanges == nil || len(overview.Daily) == 0 {
			t.Errorf("%s lists plan_margins=%v loss_customers=%v policy_changes=%v daily=%d, want empty lists and every day", period,
				overview.PlanMargins, overview.LossCustomers, overview.PolicyChanges, len(overview.Daily))
		}
	}
	current := decodeInto[dashboard.OverviewResponse](t, harness.memberGet(t, httpapi.EnvironmentTest, overviewPath))
	previous := decodeInto[dashboard.OverviewResponse](t, harness.memberGet(t, httpapi.EnvironmentTest, overviewPath+"?period=previous"))
	if current.Daily[0].Date != "2026-09-01" || len(current.Daily) != 16 {
		t.Errorf("current daily starts %s with %d days, want the calendar month through today", current.Daily[0].Date, len(current.Daily))
	}
	if previous.Daily[0].Date != "2026-08-01" || len(previous.Daily) != 31 {
		t.Errorf("previous daily starts %s with %d days, want the previous calendar month", previous.Daily[0].Date, len(previous.Daily))
	}
}

func TestOverviewDocumentsNullRowFields(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	document := decodeInto[openAPIDocument](t, harness.serve(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/openapi.json", nil)))

	schemas := document.Components.Schemas
	nullRowFields := map[string]any{
		"mode":                  schemas["PlanMarginResponse"].Properties["mode"]["enum"],
		"latest_policy_outcome": schemas["LossCustomerResponse"].Properties["latest_policy_outcome"]["oneOf"],
	}
	want := map[string]any{
		"mode": []any{"margin_target", "fixed_allowance", nil},
		"latest_policy_outcome": []any{
			map[string]any{"$ref": "#/components/schemas/PolicyOutcomeResponse"},
			map[string]any{"type": "null"},
		},
	}
	if diff := cmp.Diff(want, nullRowFields); diff != "" {
		t.Errorf("null row fields mismatch (-want +got):\n%s", diff)
	}
}

func TestFixedAllowancePlansHaveNoTargetMargin(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	test := httpapi.EnvironmentTest
	studio := harness.insertPlan(t, test, "Studio", 8000, amount(5))
	hobby := harness.insertPlan(t, test, "Hobby", 0, amount(5))
	acme := harness.insertCustomer(t, test, "acme", nil, &studio)
	cedar := harness.insertCustomer(t, test, "cedar", nil, &hobby)
	for _, customer := range []struct {
		customerID uuid.UUID
		revenue    money.Amount
		cost       money.Amount
	}{
		{customerID: acme, revenue: dollars(20), cost: dollars(10)},
		{customerID: cedar, revenue: dollars(10), cost: dollars(12)},
	} {
		harness.insertRevenue(t, test, customer.customerID, "subscription", customer.revenue, septemberStart, octoberStart, septemberStart)
		harness.insertLedgerEntry(t, ledgerFixture{
			environment: test, customerID: customer.customerID, feature: textToVideo, model: veoModel, cost: &customer.cost,
			periodStart: septemberStart, periodEnd: octoberStart, occurredAt: day(time.September, 2),
		})
		harness.refreshRollups(t, test, customer.customerID, septemberStart)
		harness.setSettled(t, test, customer.customerID, customer.cost)
	}

	overview := decodeInto[dashboard.OverviewResponse](t, harness.memberGet(t, test, overviewPath))
	listed := decodeInto[httpapi.PageBody[dashboard.CustomerMarginResponse]](t, harness.memberGet(t, test, customersPath+"?search=acme"))
	detail := decodeInto[dashboard.CustomerDetailResponse](t, harness.memberGet(t, test, customersPath+"/"+encodedCustomer(acme)))

	wantPlanMargins := []dashboard.PlanMarginResponse{
		planMargin(hobby, "Hobby", plans.ModeFixedAllowance, nil, "10.000000000", "12.000000000", pointer("-0.2000"), true),
		planMargin(studio, "Studio", plans.ModeFixedAllowance, nil, "20.000000000", "10.000000000", pointer("0.5000"), false),
	}
	if diff := cmp.Diff(wantPlanMargins, overview.PlanMargins); diff != "" {
		t.Errorf("plan margins mismatch (-want +got):\n%s", diff)
	}
	if overview.TargetMargin != nil || overview.Attention.PlansBelowTarget != 1 {
		t.Errorf("target_margin=%v plans_below_target=%d, want null and 1", overview.TargetMargin, overview.Attention.PlansBelowTarget)
	}
	if listed.Items[0].TargetMargin != nil || detail.TargetMargin != nil {
		t.Errorf("customer list target_margin=%v detail target_margin=%v, want null for a fixed allowance plan", listed.Items[0].TargetMargin, detail.TargetMargin)
	}
}

func TestOverviewRejectsUnknownPeriod(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	assertProblem(t, harness.memberGet(t, httpapi.EnvironmentTest, overviewPath+"?period=forever"), http.StatusUnprocessableEntity, "validation_failed", "query.period")
}

func TestDashboardRoutesAdmitOnlySessions(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	customerID := harness.insertCustomer(t, httpapi.EnvironmentTest, "acme", nil, nil)
	runtimeSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	adminSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)

	for _, target := range []string{overviewPath, customersPath, customersPath + "/" + encodedCustomer(customerID)} {
		assertProblem(t, harness.bearerGet(t, runtimeSecret, target), http.StatusForbidden, "scope_forbidden")
		assertProblem(t, harness.bearerGet(t, adminSecret, target), http.StatusForbidden, "scope_forbidden")
		assertStatus(t, harness.memberGet(t, httpapi.EnvironmentTest, target), http.StatusOK)
	}
}

func TestDashboardRejectsEnvironmentAboveActiveCustomerMaximum(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO customers (customer_id, environment, external_id, status)
		SELECT gen_random_uuid(), 'test', 'bulk-' || number, 'active' FROM generate_series(1, $1::integer) AS number`,
		dashboard.ActiveCustomerMaximum+1)
	if err != nil {
		t.Fatalf("insert customers: %v", err)
	}

	assertProblem(t, harness.memberGet(t, httpapi.EnvironmentTest, overviewPath), http.StatusUnprocessableEntity, "environment_too_large")
	assertProblem(t, harness.memberGet(t, httpapi.EnvironmentTest, customersPath), http.StatusUnprocessableEntity, "environment_too_large")
	assertStatus(t, harness.memberGet(t, httpapi.EnvironmentLive, customersPath), http.StatusOK)
}
