package decisions_test

import (
	"net/http"
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
	"github.com/preburn/preburn/internal/pricing"
	"github.com/preburn/preburn/internal/signals"
)

func TestReportSettlesTheDecision(t *testing.T) {
	harness := newReportHarness(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	harness.clock.Advance(time.Minute)
	occurredAt := checkStart.Add(30 * time.Second)
	request := serverReport(checked.DecisionID, "6")
	request.OccurredAt = &occurredAt

	result := harness.report(t, request)

	want := decisions.ReportResult{
		LedgerEntryID: result.LedgerEntryID,
		Cost:          formattedDollars(90),
		CostStatus:    pricing.CostStatusCosted,
	}
	if diff := cmp.Diff(want, result); diff != "" {
		t.Errorf("result mismatch (-want +got):\n%s", diff)
	}
	customer := harness.ensureCustomer(t)
	decisionID := decodeIdentifier(t, identifiers.PrefixDecision, checked.DecisionID)
	wantRow := ledgerRow{
		CustomerID:     customer.ID,
		DecisionID:     &decisionID,
		IdempotencyKey: "decision:" + decisionID.String(),
		Feature:        checkFeature,
		Provider:       falProvider,
		Model:          veoModel,
		Attributes:     map[string]any{},
		Usage:          map[string]any{outputSeconds: float64(6_000_000)},
		CostNanos:      new(int64(dollarAmount(90))),
		CostStatus:     "costed",
		DecisionSource: "server",
		PeriodStart:    checkPeriod.Start,
		PeriodEnd:      checkPeriod.End,
		OccurredAt:     occurredAt,
		CreatedAt:      checkStart.Add(time.Minute),
	}
	if diff := cmp.Diff(wantRow, harness.ledgerEntry(t, result)); diff != "" {
		t.Errorf("ledger entry mismatch (-want +got):\n%s", diff)
	}
	status, settledAt := harness.decisionStatus(t, checked.DecisionID)
	if status != "settled" || settledAt == nil || !settledAt.Equal(checkStart.Add(time.Minute)) {
		t.Errorf("decision status %s settled at %v, want settled at the report", status, settledAt)
	}
	wantCounter := map[string]string{
		"settled": "900000000", "settled:" + checkFeature: "900000000",
		"reserved": "0", "reserved:" + checkFeature: "0",
		"count": "1", "count:" + checkFeature: "1",
	}
	if diff := cmp.Diff(wantCounter, harness.counter(t, customer.ID)); diff != "" {
		t.Errorf("counter mismatch (-want +got):\n%s", diff)
	}
	if status := harness.reservationStatus(t, checked.DecisionID); status != "settled" {
		t.Errorf("reservation status = %s, want settled", status)
	}
	job := jobstest.RequireInserted(t, harness.pool, ledger.RollupRefreshArgs{}, nil)
	if diff := cmp.Diff(ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, customer.ID, checkPeriod.Start), job.Args); diff != "" {
		t.Errorf("rollup refresh arguments mismatch (-want +got):\n%s", diff)
	}
	reported := labeledCounter(t, harness.registry, reportsMetricName, map[string]string{"environment": "test", "cost_status": "costed", "duplicate": "false"})
	if reported != 1 {
		t.Errorf("%s = %v, want 1", reportsMetricName, reported)
	}
}

func TestReportTwiceReturnsTheSameEntry(t *testing.T) {
	harness := newReportHarness(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	first := harness.report(t, serverReport(checked.DecisionID, "6"))
	customer := harness.ensureCustomer(t)
	counterAfterFirst := harness.counter(t, customer.ID)

	second := harness.report(t, serverReport(checked.DecisionID, "8"))

	want := first
	want.Duplicate = true
	if diff := cmp.Diff(want, second); diff != "" {
		t.Errorf("second result mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(counterAfterFirst, harness.counter(t, customer.ID)); diff != "" {
		t.Errorf("counter changed by the duplicate (-first +second):\n%s", diff)
	}
	if count := harness.ledgerEntryCount(t); count != 1 {
		t.Errorf("ledger entries = %d, want 1", count)
	}
	duplicates := labeledCounter(t, harness.registry, reportsMetricName, map[string]string{"environment": "test", "cost_status": "costed", "duplicate": "true"})
	if duplicates != 1 {
		t.Errorf("%s with duplicate true = %v, want 1", reportsMetricName, duplicates)
	}
}

func TestConcurrentReportsOfOneDecisionStoreOneEntry(t *testing.T) {
	harness := newReportHarness(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	const reporters = 10
	results := make([]decisions.ReportResult, reporters)
	failures := make([]error, reporters)

	var group sync.WaitGroup
	for index := range reporters {
		group.Go(func() {
			results[index], failures[index] = harness.reports.Report(t.Context(), httpapi.EnvironmentTest, serverReport(checked.DecisionID, "6"))
		})
	}
	group.Wait()

	stored := 0
	for index, result := range results {
		if failures[index] != nil {
			t.Fatalf("report %d: %v", index, failures[index])
		}
		if !result.Duplicate {
			stored++
		}
		if result.LedgerEntryID != results[0].LedgerEntryID {
			t.Errorf("report %d entry = %s, want %s", index, result.LedgerEntryID, results[0].LedgerEntryID)
		}
	}
	if stored != 1 || harness.ledgerEntryCount(t) != 1 {
		t.Errorf("stored reports = %d and ledger entries = %d, want 1 and 1", stored, harness.ledgerEntryCount(t))
	}
	counter := harness.counter(t, harness.ensureCustomer(t).ID)
	if counter["settled"] != "900000000" || counter["reserved"] != "0" || counter["count"] != "1" {
		t.Errorf("counter = %v, want the usage settled once", counter)
	}
}

func TestReportAttributesReplaceDecisionAttributesBeforeRating(t *testing.T) {
	silent := pricing.Attributes{"audio": pricing.BooleanAttribute(false)}
	tests := []struct {
		name             string
		policyAction     string
		checkAttributes  pricing.Attributes
		reportAttributes pricing.Attributes
		wantCost         int64
		wantAttributes   map[string]any
	}{
		{
			name:            "decision attributes",
			checkAttributes: silent,
			wantCost:        80,
			wantAttributes:  map[string]any{"audio": false},
		},
		{
			name:             "report attribute replaces the decision attribute",
			checkAttributes:  silent,
			reportAttributes: pricing.Attributes{"audio": pricing.BooleanAttribute(true)},
			wantCost:         120,
			wantAttributes:   map[string]any{"audio": true},
		},
		{
			name:             "report attribute joins the decision attributes",
			checkAttributes:  pricing.Attributes{"resolution": pricing.StringAttribute("720p")},
			reportAttributes: silent,
			wantCost:         80,
			wantAttributes:   map[string]any{"audio": false, "resolution": "720p"},
		},
		{
			name:           "cap override of a pricing attribute",
			policyAction:   `{"outcome": "cap", "overrides": {"audio": false}}`,
			wantCost:       80,
			wantAttributes: map[string]any{"audio": false},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newReportHarness(t)
			if test.policyAction != "" {
				harness.createPolicy(t, map[string]string{"action": test.policyAction})
			}
			checkRequest := videoCheck(veoModel, "8")
			checkRequest.Attributes = test.checkAttributes
			checked := harness.check(t, checkRequest)
			request := serverReport(checked.DecisionID, "8")
			request.Attributes = test.reportAttributes

			result := harness.report(t, request)

			if result.Cost == nil || *result.Cost != *formattedDollars(test.wantCost) {
				t.Errorf("cost = %v, want %s", result.Cost, *formattedDollars(test.wantCost))
			}
			if diff := cmp.Diff(test.wantAttributes, harness.ledgerEntry(t, result).Attributes); diff != "" {
				t.Errorf("stored attributes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReportAfterExpiryAddsTheUsageWithoutReservation(t *testing.T) {
	harness := newReportHarness(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	harness.expire(t, checked.DecisionID)

	harness.report(t, serverReport(checked.DecisionID, "6"))

	wantCounter := map[string]string{
		"settled": "900000000", "settled:" + checkFeature: "900000000",
		"reserved": "0", "reserved:" + checkFeature: "0",
		"count": "1", "count:" + checkFeature: "1",
	}
	if diff := cmp.Diff(wantCounter, harness.counter(t, harness.ensureCustomer(t).ID)); diff != "" {
		t.Errorf("counter mismatch (-want +got):\n%s", diff)
	}
	if status, _ := harness.decisionStatus(t, checked.DecisionID); status != "settled" {
		t.Errorf("decision status = %s, want settled", status)
	}
	if status := harness.reservationStatus(t, checked.DecisionID); status != "expired" {
		t.Errorf("reservation status = %s, want expired", status)
	}
}

func TestReportRejectsInvalidFields(t *testing.T) {
	validFallback := fallbackReport(identifiers.New().String(), "6")
	withFields := func(change func(request *decisions.ReportRequest)) decisions.ReportRequest {
		request := validFallback
		change(&request)
		return request
	}
	tests := []struct {
		name      string
		request   decisions.ReportRequest
		locations []string
	}{
		{
			name: "fallback without its fields",
			request: decisions.ReportRequest{
				DecisionSource: ledger.DecisionSourceFallback,
				Usage:          map[string]string{outputSeconds: "6"},
			},
			locations: []string{"body.idempotency_key", "body.customer_id", "body.feature", "body.provider", "body.model"},
		},
		{
			name: "fallback with a decision",
			request: withFields(func(request *decisions.ReportRequest) {
				request.DecisionID = identifiers.Encode(identifiers.PrefixDecision, identifiers.New())
			}),
			locations: []string{"body.decision_id"},
		},
		{
			name:      "idempotency key that is not a UUID",
			request:   withFields(func(request *decisions.ReportRequest) { request.IdempotencyKey = "report-1" }),
			locations: []string{"body.idempotency_key"},
		},
		{
			name:      "unknown decision source",
			request:   withFields(func(request *decisions.ReportRequest) { request.DecisionSource = "client" }),
			locations: []string{"body.decision_source"},
		},
		{
			name:      "server report without a decision",
			request:   serverReport("", "6"),
			locations: []string{"body.decision_id"},
		},
		{
			name:      "server report with a malformed decision id",
			request:   serverReport("dec_1", "6"),
			locations: []string{"body.decision_id"},
		},
		{
			name:      "missing usage",
			request:   withFields(func(request *decisions.ReportRequest) { request.Usage = nil }),
			locations: []string{"body.usage"},
		},
		{
			name: "invalid usage and attributes",
			request: withFields(func(request *decisions.ReportRequest) {
				request.Usage = map[string]string{"parsecs": "1", outputSeconds: "-1"}
				request.Attributes = pricing.Attributes{"resolution": pricing.StringAttribute("999p")}
			}),
			locations: []string{"body.attributes.resolution", "body.usage.output_seconds", "body.usage"},
		},
		{
			name:      "invalid customer id",
			request:   withFields(func(request *decisions.ReportRequest) { request.CustomerID = "no spaces" }),
			locations: []string{"body.customer_id"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newReportHarness(t)

			_, err := harness.reports.Report(t.Context(), httpapi.EnvironmentTest, test.request)

			if diff := cmp.Diff(test.locations, problemLocations(t, err)); diff != "" {
				t.Errorf("problem locations mismatch (-want +got):\n%s", diff)
			}
			if count := harness.ledgerEntryCount(t); count != 0 {
				t.Errorf("ledger entries = %d, want none", count)
			}
		})
	}
}

func TestFallbackReportCountsTheRequest(t *testing.T) {
	harness := newReportHarness(t)
	idempotencyKey := identifiers.New()
	occurredAt := checkStart.Add(-time.Minute)
	request := fallbackReport(strings.ToUpper(idempotencyKey.String()), "6")
	request.OccurredAt = &occurredAt

	result := harness.report(t, request)

	customer, err := harness.customers.CustomerByExternalID(t.Context(), httpapi.EnvironmentTest, fallbackCustomer)
	if err != nil {
		t.Fatalf("find created customer: %v", err)
	}
	wantRow := ledgerRow{
		CustomerID:     customer.ID,
		IdempotencyKey: idempotencyKey.String(),
		Feature:        checkFeature,
		Provider:       falProvider,
		Model:          veoModel,
		Attributes:     map[string]any{},
		Usage:          map[string]any{outputSeconds: float64(6_000_000)},
		CostNanos:      new(int64(dollarAmount(90))),
		CostStatus:     "costed",
		DecisionSource: "fallback",
		PeriodStart:    signals.ResolvePeriod(occurredAt, nil, nil).Start,
		PeriodEnd:      signals.ResolvePeriod(occurredAt, nil, nil).End,
		OccurredAt:     occurredAt,
		CreatedAt:      checkStart,
	}
	if diff := cmp.Diff(wantRow, harness.ledgerEntry(t, result)); diff != "" {
		t.Errorf("ledger entry mismatch (-want +got):\n%s", diff)
	}
	counterKey := harness.counters.CounterKey(httpapi.EnvironmentTest, customer.ID, wantRow.PeriodStart)
	counter, err := harness.cache.Redis().HGetAll(t.Context(), counterKey).Result()
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	wantCounter := map[string]string{
		"settled": "900000000", "settled:" + checkFeature: "900000000",
		"count": "1", "count:" + checkFeature: "1",
	}
	if diff := cmp.Diff(wantCounter, counter); diff != "" {
		t.Errorf("counter mismatch (-want +got):\n%s", diff)
	}
	jobstest.RequireInserted(t, harness.pool, ledger.RollupRefreshArgs{}, nil)

	repeated := harness.report(t, fallbackReport(idempotencyKey.String(), "9"))
	if repeated.LedgerEntryID != result.LedgerEntryID || !repeated.Duplicate {
		t.Errorf("repeated report = %+v, want the first entry %s as a duplicate", repeated, result.LedgerEntryID)
	}
	counterAfterRepeat, err := harness.cache.Redis().HGetAll(t.Context(), counterKey).Result()
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if diff := cmp.Diff(wantCounter, counterAfterRepeat); diff != "" {
		t.Errorf("counter changed by the repeated report (-want +got):\n%s", diff)
	}
}

func TestReportStoresUncostedUsageWithoutCost(t *testing.T) {
	harness := newReportHarness(t)
	request := fallbackReport(identifiers.New().String(), "6")
	request.Provider = unpricedProvider
	request.Model = unpricedModel

	result := harness.report(t, request)

	if result.Cost != nil || result.CostStatus != pricing.CostStatusUncosted {
		t.Errorf("result = %+v, want uncosted without cost", result)
	}
	row := harness.ledgerEntry(t, result)
	if row.CostNanos != nil || row.CostStatus != "uncosted" {
		t.Errorf("ledger cost %v status %s, want null and uncosted", row.CostNanos, row.CostStatus)
	}
	counter := harness.counter(t, row.CustomerID)
	if counter["settled"] != "0" || counter["count"] != "1" {
		t.Errorf("counter = %v, want settled 0 and count 1", counter)
	}
	uncosted := labeledCounter(t, harness.registry, reportsMetricName, map[string]string{"environment": "test", "cost_status": "uncosted", "duplicate": "false"})
	if uncosted != 1 {
		t.Errorf("%s with cost_status uncosted = %v, want 1", reportsMetricName, uncosted)
	}
}

func TestReportRejectsDeniedAndUnknownDecisions(t *testing.T) {
	harness := newReportHarness(t)
	harness.createPolicy(t, map[string]string{"action": `{"outcome": "deny"}`})
	denied := harness.check(t, videoCheck(veoModel, "8"))

	_, err := harness.reports.Report(t.Context(), httpapi.EnvironmentTest, serverReport(denied.DecisionID, "6"))
	assertCodedError(t, err, http.StatusConflict, "decision_not_reportable")

	unknown := identifiers.Encode(identifiers.PrefixDecision, identifiers.New())
	_, err = harness.reports.Report(t.Context(), httpapi.EnvironmentTest, serverReport(unknown, "6"))
	assertCodedError(t, err, http.StatusNotFound, "not_found")

	_, err = harness.reports.Report(t.Context(), httpapi.EnvironmentLive, serverReport(denied.DecisionID, "6"))
	assertCodedError(t, err, http.StatusNotFound, "not_found")

	if count := harness.ledgerEntryCount(t); count != 0 {
		t.Errorf("ledger entries = %d, want none", count)
	}
}

func TestReportBatch(t *testing.T) {
	harness := newReportHarness(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	missingKey := fallbackReport("", "6")
	unknown := serverReport(identifiers.Encode(identifiers.PrefixDecision, identifiers.New()), "6")

	results, err := harness.reports.ReportBatch(t.Context(), httpapi.EnvironmentTest, []decisions.ReportRequest{
		serverReport(checked.DecisionID, "6"),
		missingKey,
		fallbackReport(identifiers.New().String(), "4"),
		unknown,
	})
	if err != nil {
		t.Fatalf("report batch: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("results = %d, want 4", len(results))
	}
	statuses := []int{results[0].Status, results[1].Status, results[2].Status, results[3].Status}
	if diff := cmp.Diff([]int{http.StatusAccepted, http.StatusUnprocessableEntity, http.StatusAccepted, http.StatusNotFound}, statuses); diff != "" {
		t.Errorf("statuses mismatch (-want +got):\n%s", diff)
	}
	if results[0].Result == nil || *results[0].Result.Cost != *formattedDollars(90) || results[2].Result == nil || *results[2].Result.Cost != *formattedDollars(60) {
		t.Errorf("stored results = %+v and %+v, want costs 0.90 and 0.60", results[0].Result, results[2].Result)
	}
	wantInvalid := &decisions.ReportFailure{
		Code:   "validation_failed",
		Detail: "validation failed",
		Errors: []httpapi.ProblemError{{Location: "body.reports[1].idempotency_key", Message: "expected a UUID"}},
	}
	if diff := cmp.Diff(wantInvalid, results[1].Error); diff != "" || results[1].Result != nil {
		t.Errorf("invalid report error mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(&decisions.ReportFailure{Code: "not_found", Detail: "not found"}, results[3].Error); diff != "" {
		t.Errorf("unknown decision error mismatch (-want +got):\n%s", diff)
	}
	if count := harness.ledgerEntryCount(t); count != 2 {
		t.Errorf("ledger entries = %d, want 2", count)
	}

	oversized := make([]decisions.ReportRequest, 501)
	_, err = harness.reports.ReportBatch(t.Context(), httpapi.EnvironmentTest, oversized)
	if diff := cmp.Diff([]string{"body.reports"}, problemLocations(t, err)); diff != "" {
		t.Errorf("oversized batch problem locations mismatch (-want +got):\n%s", diff)
	}
}

func TestReportSettlesLaterWhenValkeyIsDown(t *testing.T) {
	harness := newReportHarness(t)
	checked := harness.check(t, videoCheck(veoModel, "8"))
	harness.cache.Redis().AddHook(unreachableRedisHook{})

	started := time.Now()
	result := harness.report(t, serverReport(checked.DecisionID, "6"))
	elapsed := time.Since(started)

	if result.Cost == nil || *result.Cost != *formattedDollars(90) || result.Duplicate {
		t.Errorf("result = %+v, want the stored entry costing 0.90", result)
	}
	if elapsed >= time.Second {
		t.Errorf("report took %s, want the settle to give up within its deadline", elapsed)
	}
	if !strings.Contains(harness.logs.String(), `"level":"WARN","msg":"decisions.settle_deferred"`) {
		t.Errorf("logs = %s, want a warn decisions.settle_deferred line", harness.logs.String())
	}
	if status, _ := harness.decisionStatus(t, checked.DecisionID); status != "settled" {
		t.Errorf("decision status = %s, want settled", status)
	}
}
