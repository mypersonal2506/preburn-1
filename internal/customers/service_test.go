package customers_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/plans"
)

const concurrentEnsureCalls = 20

func TestUpsertCreatesThenReplacesCustomer(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	planID := harness.createPlan(t, httpapi.EnvironmentTest, "Pro", "active")

	created := harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{
		DisplayName: pointer("Acme"),
		PlanID:      &planID,
		Metadata:    metadata(t, `{"tier": "gold", "seats": 12}`),
	})
	harness.clock.Advance(time.Minute)
	replaced := harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{DisplayName: pointer("Acme Corporation")})

	wantCreated := customers.Customer{
		ID:          created.ID,
		Environment: httpapi.EnvironmentTest,
		ExternalID:  testExternalID,
		DisplayName: pointer("Acme"),
		PlanID:      &planID,
		Metadata:    metadata(t, `{"seats":12,"tier":"gold"}`),
		Status:      "active",
		CreatedAt:   testStart,
		UpdatedAt:   testStart,
	}
	if diff := cmp.Diff(wantCreated, created); diff != "" {
		t.Errorf("created customer mismatch (-want +got):\n%s", diff)
	}
	wantReplaced := customers.Customer{
		ID:          created.ID,
		Environment: httpapi.EnvironmentTest,
		ExternalID:  testExternalID,
		DisplayName: pointer("Acme Corporation"),
		Metadata:    metadata(t, `{}`),
		Status:      "active",
		CreatedAt:   testStart,
		UpdatedAt:   testStart.Add(time.Minute),
	}
	if diff := cmp.Diff(wantReplaced, replaced); diff != "" {
		t.Errorf("replaced customer mismatch (-want +got):\n%s", diff)
	}
	if count := harness.testCustomerCount(t, testExternalID); count != 1 {
		t.Errorf("customer rows = %d, want 1", count)
	}
}

func TestUpsertKeepsEnvironmentsApart(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	testCustomer := harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{DisplayName: pointer("Acme test")})
	liveCustomer := harness.upsert(t, httpapi.EnvironmentLive, testExternalID, customers.UpsertInput{DisplayName: pointer("Acme live")})

	if testCustomer.ID == liveCustomer.ID {
		t.Fatalf("test and live customers share id %s", testCustomer.ID)
	}
	reloaded, err := harness.service.CustomerByExternalID(t.Context(), httpapi.EnvironmentTest, testExternalID)
	if err != nil {
		t.Fatalf("load test customer: %v", err)
	}
	if *reloaded.DisplayName != "Acme test" {
		t.Errorf("test customer display name = %s, want Acme test", *reloaded.DisplayName)
	}
}

func TestUpsertRequiresActivePlanOfEnvironment(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	tests := []struct {
		name   string
		planID uuid.UUID
	}{
		{name: "unknown plan", planID: identifiers.New()},
		{name: "archived plan", planID: harness.createPlan(t, httpapi.EnvironmentTest, "Legacy", "archived")},
		{name: "disabled plan", planID: harness.createPlan(t, httpapi.EnvironmentTest, "Paused", "disabled")},
		{name: "plan of the live environment", planID: harness.createPlan(t, httpapi.EnvironmentLive, "Pro", "active")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := harness.service.Upsert(t.Context(), httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{PlanID: &test.planID})

			if !errors.Is(err, plans.ErrPlanNotFound) {
				t.Errorf("error = %v, want plans.ErrPlanNotFound", err)
			}
		})
	}
	if count := harness.testCustomerCount(t, testExternalID); count != 0 {
		t.Errorf("customer rows = %d after rejected upserts, want 0", count)
	}
}

func TestUpsertValidatesInput(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	tests := []struct {
		name       string
		externalID string
		input      customers.UpsertInput
		locations  []string
	}{
		{name: "empty external id", externalID: "", locations: []string{"path.external_id"}},
		{name: "129 character external id", externalID: strings.Repeat("a", 129), locations: []string{"path.external_id"}},
		{name: "space in external id", externalID: "acme corp", locations: []string{"path.external_id"}},
		{name: "slash in external id", externalID: "acme/1", locations: []string{"path.external_id"}},
		{name: "letter outside ASCII in external id", externalID: "acmé", locations: []string{"path.external_id"}},
		{name: "empty display name", externalID: testExternalID, input: customers.UpsertInput{DisplayName: pointer("")}, locations: []string{"body.display_name"}},
		{name: "display name of spaces", externalID: testExternalID, input: customers.UpsertInput{DisplayName: pointer("   ")}, locations: []string{"body.display_name"}},
		{name: "201 character display name", externalID: testExternalID, input: customers.UpsertInput{DisplayName: pointer(strings.Repeat("é", 201))}, locations: []string{"body.display_name"}},
		{name: "control character in display name", externalID: testExternalID, input: customers.UpsertInput{DisplayName: pointer("Acme\nCorp")}, locations: []string{"body.display_name"}},
		{name: "51 metadata keys", externalID: testExternalID, input: customers.UpsertInput{Metadata: metadata(t, metadataWithKeys(51))}, locations: []string{"body.metadata"}},
		{name: "4097 bytes of metadata", externalID: testExternalID, input: customers.UpsertInput{Metadata: metadata(t, metadataOfBytes(4097))}, locations: []string{"body.metadata"}},
		{name: "every field", externalID: "", input: customers.UpsertInput{DisplayName: pointer(""), Metadata: metadata(t, metadataWithKeys(51))}, locations: []string{"path.external_id", "body.display_name", "body.metadata"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := harness.service.Upsert(t.Context(), httpapi.EnvironmentTest, test.externalID, test.input)

			assertValidationProblem(t, err, test.locations...)
		})
	}
	boundary := customers.UpsertInput{DisplayName: pointer(strings.Repeat("é", 200)), Metadata: metadata(t, metadataWithKeys(50))}
	harness.upsert(t, httpapi.EnvironmentTest, strings.Repeat("a", 128), boundary)
	harness.upsert(t, httpapi.EnvironmentTest, "Acme.Corp_1:eu@west-2", customers.UpsertInput{Metadata: metadata(t, metadataOfBytes(4096))})
}

func TestEnsureCreatesCustomerOnce(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	first, err := harness.service.Ensure(t.Context(), httpapi.EnvironmentLive, testExternalID)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	harness.clock.Advance(time.Minute)
	second, err := harness.service.Ensure(t.Context(), httpapi.EnvironmentLive, testExternalID)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	want := customers.Customer{
		ID:          first.ID,
		Environment: httpapi.EnvironmentLive,
		ExternalID:  testExternalID,
		Metadata:    metadata(t, `{}`),
		Status:      "active",
		CreatedAt:   testStart,
		UpdatedAt:   testStart,
	}
	if diff := cmp.Diff(want, first); diff != "" {
		t.Errorf("created customer mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(first, second); diff != "" {
		t.Errorf("second ensure mismatch (-first +second):\n%s", diff)
	}
}

func TestEnsureReturnsUpsertedCustomer(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	planID := harness.createPlan(t, httpapi.EnvironmentTest, "Pro", "active")
	upserted := harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{DisplayName: pointer("Acme"), PlanID: &planID})

	ensured, err := harness.service.Ensure(t.Context(), httpapi.EnvironmentTest, testExternalID)

	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if diff := cmp.Diff(upserted, ensured); diff != "" {
		t.Errorf("ensured customer mismatch (-upserted +ensured):\n%s", diff)
	}
}

func TestConcurrentEnsureCreatesOneCustomer(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	configuration := harness.pool.Config().Copy()
	configuration.MaxConns = concurrentEnsureCalls
	pool, err := pgxpool.NewWithConfig(t.Context(), configuration)
	if err != nil {
		t.Fatalf("open pool of %d connections: %v", concurrentEnsureCalls, err)
	}
	t.Cleanup(pool.Close)
	service := customers.NewService(pool, harness.cache, harness.clock)
	start := make(chan struct{})
	results := make([]customers.Customer, concurrentEnsureCalls)
	errs := make([]error, concurrentEnsureCalls)
	var group sync.WaitGroup
	for call := range concurrentEnsureCalls {
		group.Go(func() {
			<-start
			results[call], errs[call] = service.Ensure(t.Context(), httpapi.EnvironmentTest, testExternalID)
		})
	}

	close(start)
	group.Wait()

	for call := range concurrentEnsureCalls {
		if errs[call] != nil {
			t.Fatalf("ensure call %d: %v", call, errs[call])
		}
		if results[call].ID != results[0].ID {
			t.Errorf("ensure call %d returned customer %s, want %s", call, results[call].ID, results[0].ID)
		}
	}
	if count := harness.testCustomerCount(t, testExternalID); count != 1 {
		t.Errorf("customer rows = %d, want 1", count)
	}
}

func TestEnsureRejectsInvalidExternalID(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	_, err := harness.service.Ensure(t.Context(), httpapi.EnvironmentTest, "acme corp")

	assertValidationProblem(t, err, "body.customer_id")
	if count := harness.testCustomerCount(t, "acme corp"); count != 0 {
		t.Errorf("customer rows = %d, want 0", count)
	}
}

func TestEnsureUserCreatesUserOncePerCustomer(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	acme := harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{})
	cedar := harness.upsert(t, httpapi.EnvironmentTest, "cedar", customers.UpsertInput{})

	first, err := harness.service.EnsureUser(t.Context(), httpapi.EnvironmentTest, acme.ID, "user-1")
	if err != nil {
		t.Fatalf("first ensure user: %v", err)
	}
	harness.clock.Advance(time.Minute)
	second, err := harness.service.EnsureUser(t.Context(), httpapi.EnvironmentTest, acme.ID, "user-1")
	if err != nil {
		t.Fatalf("second ensure user: %v", err)
	}
	otherCustomer, err := harness.service.EnsureUser(t.Context(), httpapi.EnvironmentTest, cedar.ID, "user-1")
	if err != nil {
		t.Fatalf("ensure user of another customer: %v", err)
	}

	want := customers.CustomerUser{
		ID:          first.ID,
		Environment: httpapi.EnvironmentTest,
		CustomerID:  acme.ID,
		ExternalID:  "user-1",
		CreatedAt:   testStart,
	}
	if diff := cmp.Diff(want, first); diff != "" {
		t.Errorf("created user mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(first, second); diff != "" {
		t.Errorf("second ensure user mismatch (-first +second):\n%s", diff)
	}
	if otherCustomer.ID == first.ID || otherCustomer.CustomerID != cedar.ID {
		t.Errorf("user of another customer = %+v, want a separate user of %s", otherCustomer, cedar.ID)
	}
}

func TestConcurrentEnsureUserCreatesOneUser(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	customer := harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{})
	start := make(chan struct{})
	results := make([]customers.CustomerUser, concurrentEnsureCalls)
	errs := make([]error, concurrentEnsureCalls)
	var group sync.WaitGroup
	for call := range concurrentEnsureCalls {
		group.Go(func() {
			<-start
			results[call], errs[call] = harness.service.EnsureUser(t.Context(), httpapi.EnvironmentTest, customer.ID, "user-1")
		})
	}

	close(start)
	group.Wait()

	for call := range concurrentEnsureCalls {
		if errs[call] != nil {
			t.Fatalf("ensure user call %d: %v", call, errs[call])
		}
		if results[call].ID != results[0].ID {
			t.Errorf("ensure user call %d returned user %s, want %s", call, results[call].ID, results[0].ID)
		}
	}
	var count int
	if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM customer_users WHERE customer_id = $1", customer.ID).Scan(&count); err != nil {
		t.Fatalf("count customer users: %v", err)
	}
	if count != 1 {
		t.Errorf("customer user rows = %d, want 1", count)
	}
}

func TestEnsureUserRejectsInvalidExternalID(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	customer := harness.upsert(t, httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{})

	_, err := harness.service.EnsureUser(t.Context(), httpapi.EnvironmentTest, customer.ID, strings.Repeat("u", 129))

	assertValidationProblem(t, err, "body.customer_user_id")
}

func TestCustomerLookupsStayInEnvironment(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	customer := harness.upsert(t, httpapi.EnvironmentLive, testExternalID, customers.UpsertInput{DisplayName: pointer("Acme")})

	byID, err := harness.service.Customer(t.Context(), httpapi.EnvironmentLive, customer.ID)
	if err != nil {
		t.Fatalf("load customer by id: %v", err)
	}
	byExternalID, err := harness.service.CustomerByExternalID(t.Context(), httpapi.EnvironmentLive, testExternalID)
	if err != nil {
		t.Fatalf("load customer by external id: %v", err)
	}

	if diff := cmp.Diff(customer, byID); diff != "" {
		t.Errorf("customer by id mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(customer, byExternalID); diff != "" {
		t.Errorf("customer by external id mismatch (-want +got):\n%s", diff)
	}
	if _, err := harness.service.Customer(t.Context(), httpapi.EnvironmentTest, customer.ID); !errors.Is(err, httpapi.ErrNotFound) {
		t.Errorf("customer by id in the test environment error = %v, want ErrNotFound", err)
	}
	if _, err := harness.service.CustomerByExternalID(t.Context(), httpapi.EnvironmentTest, testExternalID); !errors.Is(err, httpapi.ErrNotFound) {
		t.Errorf("customer by external id in the test environment error = %v, want ErrNotFound", err)
	}
	if count := harness.testCustomerCount(t, testExternalID); count != 0 {
		t.Errorf("test customer rows = %d after lookups, want 0", count)
	}
}

func TestListSearchesExternalIDPrefixIgnoringCase(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	for _, externalID := range []string{"acme-eu", "ACME-US", "acmex", "beta-acme", "a_c", "abc"} {
		harness.upsert(t, httpapi.EnvironmentTest, externalID, customers.UpsertInput{})
		harness.clock.Advance(time.Second)
	}
	harness.upsert(t, httpapi.EnvironmentLive, "acme-live", customers.UpsertInput{})
	tests := []struct {
		search string
		want   []string
	}{
		{search: "", want: []string{"abc", "a_c", "beta-acme", "acmex", "ACME-US", "acme-eu"}},
		{search: "acme", want: []string{"acmex", "ACME-US", "acme-eu"}},
		{search: "AcMe-", want: []string{"ACME-US", "acme-eu"}},
		{search: "acme-us", want: []string{"ACME-US"}},
		{search: "a_c", want: []string{"a_c"}},
		{search: "zeta", want: nil},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("search %q", test.search), func(t *testing.T) {
			listed, nextCursor, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, test.search, "", 0)
			if err != nil {
				t.Fatalf("list: %v", err)
			}

			if diff := cmp.Diff(test.want, externalIDs(listed)); diff != "" {
				t.Errorf("listed external ids mismatch (-want +got):\n%s", diff)
			}
			if nextCursor != "" {
				t.Errorf("next cursor = %q on the only page, want empty", nextCursor)
			}
		})
	}
	_, _, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, "acme%", "", 0)
	assertValidationProblem(t, err, "query.search")
}

func TestListPagesNewestFirst(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	for _, externalID := range []string{"acme-1", "acme-2", "acme-3"} {
		harness.upsert(t, httpapi.EnvironmentTest, externalID, customers.UpsertInput{})
	}
	harness.upsert(t, httpapi.EnvironmentTest, "beta", customers.UpsertInput{})

	var pages [][]string
	cursor := ""
	for {
		listed, nextCursor, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, "acme", cursor, 2)
		if err != nil {
			t.Fatalf("list page %d: %v", len(pages)+1, err)
		}
		pages = append(pages, externalIDs(listed))
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	if diff := cmp.Diff([][]string{{"acme-3", "acme-2"}, {"acme-1"}}, pages); diff != "" {
		t.Errorf("pages mismatch (-want +got):\n%s", diff)
	}
	if _, _, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, "", "not-a-cursor", 0); !errors.Is(err, httpapi.ErrInvalidCursor) {
		t.Errorf("list with a foreign cursor error = %v, want ErrInvalidCursor", err)
	}
	_, _, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, "", "", httpapi.ListLimitMaximum+1)
	assertValidationProblem(t, err, "query.limit")
}

func externalIDs(listed []customers.Customer) []string {
	var values []string
	for _, customer := range listed {
		values = append(values, customer.ExternalID)
	}
	return values
}

func metadataWithKeys(count int) string {
	entries := make([]string, 0, count)
	for key := range count {
		entries = append(entries, fmt.Sprintf(`"key_%d": %d`, key, key))
	}
	return "{" + strings.Join(entries, ", ") + "}"
}

func metadataOfBytes(size int) string {
	const envelope = `{"notes":""}`
	return `{"notes":"` + strings.Repeat("<", size-len(envelope)) + `"}`
}
