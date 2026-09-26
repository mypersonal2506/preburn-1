package dashboard_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/policies"
)

type eventFixture struct {
	acme          uuid.UUID
	cedar         uuid.UUID
	allowAcme     uuid.UUID
	acmeUsage     uuid.UUID
	denyCedar     uuid.UUID
	fallbackUsage uuid.UUID
	routeAcme     uuid.UUID
}

func seedEvents(t *testing.T, harness *harness) eventFixture {
	t.Helper()
	test := httpapi.EnvironmentTest
	fixture := eventFixture{
		acme:  harness.insertCustomer(t, test, "acme", pointer("Acme Inc"), nil),
		cedar: harness.insertCustomer(t, test, "cedar", nil, nil),
	}
	usage := func(environment httpapi.Environment, customerID uuid.UUID, cost float64, occurredAt time.Time) ledgerFixture {
		entry := ledgerFixture{
			environment: environment, customerID: customerID, feature: textToVideo, model: veoModel,
			periodStart: septemberStart, periodEnd: octoberStart, occurredAt: occurredAt,
		}
		if cost > 0 {
			entry.cost = amount(cost)
		}
		return entry
	}
	fixture.allowAcme = harness.storeDecision(t, allowedDecision(test, fixture.acme, checkedAt(14)))
	fixture.acmeUsage = harness.insertLedgerEntry(t, usage(test, fixture.acme, 1.8, checkedAt(14).Add(time.Minute)))
	harness.stampLedgerEntry(t, fixture.acmeUsage, &fixture.allowAcme, checkedAt(14).Add(2*time.Minute))
	denied := allowedDecision(test, fixture.cedar, checkedAt(14).Add(time.Hour))
	denied.outcome, denied.reason, denied.reserved, denied.status = policies.OutcomeDeny, policies.ReasonUncostedDenied, 0, "unreserved"
	fixture.denyCedar = harness.storeDecision(t, denied)
	fixture.fallbackUsage = harness.insertLedgerEntry(t, usage(test, fixture.cedar, 0, checkedAt(14).Add(time.Hour)))
	routed := allowedDecision(test, fixture.acme, checkedAt(15))
	routed.model, routed.outcome, routed.reason, routed.estimatedCost = klingPro, policies.OutcomeRoute, policies.ReasonPolicyMatched, amount(1.5)
	fixture.routeAcme = harness.storeDecision(t, routed)

	liveCustomer := harness.insertCustomer(t, httpapi.EnvironmentLive, "acme", nil, nil)
	harness.storeDecision(t, allowedDecision(httpapi.EnvironmentLive, liveCustomer, checkedAt(15)))
	harness.insertLedgerEntry(t, usage(httpapi.EnvironmentLive, liveCustomer, 1, checkedAt(15)))
	return fixture
}

func eventIDs(page httpapi.PageBody[dashboard.ActivityEventResponse]) []string {
	listed := []string{}
	for _, event := range page.Items {
		listed = append(listed, event.ID)
	}
	return listed
}

func TestEventListMergesDecisionsAndLedgerEntriesNewestFirst(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	fixture := seedEvents(t, harness)

	page := decodeInto[httpapi.PageBody[dashboard.ActivityEventResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, eventsPath))

	acmeFields := func(event dashboard.ActivityEventResponse) dashboard.ActivityEventResponse {
		event.CustomerID, event.CustomerExternalID, event.CustomerDisplayName = encodedCustomer(fixture.acme), "acme", pointer("Acme Inc")
		event.Feature, event.Provider = textToVideo, falProvider
		return event
	}
	cedarFields := func(event dashboard.ActivityEventResponse) dashboard.ActivityEventResponse {
		event.CustomerID, event.CustomerExternalID = encodedCustomer(fixture.cedar), "cedar"
		event.Feature, event.Provider = textToVideo, falProvider
		return event
	}
	want := []dashboard.ActivityEventResponse{
		acmeFields(dashboard.ActivityEventResponse{
			Kind: dashboard.EventKindDecision, ID: encodedDecision(fixture.routeAcme), OccurredAt: checkedAt(15),
			Model: klingPro, Outcome: pointer(policies.OutcomeRoute), Cost: pointer("1.500000000"),
		}),
		cedarFields(dashboard.ActivityEventResponse{
			Kind: dashboard.EventKindLedgerEntry, ID: encodedLedgerEntry(fixture.fallbackUsage), OccurredAt: checkedAt(14).Add(time.Hour),
			Model: veoModel,
		}),
		cedarFields(dashboard.ActivityEventResponse{
			Kind: dashboard.EventKindDecision, ID: encodedDecision(fixture.denyCedar), OccurredAt: checkedAt(14).Add(time.Hour),
			Model: veoModel, Outcome: pointer(policies.OutcomeDeny), Cost: pointer("2.000000000"),
		}),
		acmeFields(dashboard.ActivityEventResponse{
			Kind: dashboard.EventKindLedgerEntry, ID: encodedLedgerEntry(fixture.acmeUsage), OccurredAt: checkedAt(14).Add(time.Minute),
			Model: veoModel, Cost: pointer("1.800000000"), DecisionID: pointer(encodedDecision(fixture.allowAcme)),
		}),
		acmeFields(dashboard.ActivityEventResponse{
			Kind: dashboard.EventKindDecision, ID: encodedDecision(fixture.allowAcme), OccurredAt: checkedAt(14),
			Model: veoModel, Outcome: pointer(policies.OutcomeAllow), Cost: pointer("2.000000000"),
		}),
	}
	if diff := cmp.Diff(want, page.Items); diff != "" {
		t.Errorf("events mismatch (-want +got):\n%s", diff)
	}
	if page.NextCursor != nil {
		t.Errorf("next_cursor = %s, want null", *page.NextCursor)
	}
}

func TestEventListPagesAcrossBothRecords(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	fixture := seedEvents(t, harness)

	var pages [][]string
	target := eventsPath + "?limit=2"
	for range 3 {
		page := decodeInto[httpapi.PageBody[dashboard.ActivityEventResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, target))
		pages = append(pages, eventIDs(page))
		if page.NextCursor == nil {
			break
		}
		target = eventsPath + "?limit=2&cursor=" + *page.NextCursor
	}

	want := [][]string{
		{encodedDecision(fixture.routeAcme), encodedLedgerEntry(fixture.fallbackUsage)},
		{encodedDecision(fixture.denyCedar), encodedLedgerEntry(fixture.acmeUsage)},
		{encodedDecision(fixture.allowAcme)},
	}
	if diff := cmp.Diff(want, pages); diff != "" {
		t.Errorf("pages mismatch (-want +got):\n%s", diff)
	}
}

func TestEventListStaysInItsEnvironment(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	emptyPage := decodeInto[httpapi.PageBody[dashboard.ActivityEventResponse]](t, harness.memberGet(t, httpapi.EnvironmentLive, eventsPath))
	seedEvents(t, harness)

	livePage := decodeInto[httpapi.PageBody[dashboard.ActivityEventResponse]](t, harness.memberGet(t, httpapi.EnvironmentLive, eventsPath))

	if emptyPage.Items == nil || len(emptyPage.Items) != 0 || emptyPage.NextCursor != nil {
		t.Errorf("empty environment lists %v with next_cursor %v, want an empty list and null", emptyPage.Items, emptyPage.NextCursor)
	}
	var kinds []dashboard.EventKind
	for _, event := range livePage.Items {
		kinds = append(kinds, event.Kind)
	}
	if diff := cmp.Diff([]dashboard.EventKind{dashboard.EventKindLedgerEntry, dashboard.EventKindDecision}, kinds); diff != "" {
		t.Errorf("live event kinds mismatch (-want +got):\n%s", diff)
	}
}

func TestEventListRejectsInvalidQuery(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)
	fixture := seedDecisionList(t, harness)
	decisionPage := decodeInto[httpapi.PageBody[dashboard.DecisionResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, decisionsPath+"?limit=1"))
	if decisionPage.Items[0].ID != encodedDecision(fixture.secondTie) {
		t.Fatalf("first decision = %s, want the newest", decisionPage.Items[0].ID)
	}
	eventPage := decodeInto[httpapi.PageBody[dashboard.ActivityEventResponse]](t, harness.memberGet(t, httpapi.EnvironmentTest, eventsPath+"?limit=1"))

	assertProblem(t, harness.memberGet(t, httpapi.EnvironmentTest, eventsPath+"?cursor="+*decisionPage.NextCursor), http.StatusUnprocessableEntity, "invalid_cursor")
	assertProblem(t, harness.memberGet(t, httpapi.EnvironmentLive, eventsPath+"?limit=1&cursor="+*eventPage.NextCursor), http.StatusUnprocessableEntity, "invalid_cursor")
	assertProblem(t, harness.memberGet(t, httpapi.EnvironmentTest, eventsPath+"?limit=101"), http.StatusUnprocessableEntity, "validation_failed", "query.limit")
}

func TestEventListDocumentsNullOutcome(t *testing.T) {
	t.Parallel()
	harness := newDecisionHarness(t)

	document := decodeInto[openAPIDocument](t, harness.serve(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/openapi.json", nil)))

	outcomes := document.Components.Schemas["ActivityEventResponse"].Properties["outcome"]["enum"]
	if diff := cmp.Diff([]any{"allow", "route", "cap", "deny", nil}, outcomes); diff != "" {
		t.Errorf("outcome enum mismatch (-want +got):\n%s", diff)
	}
}
