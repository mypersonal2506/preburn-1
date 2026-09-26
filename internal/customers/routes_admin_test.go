package customers_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
)

const (
	adminCustomersPath = "/api/v1/customers"
	nanosPerDollar     = 1_000_000_000
	testFeature        = "text_to_video"
)

type adminHarness struct {
	*harness
	counters     *decisions.Counters
	stateQueries *queryCounter
}

type queryCounter struct {
	count atomic.Int64
	rows  atomic.Int64
}

var (
	adminNow       = time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	septemberStart = time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	octoberStart   = time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
)

func newAdminHarness(t *testing.T) *adminHarness {
	t.Helper()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(adminNow)
	logger := logging.New(t.Output(), slog.LevelDebug)
	service := customers.NewService(pool, cacheClient, manualClock)
	apiKeys := apikeys.NewService(pool, cacheClient, manualClock, logger)
	counters := decisions.NewCounters(cacheClient)
	stateQueries := &queryCounter{}
	stateConfiguration := pool.Config().Copy()
	stateConfiguration.ConnConfig.Tracer = stateQueries
	statePool, err := pgxpool.NewWithConfig(t.Context(), stateConfiguration)
	if err != nil {
		t.Fatalf("open state pool: %v", err)
	}
	t.Cleanup(statePool.Close)
	states := customerstate.NewLoader(statePool)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, apiKeys.Authenticator())
	customers.RegisterRoutes(api, service)
	customers.RegisterAdminRoutes(api, service, states, counters)
	return &adminHarness{
		harness: &harness{
			pool:    pool,
			cache:   cacheClient,
			clock:   manualClock,
			service: service,
			apiKeys: apiKeys,
			handler: mux,
		},
		counters:     counters,
		stateQueries: stateQueries,
	}
}

func (counter *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	counter.count.Add(1)
	return ctx
}

func (counter *queryCounter) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	counter.rows.Add(data.CommandTag.RowsAffected())
}

func (harness *adminHarness) get(t *testing.T, secret string, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	request.Header.Set(authorizationHeader, "Bearer "+secret)
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *adminHarness) insertRollup(t *testing.T, customerID uuid.UUID, revenueNet money.Amount) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO period_rollups (environment, customer_id, period_start, period_end, revenue_net_nanos, cost_nanos, uncosted_count, decision_counts)
		VALUES ('test', $1, $2, $3, $4, 0, 0, '{"allow":0,"route":0,"cap":0,"deny":0}')`,
		customerID, septemberStart, octoberStart, int64(revenueNet))
	if err != nil {
		t.Fatalf("insert rollup of customer %s: %v", customerID, err)
	}
}

func (harness *adminHarness) setCounter(t *testing.T, environment httpapi.Environment, customerID uuid.UUID, settled money.Amount, reserved money.Amount, count int64) {
	t.Helper()
	fields := map[string]int64{
		"settled":                 int64(settled),
		"reserved":                int64(reserved),
		"count":                   count,
		"settled:" + testFeature:  int64(settled),
		"reserved:" + testFeature: int64(reserved),
		"count:" + testFeature:    count,
	}
	key := harness.counters.CounterKey(environment, customerID, septemberStart)
	if err := harness.counters.Overwrite(t.Context(), key, fields, octoberStart); err != nil {
		t.Fatalf("overwrite counter of customer %s: %v", customerID, err)
	}
}

func dollars(amount int64) money.Amount {
	return money.Amount(amount * nanosPerDollar)
}

func TestAdminCustomerRoutesAdmitOnlyAdminKeys(t *testing.T) {
	t.Parallel()
	harness := newAdminHarness(t)
	harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{})
	runtimeSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	adminSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)

	for _, target := range []string{adminCustomersPath, adminCustomersPath + "/" + testExternalID} {
		assertProblem(t, harness.get(t, runtimeSecret, target), http.StatusForbidden, "scope_forbidden")
		assertStatus(t, harness.get(t, adminSecret, target), http.StatusOK)
	}
}

func TestGetCustomerRouteReturnsCurrentSignals(t *testing.T) {
	t.Parallel()
	harness := newAdminHarness(t)
	planID := harness.createPlan(t, httpapi.EnvironmentTest, "Pro", "active")
	customer := harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{PlanID: &planID})
	harness.insertRollup(t, customer.ID, dollars(100))
	harness.setCounter(t, httpapi.EnvironmentTest, customer.ID, dollars(31), dollars(2), 7)
	secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)

	recorder := harness.get(t, secret, adminCustomersPath+"/"+testExternalID)

	assertStatus(t, recorder, http.StatusOK)
	body := decodeBody(t, recorder)
	encodedPlanID := identifiers.Encode(identifiers.PrefixPlan, planID)
	want := map[string]any{
		"id":                identifiers.Encode(identifiers.PrefixCustomer, customer.ID),
		"external_id":       testExternalID,
		"display_name":      nil,
		"plan_id":           encodedPlanID,
		"effective_plan_id": encodedPlanID,
		"metadata":          map[string]any{},
		"status":            "active",
		"created_at":        adminNow.Format(time.RFC3339),
		"updated_at":        adminNow.Format(time.RFC3339),
		"period_start":      septemberStart.Format(time.RFC3339),
		"period_end":        octoberStart.Format(time.RFC3339),
		"signals": map[string]any{
			"period_revenue_net":     "100.000000000",
			"cost_allowance":         "60.000000000",
			"cost_to_date":           "31.000000000",
			"reserved":               "2.000000000",
			"elapsed_fraction":       "0.5167",
			"allowance_remaining":    "27.000000000",
			"pace":                   "1.0000",
			"projected_margin":       "0.4000",
			"request_estimated_cost": nil,
			"period_decision_count":  7.0,
			"features": map[string]any{
				testFeature: map[string]any{"cost_to_date": "31.000000000", "reserved": "2.000000000", "period_decision_count": 7.0},
			},
		},
	}
	if diff := cmp.Diff(want, body); diff != "" {
		t.Errorf("customer body mismatch (-want +got):\n%s", diff)
	}
}

func TestGetCustomerRouteFollowsDefaultPlanWithoutCounter(t *testing.T) {
	t.Parallel()
	harness := newAdminHarness(t)
	planID := harness.createPlan(t, httpapi.EnvironmentTest, "Default", "active")
	if _, err := harness.pool.Exec(t.Context(), "UPDATE environment_settings SET default_plan_id = $1 WHERE environment = 'test'", planID); err != nil {
		t.Fatalf("set default plan: %v", err)
	}
	harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{})
	secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)

	body := decodeBody(t, harness.get(t, secret, adminCustomersPath+"/"+testExternalID))

	if body["plan_id"] != nil || body["effective_plan_id"] != identifiers.Encode(identifiers.PrefixPlan, planID) {
		t.Errorf("plan_id = %v effective_plan_id = %v, want null and the default plan", body["plan_id"], body["effective_plan_id"])
	}
	customerSignals := body["signals"].(map[string]any)
	if customerSignals["cost_to_date"] != "0.000000000" || customerSignals["pace"] != "0.0000" || customerSignals["period_decision_count"] != 0.0 {
		t.Errorf("signals = %v, want zero cost, pace and count without a counter", customerSignals)
	}
}

func TestGetCustomerRouteStaysInEnvironment(t *testing.T) {
	t.Parallel()
	harness := newAdminHarness(t)
	harness.upsert(t, httpapi.EnvironmentLive, testExternalID, customers.UpsertInput{})
	testSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)
	liveSecret := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeAdmin)

	assertProblem(t, harness.get(t, testSecret, adminCustomersPath+"/"+testExternalID), http.StatusNotFound, "not_found")
	assertStatus(t, harness.get(t, liveSecret, adminCustomersPath+"/"+testExternalID), http.StatusOK)
	assertProblem(t, harness.get(t, liveSecret, adminCustomersPath+"/unknown"), http.StatusNotFound, "not_found")
}

func TestListCustomersRoutePagesWithSignals(t *testing.T) {
	t.Parallel()
	harness := newAdminHarness(t)
	planID := harness.createPlan(t, httpapi.EnvironmentTest, "Pro", "active")
	for index, externalID := range []string{"acme", "acme-labs", "cedar"} {
		customer := harness.upsert(t, httpapi.EnvironmentTest, externalID, customers.UpsertInput{PlanID: &planID})
		harness.insertRollup(t, customer.ID, dollars(int64(100*(index+1))))
		harness.setCounter(t, httpapi.EnvironmentTest, customer.ID, dollars(int64(10*(index+1))), 0, int64(index+1))
		harness.clock.Advance(time.Second)
	}
	harness.upsert(t, httpapi.EnvironmentLive, "acme", customers.UpsertInput{})
	secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)

	firstPage := decodeBody(t, harness.get(t, secret, adminCustomersPath+"?limit=2"))
	secondPage := decodeBody(t, harness.get(t, secret, adminCustomersPath+"?limit=2&cursor="+firstPage["next_cursor"].(string)))
	searched := decodeBody(t, harness.get(t, secret, adminCustomersPath+"?search=ACME"))

	type listed struct {
		ExternalID    string
		RevenueNet    any
		CostToDate    any
		DecisionCount any
	}
	summarize := func(page map[string]any) []listed {
		var summaries []listed
		for _, entry := range page["items"].([]any) {
			item := entry.(map[string]any)
			itemSignals := item["signals"].(map[string]any)
			summaries = append(summaries, listed{
				ExternalID:    item["external_id"].(string),
				RevenueNet:    itemSignals["period_revenue_net"],
				CostToDate:    itemSignals["cost_to_date"],
				DecisionCount: itemSignals["period_decision_count"],
			})
		}
		return summaries
	}
	wantFirst := []listed{
		{ExternalID: "cedar", RevenueNet: "300.000000000", CostToDate: "30.000000000", DecisionCount: 3.0},
		{ExternalID: "acme-labs", RevenueNet: "200.000000000", CostToDate: "20.000000000", DecisionCount: 2.0},
	}
	wantSecond := []listed{{ExternalID: "acme", RevenueNet: "100.000000000", CostToDate: "10.000000000", DecisionCount: 1.0}}
	wantSearched := []listed{wantFirst[1], wantSecond[0]}
	if diff := cmp.Diff(wantFirst, summarize(firstPage)); diff != "" {
		t.Errorf("first page mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantSecond, summarize(secondPage)); diff != "" {
		t.Errorf("second page mismatch (-want +got):\n%s", diff)
	}
	if secondPage["next_cursor"] != nil {
		t.Errorf("second page next_cursor = %v, want null", secondPage["next_cursor"])
	}
	if diff := cmp.Diff(wantSearched, summarize(searched)); diff != "" {
		t.Errorf("searched page mismatch (-want +got):\n%s", diff)
	}
}

func TestListCustomersRouteLoadsStatesOfAPageInOneBatch(t *testing.T) {
	t.Parallel()
	harness := newAdminHarness(t)
	for _, externalID := range []string{"acme", "cedar", "hilltop", "lumber", "studio-north", "tall-oak"} {
		harness.upsert(t, httpapi.EnvironmentTest, externalID, customers.UpsertInput{})
		harness.clock.Advance(time.Second)
	}
	if _, err := harness.pool.Exec(t.Context(), "UPDATE customers SET status = 'disabled' WHERE external_id IN ('acme', 'cedar')"); err != nil {
		t.Fatalf("disable the oldest customers: %v", err)
	}
	secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)
	stateQueriesOfPage := func(limit int) int64 {
		t.Helper()
		before := harness.stateQueries.count.Load()
		page := decodeBody(t, harness.get(t, secret, fmt.Sprintf("%s?limit=%d", adminCustomersPath, limit)))
		if listed := len(page["items"].([]any)); listed != limit {
			t.Fatalf("page lists %d customers, want %d", listed, limit)
		}
		return harness.stateQueries.count.Load() - before
	}

	twoCustomers, sixCustomers := stateQueriesOfPage(2), stateQueriesOfPage(6)

	if twoCustomers != sixCustomers {
		t.Errorf("state queries = %d for a page of 2 and %d for a page of 6 with 2 disabled customers, want one batch whatever the page", twoCustomers, sixCustomers)
	}
}

func TestListCustomersRouteReadsOnlyTheStatesOfThePage(t *testing.T) {
	t.Parallel()
	harness := newAdminHarness(t)
	for _, externalID := range []string{"acme", "cedar", "hilltop", "lumber", "studio-north", "tall-oak"} {
		harness.upsert(t, httpapi.EnvironmentTest, externalID, customers.UpsertInput{})
	}
	secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)
	before := harness.stateQueries.rows.Load()

	page := decodeBody(t, harness.get(t, secret, adminCustomersPath+"?limit=2"))

	if listed := len(page["items"].([]any)); listed != 2 {
		t.Fatalf("page lists %d customers, want 2", listed)
	}
	if rows := harness.stateQueries.rows.Load() - before; rows != 2 {
		t.Errorf("state queries read %d rows for a page of 2 customers without revenue, want 2, the plan row of each listed customer", rows)
	}
}

func TestListCustomersRouteLoadsSignalsOfInactiveCustomers(t *testing.T) {
	t.Parallel()
	harness := newAdminHarness(t)
	active := harness.upsert(t, httpapi.EnvironmentTest, "acme", customers.UpsertInput{})
	disabled := harness.upsert(t, httpapi.EnvironmentTest, "cedar", customers.UpsertInput{})
	harness.insertRollup(t, active.ID, dollars(100))
	harness.insertRollup(t, disabled.ID, dollars(40))
	harness.setCounter(t, httpapi.EnvironmentTest, disabled.ID, dollars(4), 0, 2)
	if _, err := harness.pool.Exec(t.Context(), "UPDATE customers SET status = 'disabled' WHERE customer_id = $1", disabled.ID); err != nil {
		t.Fatalf("disable customer: %v", err)
	}
	secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)

	page := decodeBody(t, harness.get(t, secret, adminCustomersPath))

	got := map[string][2]any{}
	for _, entry := range page["items"].([]any) {
		item := entry.(map[string]any)
		itemSignals := item["signals"].(map[string]any)
		got[item["external_id"].(string)] = [2]any{itemSignals["period_revenue_net"], itemSignals["cost_to_date"]}
	}
	want := map[string][2]any{
		"acme":  {"100.000000000", "0.000000000"},
		"cedar": {"40.000000000", "4.000000000"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("revenue and cost by customer mismatch (-want +got):\n%s", diff)
	}
}

func TestListCustomersRouteRejectsInvalidQuery(t *testing.T) {
	t.Parallel()
	harness := newAdminHarness(t)
	secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)
	liveSecret := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeAdmin)
	harness.upsert(t, httpapi.EnvironmentTest, "acme", customers.UpsertInput{})
	harness.upsert(t, httpapi.EnvironmentTest, "cedar", customers.UpsertInput{})
	testCursor, isCursor := decodeBody(t, harness.get(t, secret, adminCustomersPath+"?limit=1"))["next_cursor"].(string)
	if !isCursor {
		t.Fatal("first test page has no next cursor")
	}

	assertProblem(t, harness.get(t, secret, adminCustomersPath+"?search=a%20b"), http.StatusUnprocessableEntity, "validation_failed", "query.search")
	assertProblem(t, harness.get(t, secret, adminCustomersPath+"?limit=101"), http.StatusUnprocessableEntity, "validation_failed", "query.limit")
	assertProblem(t, harness.get(t, secret, adminCustomersPath+"?cursor=bogus"), http.StatusUnprocessableEntity, "invalid_cursor")
	assertProblem(t, harness.get(t, liveSecret, adminCustomersPath+"?cursor="+testCursor), http.StatusUnprocessableEntity, "invalid_cursor")
}
