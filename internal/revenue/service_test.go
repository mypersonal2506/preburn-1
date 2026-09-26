package revenue_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/revenue"
	"github.com/preburn/preburn/internal/signals"
)

func TestRecordCreatesEntryAndRefreshesRollup(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	input := subscription(testExternalID, testSourceReference, dollars(30), augustStart, septemberStart)

	entry, duplicate := harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, input)

	customerID := harness.testCustomerID(t)
	want := revenue.Entry{
		ID:                 entry.ID,
		Environment:        httpapi.EnvironmentTest,
		CustomerID:         customerID,
		CustomerExternalID: testExternalID,
		Kind:               revenue.KindSubscription,
		Amount:             dollars(30),
		PeriodStart:        augustStart,
		PeriodEnd:          septemberStart,
		Source:             revenue.SourceAPI,
		SourceReference:    testSourceReference,
		OccurredAt:         testStart,
		CreatedAt:          testStart,
	}
	if diff := cmp.Diff(want, entry); diff != "" || duplicate {
		t.Errorf("recorded entry duplicate=%t mismatch (-want +got):\n%s", duplicate, diff)
	}
	wantRollups := []rollup{{PeriodStart: augustStart, PeriodEnd: septemberStart, RevenueNet: dollars(30)}}
	if diff := cmp.Diff(wantRollups, harness.rollups(t, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}

	harness.clock.Advance(time.Minute)
	again, duplicate := harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, input)

	if diff := cmp.Diff(want, again); diff != "" || !duplicate {
		t.Errorf("second record duplicate=%t, want the first entry with duplicate true (-want +got):\n%s", duplicate, diff)
	}
	if entries := harness.count(t, "revenue_entries"); entries != 1 {
		t.Errorf("revenue entries = %d, want 1", entries)
	}
	if diff := cmp.Diff(wantRollups, harness.rollups(t, customerID)); diff != "" {
		t.Errorf("rollups after the duplicate mismatch (-want +got):\n%s", diff)
	}
}

func TestRecordAttributesOverlappingSubscriptionsToTheirOwnPeriods(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, subscription(testExternalID, "august-first", dollars(30), augustStart, septemberStart))
	harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, subscription(testExternalID, "august-second", dollars(30), augustSecond, septemberSecond))

	want := []rollup{
		{PeriodStart: augustStart, PeriodEnd: septemberStart, RevenueNet: dollars(30)},
		{PeriodStart: augustSecond, PeriodEnd: septemberSecond, RevenueNet: dollars(30)},
	}
	if diff := cmp.Diff(want, harness.rollups(t, harness.testCustomerID(t))); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRecordFoldsZeroLengthRefundIntoBillingPeriod(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, subscription(testExternalID, "subscription", dollars(30), augustStart, septemberStart))
	refundDay := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)

	harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, revenue.RecordInput{
		CustomerExternalID: testExternalID,
		Kind:               revenue.KindRefund,
		Amount:             dollars(10),
		PeriodStart:        refundDay,
		PeriodEnd:          refundDay,
		SourceReference:    "refund",
	})

	want := []rollup{{PeriodStart: augustStart, PeriodEnd: septemberStart, RevenueNet: dollars(20)}}
	if diff := cmp.Diff(want, harness.rollups(t, harness.testCustomerID(t))); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRecordValidatesInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		change    func(input *revenue.RecordInput)
		locations []string
	}{
		{name: "unknown kind", change: func(input *revenue.RecordInput) { input.Kind = "tip" }, locations: []string{"body.kind"}},
		{name: "negative amount", change: func(input *revenue.RecordInput) { input.Amount = -1 }, locations: []string{"body.amount"}},
		{name: "period end before start", change: func(input *revenue.RecordInput) { input.PeriodEnd = augustStart.Add(-time.Second) }, locations: []string{"body.period_end"}},
		{name: "period longer than 400 days", change: func(input *revenue.RecordInput) {
			input.PeriodEnd = augustStart.AddDate(0, 0, 400).Add(time.Microsecond)
		}, locations: []string{"body.period_end"}},
		{name: "empty source reference", change: func(input *revenue.RecordInput) { input.SourceReference = "" }, locations: []string{"body.source_reference"}},
		{name: "source reference of 201 characters", change: func(input *revenue.RecordInput) {
			input.SourceReference = strings.Repeat("é", 201)
		}, locations: []string{"body.source_reference"}},
		{name: "every field invalid", change: func(input *revenue.RecordInput) {
			input.Kind = "tip"
			input.Amount = -1
			input.PeriodEnd = augustStart.Add(-time.Second)
			input.SourceReference = ""
		}, locations: []string{"body.kind", "body.amount", "body.period_end", "body.source_reference"}},
		{name: "invalid customer id", change: func(input *revenue.RecordInput) { input.CustomerExternalID = "acme corp" }, locations: []string{"body.customer_id"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			harness := newHarness(t)
			input := subscription(testExternalID, testSourceReference, dollars(30), augustStart, septemberStart)
			test.change(&input)

			_, _, err := harness.service.Record(t.Context(), httpapi.EnvironmentTest, revenue.SourceAPI, input)

			assertValidationProblem(t, err, test.locations...)
			if entries, customers := harness.count(t, "revenue_entries"), harness.count(t, "customers"); entries != 0 || customers != 0 {
				t.Errorf("revenue entries = %d and customers = %d after a rejected record, want 0 and 0", entries, customers)
			}
		})
	}
}

func TestRecordAcceptsBoundaryValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(input *revenue.RecordInput)
	}{
		{name: "zero amount", change: func(input *revenue.RecordInput) { input.Amount = 0 }},
		{name: "zero-length period", change: func(input *revenue.RecordInput) { input.PeriodEnd = input.PeriodStart }},
		{name: "period of 400 days", change: func(input *revenue.RecordInput) { input.PeriodEnd = augustStart.AddDate(0, 0, 400) }},
		{name: "source reference of 200 characters", change: func(input *revenue.RecordInput) { input.SourceReference = strings.Repeat("é", 200) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			harness := newHarness(t)
			input := subscription(testExternalID, testSourceReference, dollars(30), augustStart, septemberStart)
			test.change(&input)

			entry, duplicate := harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, input)

			if duplicate || entry.Amount != input.Amount || !entry.PeriodEnd.Equal(input.PeriodEnd) || entry.SourceReference != input.SourceReference {
				t.Errorf("entry = %+v duplicate=%t, want the recorded input", entry, duplicate)
			}
		})
	}
}

func TestRecordKeepsOccurredAt(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	input := subscription(testExternalID, testSourceReference, dollars(30), augustStart, septemberStart)
	input.OccurredAt = pointer(time.Date(2026, time.August, 1, 9, 30, 0, 0, time.FixedZone("UTC+2", 2*60*60)))

	entry, _ := harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, input)

	if want := time.Date(2026, time.August, 1, 7, 30, 0, 0, time.UTC); !entry.OccurredAt.Equal(want) {
		t.Errorf("occurred at = %s, want %s", entry.OccurredAt, want)
	}
}

func TestRecordCreatesUnknownCustomerOnDefaultPlan(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	defaultPlanID := harness.insertPlan(t, httpapi.EnvironmentLive, "Creator")
	harness.setDefaultPlan(t, httpapi.EnvironmentLive, defaultPlanID)

	entry, _ := harness.record(t, httpapi.EnvironmentLive, revenue.SourceAPI, subscription("newcomer", testSourceReference, dollars(30), septemberStart, octoberStart))

	customer, err := harness.customers.CustomerByExternalID(t.Context(), httpapi.EnvironmentLive, "newcomer")
	if err != nil {
		t.Fatalf("select created customer: %v", err)
	}
	if customer.ID != entry.CustomerID || customer.PlanID != nil {
		t.Errorf("customer = %+v, want the entry's customer %s without a plan of its own", customer, entry.CustomerID)
	}
	state, err := harness.states.Load(t.Context(), httpapi.EnvironmentLive, customer.ID, testStart)
	if err != nil {
		t.Fatalf("load customer state: %v", err)
	}
	if state.PlanID == nil || *state.PlanID != defaultPlanID || state.NetRevenue != dollars(30) {
		t.Errorf("state plan=%v net revenue=%s, want default plan %s and 30 USD", state.PlanID, money.FormatAmount(state.NetRevenue), defaultPlanID)
	}
}

func TestRecordRefreshesCachedCustomerState(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	harness.setDefaultPlan(t, httpapi.EnvironmentTest, harness.insertPlan(t, httpapi.EnvironmentTest, "Creator"))
	customer, err := harness.customers.Cache().Ensure(t.Context(), httpapi.EnvironmentTest, testExternalID)
	if err != nil {
		t.Fatalf("ensure customer: %v", err)
	}
	if allowance := costAllowance(t, harness, customer.ID); allowance != 0 {
		t.Fatalf("allowance before revenue = %s, want 0", money.FormatAmount(allowance))
	}

	harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, subscription(testExternalID, testSourceReference, dollars(30), septemberStart, octoberStart))

	if allowance := costAllowance(t, harness, customer.ID); allowance != dollars(18) {
		t.Errorf("allowance after revenue = %s, want 18 USD (30 USD revenue at a 40%% target margin)", money.FormatAmount(allowance))
	}
}

func TestRecordPublishesCustomerInvalidation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	subscription := subscribeInvalidations(t, harness.cache)

	entry, _ := harness.record(t, httpapi.EnvironmentLive, revenue.SourceStripe, revenue.RecordInput{
		CustomerExternalID: testExternalID,
		Kind:               revenue.KindStripeFee,
		Amount:             dollars(1),
		PeriodStart:        septemberStart,
		PeriodEnd:          septemberStart,
		SourceReference:    "txn_1Q2w3E4r5T6y",
	})

	assertInvalidation(t, subscription, cache.Invalidation{
		Kind:        cache.InvalidationKindCustomer,
		Environment: string(httpapi.EnvironmentLive),
		ID:          identifiers.Encode(identifiers.PrefixCustomer, entry.CustomerID),
	})
}

func TestRecordDuplicateReturnsFirstEntry(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	first, _ := harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, subscription(testExternalID, testSourceReference, dollars(30), augustStart, septemberStart))

	again, duplicate := harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, subscription("other", testSourceReference, dollars(99), septemberStart, octoberStart))

	if diff := cmp.Diff(first, again); diff != "" || !duplicate {
		t.Errorf("duplicate=%t, want the first entry (-want +got):\n%s", duplicate, diff)
	}
	wantRollups := []rollup{{PeriodStart: augustStart, PeriodEnd: septemberStart, RevenueNet: dollars(30)}}
	if diff := cmp.Diff(wantRollups, harness.rollups(t, first.CustomerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRecordKeepsSourcesKindsAndEnvironmentsApart(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	tests := []struct {
		environment httpapi.Environment
		source      revenue.Source
		kind        revenue.Kind
	}{
		{environment: httpapi.EnvironmentTest, source: revenue.SourceAPI, kind: revenue.KindSubscription},
		{environment: httpapi.EnvironmentTest, source: revenue.SourceStripe, kind: revenue.KindSubscription},
		{environment: httpapi.EnvironmentTest, source: revenue.SourceImport, kind: revenue.KindSubscription},
		{environment: httpapi.EnvironmentTest, source: revenue.SourceAPI, kind: revenue.KindCreditNote},
		{environment: httpapi.EnvironmentLive, source: revenue.SourceAPI, kind: revenue.KindSubscription},
	}
	for _, test := range tests {
		input := subscription(testExternalID, testSourceReference, dollars(30), augustStart, septemberStart)
		input.Kind = test.kind

		entry, duplicate := harness.record(t, test.environment, test.source, input)

		if duplicate || entry.Environment != test.environment || entry.Source != test.source || entry.Kind != test.kind {
			t.Errorf("entry = %+v duplicate=%t, want a new %s %s %s entry", entry, duplicate, test.environment, test.source, test.kind)
		}
	}
}

func TestConcurrentRecordsCreateOneEntry(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	const callers = 8
	input := subscription(testExternalID, testSourceReference, dollars(30), augustStart, septemberStart)
	entryIDs := make([]uuid.UUID, callers)
	duplicates := make([]bool, callers)
	var group sync.WaitGroup
	for caller := range callers {
		group.Go(func() {
			entry, duplicate, err := harness.service.Record(t.Context(), httpapi.EnvironmentTest, revenue.SourceAPI, input)
			if err != nil {
				t.Errorf("record from caller %d: %v", caller, err)
				return
			}
			entryIDs[caller] = entry.ID
			duplicates[caller] = duplicate
		})
	}
	group.Wait()

	created := 0
	for caller := range callers {
		if !duplicates[caller] {
			created++
		}
		if entryIDs[caller] != entryIDs[0] {
			t.Errorf("caller %d got entry %s, want %s", caller, entryIDs[caller], entryIDs[0])
		}
	}
	if created != 1 {
		t.Errorf("callers that created the entry = %d, want 1", created)
	}
	wantRollups := []rollup{{PeriodStart: augustStart, PeriodEnd: septemberStart, RevenueNet: dollars(30)}}
	if diff := cmp.Diff(wantRollups, harness.rollups(t, harness.testCustomerID(t))); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestListPagesNewestFirst(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	var recorded []revenue.Entry
	for _, reference := range []string{"first", "second", "third"} {
		entry, _ := harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, subscription(testExternalID, reference, dollars(30), augustStart, septemberStart))
		recorded = append(recorded, entry)
		harness.clock.Advance(time.Minute)
	}
	harness.record(t, httpapi.EnvironmentLive, revenue.SourceAPI, subscription(testExternalID, "live", dollars(30), augustStart, septemberStart))

	firstPage, cursor, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, revenue.ListFilter{}, "", 2)
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	secondPage, lastCursor, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, revenue.ListFilter{}, cursor, 2)
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}

	if diff := cmp.Diff([]revenue.Entry{recorded[2], recorded[1]}, firstPage); diff != "" || cursor == "" {
		t.Errorf("first page cursor=%q mismatch (-want +got):\n%s", cursor, diff)
	}
	if diff := cmp.Diff([]revenue.Entry{recorded[0]}, secondPage); diff != "" || lastCursor != "" {
		t.Errorf("second page cursor=%q mismatch (-want +got):\n%s", lastCursor, diff)
	}
}

func TestListFiltersByCustomerAndKind(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	acmeSubscription, _ := harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, subscription(testExternalID, "acme-subscription", dollars(30), augustStart, septemberStart))
	refund := subscription(testExternalID, "acme-refund", dollars(5), augustStart, augustStart)
	refund.Kind = revenue.KindRefund
	acmeRefund, _ := harness.record(t, httpapi.EnvironmentTest, revenue.SourceAPI, refund)
	otherSubscription, _ := harness.record(t, httpapi.EnvironmentTest, revenue.SourceStripe, subscription("cedar", "cedar-subscription", dollars(20), augustStart, septemberStart))
	tests := []struct {
		name   string
		filter revenue.ListFilter
		want   []revenue.Entry
	}{
		{name: "customer", filter: revenue.ListFilter{CustomerExternalID: testExternalID}, want: []revenue.Entry{acmeRefund, acmeSubscription}},
		{name: "kind", filter: revenue.ListFilter{Kind: revenue.KindSubscription}, want: []revenue.Entry{otherSubscription, acmeSubscription}},
		{name: "customer and kind", filter: revenue.ListFilter{CustomerExternalID: testExternalID, Kind: revenue.KindRefund}, want: []revenue.Entry{acmeRefund}},
		{name: "unknown customer", filter: revenue.ListFilter{CustomerExternalID: "lumber"}, want: []revenue.Entry{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			listed, cursor, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, test.filter, "", 0)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if diff := cmp.Diff(test.want, listed); diff != "" || cursor != "" {
				t.Errorf("listed cursor=%q mismatch (-want +got):\n%s", cursor, diff)
			}
		})
	}
}

func TestListRejectsInvalidPaging(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	_, _, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, revenue.ListFilter{}, "not-a-cursor", 0)
	if !errors.Is(err, httpapi.ErrInvalidCursor) {
		t.Errorf("list with a malformed cursor error = %v, want ErrInvalidCursor", err)
	}
	_, _, err = harness.service.List(t.Context(), httpapi.EnvironmentTest, revenue.ListFilter{}, "", httpapi.ListLimitMaximum+1)
	assertValidationProblem(t, err, "query.limit")
}

func costAllowance(t *testing.T, harness *harness, customerID uuid.UUID) money.Amount {
	t.Helper()
	state, err := harness.states.Load(t.Context(), httpapi.EnvironmentTest, customerID, testStart)
	if err != nil {
		t.Fatalf("load customer state: %v", err)
	}
	computed, err := signals.Compute(state, signals.CounterSnapshot{}, nil, testStart)
	if err != nil {
		t.Fatalf("compute signals: %v", err)
	}
	return computed.CostAllowance
}
