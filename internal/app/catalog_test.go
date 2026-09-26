package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	testExternalID       = "acme"
	testCustomerName     = "Acme"
	unpricedProvider     = "example"
	unpricedModel        = "unpriced-model"
	overrideUnitPrice    = "1"
	overrideUnitQuantity = 1_000_000
	quotedOutputTokens   = "10"
	sessionUserAgent     = "test"
	jsonContentType      = "application/json"
	contentTypeHeader    = "Content-Type"
)

type authorization func(request *http.Request)

func TestServeRegistersCatalogCustomerPlanAndSettingsRoutes(t *testing.T) {
	application, logs := newTestApp(t, RoleAPI)
	baseURL, _ := startServer(t, application, logs)
	_, adminSecret, err := application.APIKeys.Create(t.Context(), httpapi.EnvironmentTest, testKeyName, apikeys.ScopeAdmin, nil)
	if err != nil {
		t.Fatalf("create admin key: %v", err)
	}
	_, runtimeSecret, err := application.APIKeys.Create(t.Context(), httpapi.EnvironmentTest, testKeyName, apikeys.ScopeRuntime, nil)
	if err != nil {
		t.Fatalf("create runtime key: %v", err)
	}
	member, err := application.Installation.CreateAdmin(t.Context(), testEmail, testDisplayName, testPassword)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	sessionToken, err := application.Members.Sessions().Start(t.Context(), member.ID, sessionUserAgent, netip.MustParseAddr("127.0.0.1"))
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	adminKey := bearer(adminSecret)
	runtimeKey := bearer(runtimeSecret)
	session := func(request *http.Request) {
		request.AddCookie(&http.Cookie{Name: members.SessionCookieName, Value: sessionToken}) //nolint:gosec // G124: a request cookie carries no attributes.
		request.Header.Set(httpapi.EnvironmentHeader, string(httpapi.EnvironmentTest))
	}

	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		authorize authorization
		status    int
		contains  string
	}{
		{name: "pricing meters", method: http.MethodGet, path: "/api/v1/pricing/meters", authorize: adminKey, status: http.StatusOK, contains: `"output_tokens"`},
		{name: "pricing models", method: http.MethodGet, path: "/api/v1/pricing/models", authorize: adminKey, status: http.StatusOK, contains: `"items"`},
		{name: "pricing quote", method: http.MethodPost, path: "/api/v1/pricing/quote", body: `{"provider":"example","model":"unpriced-model","usage":{"output_tokens":"10"}}`, authorize: adminKey, status: http.StatusOK, contains: `"cost_status":"uncosted"`},
		{name: "plans", method: http.MethodGet, path: "/api/v1/plans", authorize: adminKey, status: http.StatusOK, contains: `"items"`},
		{name: "customer upsert", method: http.MethodPut, path: "/api/v1/customers/" + testExternalID, body: `{}`, authorize: runtimeKey, status: http.StatusOK, contains: `"external_id":"acme"`},
		{name: "settings", method: http.MethodGet, path: "/api/v1/settings", authorize: session, status: http.StatusOK, contains: `"installation_name"`},
		{name: "settings with an admin key", method: http.MethodGet, path: "/api/v1/settings", authorize: adminKey, status: http.StatusForbidden, contains: `"scope_forbidden"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, body := send(t, test.method, baseURL+test.path, test.body, test.authorize)

			if status != test.status {
				t.Fatalf("status = %d, want %d, body %s", status, test.status, body)
			}
			if !strings.Contains(body, test.contains) {
				t.Errorf("body = %s, want it to contain %s", body, test.contains)
			}
		})
	}
}

func TestServeClearsCustomersAndRuleSetsChangedByAnotherProcess(t *testing.T) {
	application, logs := newTestApp(t, RoleAPI)
	startServer(t, application, logs)
	waitForInvalidationSubscriber(t, application)
	assertCustomerName(t, application, "")
	assertQuoteStatus(t, application, pricing.CostStatusUncosted)

	changeCustomerAndPricingInAnotherProcess(t, application)

	waitFor(t, "customer display name after the upsert of another process", func() bool {
		return customerName(t, application) == testCustomerName
	})
	waitFor(t, "costed quote after the override of another process", func() bool {
		return quote(t, application).CostStatus == pricing.CostStatusCosted
	})
}

func TestResubscribedClearsEveryCacheAndRebuildsCounters(t *testing.T) {
	application, _ := newTestApp(t, RoleAPI)
	_, resubscribed := application.invalidationHandlers()
	key, secret, err := application.APIKeys.Create(t.Context(), httpapi.EnvironmentTest, testKeyName, apikeys.ScopeRuntime, nil)
	if err != nil {
		t.Fatalf("create runtime key: %v", err)
	}
	assertKeyAuthenticates(t, application, secret, nil)
	assertCustomerName(t, application, "")
	assertQuoteStatus(t, application, pricing.CostStatusUncosted)
	customerID := customerIDOf(t, application)
	assertActivePolicies(t, application, 0)
	assertAllowance(t, application, customerID, 0)
	otherKeys := apikeys.NewService(application.Pool, application.Cache, application.Clock, application.Logger)
	if err := otherKeys.Revoke(t.Context(), httpapi.EnvironmentTest, key.ID); err != nil {
		t.Fatalf("revoke from another process: %v", err)
	}
	changeCustomerAndPricingInAnotherProcess(t, application)
	changePoliciesAndDefaultPlanInAnotherProcess(t, application)
	assertKeyAuthenticates(t, application, secret, nil)
	assertCustomerName(t, application, "")
	assertQuoteStatus(t, application, pricing.CostStatusUncosted)
	assertActivePolicies(t, application, 0)
	assertAllowance(t, application, customerID, 0)
	if countersReady(t, application) {
		t.Fatal("counters ready before the resubscribe, want the marker missing")
	}

	rebuildContext, stopRebuilds := context.WithCancel(t.Context())
	defer stopRebuilds()
	go application.CounterBootstrap.ServeRebuildRequests(rebuildContext)

	resubscribed()

	assertKeyAuthenticates(t, application, secret, httpapi.ErrAuthenticationRequired)
	assertCustomerName(t, application, testCustomerName)
	assertQuoteStatus(t, application, pricing.CostStatusCosted)
	assertActivePolicies(t, application, 1)
	assertAllowance(t, application, customerID, testAllowanceNanos)
	waitFor(t, "counters ready after the resubscribe", func() bool {
		return countersReady(t, application)
	})
}

func TestRegisterJobsAddsLiteLLMRefreshOnlyWhenEnabled(t *testing.T) {
	tests := []struct {
		name         string
		enabled      bool
		periodicJobs int
		registered   bool
	}{
		{name: "refresh off", enabled: false, periodicJobs: 5, registered: false},
		{name: "refresh on", enabled: true, periodicJobs: 6, registered: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := testConfiguration(t)
			configuration.PricingLiteLLMRefresh = test.enabled
			application, _ := openTestApp(t, configuration, RoleWorker)

			workers, periodicJobs, err := RegisterJobs(application)
			if err != nil {
				t.Fatalf("register jobs: %v", err)
			}

			if len(periodicJobs) != test.periodicJobs {
				t.Errorf("periodic jobs = %d, want %d", len(periodicJobs), test.periodicJobs)
			}
			client, err := river.NewClient(riverpgxv5.New(application.Pool), &river.Config{Workers: workers})
			if err != nil {
				t.Fatalf("new river client: %v", err)
			}
			_, err = client.Insert(t.Context(), pricing.LiteLLMRefreshArgs{}, nil)
			var unknownKind *river.UnknownJobKindError
			if err != nil && !errors.As(err, &unknownKind) {
				t.Fatalf("insert pricing_litellm_refresh: %v", err)
			}
			if registered := err == nil; registered != test.registered {
				t.Errorf("pricing_litellm_refresh registered = %t, want %t", registered, test.registered)
			}
		})
	}
}

func changeCustomerAndPricingInAnotherProcess(t *testing.T, application *App) {
	t.Helper()
	name := testCustomerName
	otherCustomers := customers.NewService(application.Pool, application.Cache, application.Clock)
	if _, err := otherCustomers.Upsert(t.Context(), httpapi.EnvironmentTest, testExternalID, customers.UpsertInput{DisplayName: &name}); err != nil {
		t.Fatalf("upsert customer from another process: %v", err)
	}
	otherPricing := pricing.NewService(application.Pool, application.Cache, application.Jobs, catalogfiles.Catalog{}, application.Clock)
	_, err := otherPricing.CreateOverride(t.Context(), httpapi.EnvironmentTest, pricing.CreateOverrideInput{
		Provider:     unpricedProvider,
		Model:        unpricedModel,
		Meter:        string(pricing.MeterOutputTokens),
		UnitPrice:    overrideUnitPrice,
		UnitQuantity: overrideUnitQuantity,
	})
	if err != nil {
		t.Fatalf("create override from another process: %v", err)
	}
}

func customerName(t *testing.T, application *App) string {
	t.Helper()
	customer, err := application.Customers.Cache().Ensure(t.Context(), httpapi.EnvironmentTest, testExternalID)
	if err != nil {
		t.Fatalf("ensure customer: %v", err)
	}
	if customer.DisplayName == nil {
		return ""
	}
	return *customer.DisplayName
}

func assertCustomerName(t *testing.T, application *App, want string) {
	t.Helper()
	if name := customerName(t, application); name != want {
		t.Errorf("customer display name = %q, want %q", name, want)
	}
}

func quote(t *testing.T, application *App) pricing.RatedRequest {
	t.Helper()
	rated, err := application.Pricing.Quote(t.Context(), httpapi.EnvironmentTest, pricing.QuoteRequest{
		Provider: unpricedProvider,
		Model:    unpricedModel,
		Usage:    map[string]string{string(pricing.MeterOutputTokens): quotedOutputTokens},
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	return rated
}

func assertQuoteStatus(t *testing.T, application *App, want pricing.CostStatus) {
	t.Helper()
	if status := quote(t, application).CostStatus; status != want {
		t.Errorf("quote cost status = %s, want %s", status, want)
	}
}

func assertKeyAuthenticates(t *testing.T, application *App, secret string, want error) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, runtimeProbePath, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	bearer(secret)(request)
	_, err = application.APIKeys.Authenticator().Authenticate(t.Context(), request, httpapi.RouteGroupRuntime)
	if !errors.Is(err, want) {
		t.Errorf("authenticate key: err = %v, want %v", err, want)
	}
}

func waitForInvalidationSubscriber(t *testing.T, application *App) {
	t.Helper()
	channel := application.Cache.Key(invalidationChannel)
	waitFor(t, "invalidation subscriber", func() bool {
		subscribers, err := application.Cache.Redis().PubSubNumSub(t.Context(), channel).Result()
		if err != nil {
			t.Fatalf("count subscribers of %s: %v", channel, err)
		}
		return subscribers[channel] == 1
	})
}

func bearer(secret string) authorization {
	return func(request *http.Request) {
		request.Header.Set(authorizationHeader, "Bearer "+secret)
	}
}

func send(t *testing.T, method string, target string, body string, authorize authorization) (status int, responseBody string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if err != nil {
		t.Fatalf("create request %s %s: %v", method, target, err)
	}
	if body != "" {
		request.Header.Set(contentTypeHeader, jsonContentType)
	}
	authorize(request)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body of %s %s: %v", method, target, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close body of %s %s: %v", method, target, err)
	}
	return response.StatusCode, string(contents)
}
