package installation_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/installation"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/plans"
)

const (
	settingsPath                = "/api/v1/settings"
	settingsAuthorizationHeader = "Authorization"
	settingsInvalidationChannel = "invalidate"
	settingsWaitTimeout         = 10 * time.Second
)

type settingsHarness struct {
	pool    *pgxpool.Pool
	cache   *cache.Client
	service *installation.SettingsService
	plans   *plans.Service
	apiKeys *apikeys.Service
	handler http.Handler
}

type settingsAuthenticator struct {
	apiKeys *apikeys.Authenticator
}

func newSettingsHarness(t *testing.T) *settingsHarness {
	t.Helper()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(testStart)
	logger := logging.New(t.Output(), slog.LevelDebug)
	service := installation.NewSettingsService(pool, cacheClient, manualClock)
	apiKeyService := apikeys.NewService(pool, cacheClient, manualClock, logger)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, settingsAuthenticator{apiKeys: apiKeyService.Authenticator()})
	installation.RegisterSettingsRoutes(api, service)
	return &settingsHarness{
		pool:    pool,
		cache:   cacheClient,
		service: service,
		plans:   plans.NewService(pool, cacheClient, manualClock),
		apiKeys: apiKeyService,
		handler: mux,
	}
}

func (authenticator settingsAuthenticator) Authenticate(ctx context.Context, request *http.Request, group httpapi.RouteGroup) (httpapi.Principal, error) {
	if len(request.Header.Values(settingsAuthorizationHeader)) > 0 {
		return authenticator.apiKeys.Authenticate(ctx, request, group)
	}
	environment, err := httpapi.EnvironmentFromHeader(request.Header)
	if err != nil {
		return nil, err
	}
	return httpapi.MemberPrincipal{MemberID: identifiers.New(), Environment: environment}, nil
}

func (harness *settingsHarness) serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	harness.handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *settingsHarness) memberRequest(t *testing.T, method string, environment httpapi.Environment, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, method, settingsPath, body)
	request.Header.Set(httpapi.EnvironmentHeader, string(environment))
	return harness.serve(request)
}

func (harness *settingsHarness) settingsBody(t *testing.T, environment httpapi.Environment) map[string]any {
	t.Helper()
	recorder := harness.memberRequest(t, http.MethodGet, environment, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s in %s = %d, want 200, body %s", settingsPath, environment, recorder.Code, recorder.Body.String())
	}
	return decodeBody(t, recorder)
}

func (harness *settingsHarness) createPlan(t *testing.T, environment httpapi.Environment, name string) plans.Plan {
	t.Helper()
	targetMargin := "0.40"
	plan, err := harness.plans.Create(t.Context(), environment, plans.CreateInput{Name: name, Mode: plans.ModeMarginTarget, TargetMargin: &targetMargin})
	if err != nil {
		t.Fatalf("create plan %s in %s: %v", name, environment, err)
	}
	return plan
}

func encodedPlanID(plan plans.Plan) string {
	return identifiers.Encode(identifiers.PrefixPlan, plan.ID)
}

func subscribeSettingsInvalidations(t *testing.T, cacheClient *cache.Client) *redis.PubSub {
	t.Helper()
	subscription := cacheClient.Redis().Subscribe(t.Context(), cacheClient.Key(settingsInvalidationChannel))
	t.Cleanup(func() {
		if err := subscription.Close(); err != nil {
			t.Errorf("close subscription: %v", err)
		}
	})
	if _, err := subscription.ReceiveTimeout(t.Context(), settingsWaitTimeout); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	return subscription
}

func assertSettingsInvalidation(t *testing.T, subscription *redis.PubSub, want cache.Invalidation) {
	t.Helper()
	received, err := subscription.ReceiveTimeout(t.Context(), settingsWaitTimeout)
	if err != nil {
		t.Fatalf("receive invalidation: %v", err)
	}
	message, isMessage := received.(*redis.Message)
	if !isMessage {
		t.Fatalf("received %T, want *redis.Message", received)
	}
	var invalidation cache.Invalidation
	if err := json.Unmarshal([]byte(message.Payload), &invalidation); err != nil {
		t.Fatalf("decode invalidation %q: %v", message.Payload, err)
	}
	if diff := cmp.Diff(want, invalidation); diff != "" {
		t.Errorf("invalidation mismatch (-want +got):\n%s", diff)
	}
}

func assertSettingsLocations(t *testing.T, recorder *httptest.ResponseRecorder, want ...string) {
	t.Helper()
	assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed")
	var problem httpapi.Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem %q: %v", recorder.Body.String(), err)
	}
	locations := make([]string, 0, len(problem.Errors))
	for _, fieldError := range problem.Errors {
		locations = append(locations, fieldError.Location)
	}
	if diff := cmp.Diff(want, locations); diff != "" {
		t.Errorf("locations mismatch (-want +got):\n%s", diff)
	}
}
