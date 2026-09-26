package dashboard_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/signals"
)

const (
	decisionsPath  = "/api/v1/dashboard/decisions"
	eventsPath     = "/api/v1/dashboard/events"
	onboardingPath = "/api/v1/dashboard/onboarding"
	featuresPath   = "/api/v1/dashboard/features"
	klingPro       = "fal-ai/kling-video/v2.5-turbo/pro"
)

type storedDecision struct {
	environment          httpapi.Environment
	customerID           uuid.UUID
	customerUserID       *uuid.UUID
	feature              string
	requestedModel       string
	model                string
	attributes           string
	overrides            string
	outcome              policies.Outcome
	reason               policies.Reason
	matchedPolicyID      *uuid.UUID
	matchedPolicyVersion *int
	signals              signals.Response
	requestedCost        *money.Amount
	estimatedCost        *money.Amount
	reserved             money.Amount
	status               string
	expiresAt            time.Time
	settledAt            *time.Time
	createdAt            time.Time
}

func newDecisionHarness(t *testing.T) *harness {
	t.Helper()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(testNow)
	logger := logging.New(t.Output(), slog.LevelDebug)
	apiKeyService := apikeys.NewService(pool, cacheClient, manualClock, logger)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, testAuthenticator{apiKeys: apiKeyService.Authenticator()})
	dashboard.RegisterDecisionRoutes(api, dashboard.NewDecisionService(pool))
	dashboard.RegisterEventRoutes(api, dashboard.NewEventService(pool))
	dashboard.RegisterOnboardingRoutes(api, dashboard.NewOnboardingService(pool))
	dashboard.RegisterFeatureRoutes(api, dashboard.NewFeatureService(pool, manualClock))
	return &harness{
		pool:    pool,
		cache:   cacheClient,
		clock:   manualClock,
		apiKeys: apiKeyService,
		handler: mux,
	}
}

func allowedDecision(environment httpapi.Environment, customerID uuid.UUID, createdAt time.Time) storedDecision {
	return storedDecision{
		environment:    environment,
		customerID:     customerID,
		feature:        textToVideo,
		requestedModel: veoModel,
		model:          veoModel,
		attributes:     "{}",
		overrides:      "{}",
		outcome:        policies.OutcomeAllow,
		reason:         policies.ReasonNoPolicyMatched,
		signals:        signals.Response{Features: map[string]signals.FeatureResponse{}},
		requestedCost:  amount(2),
		estimatedCost:  amount(2),
		reserved:       dollars(2),
		status:         "reserved",
		expiresAt:      createdAt.Add(10 * time.Minute),
		createdAt:      createdAt,
	}
}

func (harness *harness) storeDecision(t *testing.T, decision storedDecision) uuid.UUID {
	t.Helper()
	encodedSignals, err := json.Marshal(decision.signals)
	if err != nil {
		t.Fatalf("encode signals: %v", err)
	}
	decisionID := identifiers.New()
	_, err = harness.pool.Exec(t.Context(),
		`INSERT INTO decisions (decision_id, environment, customer_id, customer_user_id, feature, requested_provider, requested_model,
			provider, model, attributes, overrides, outcome, reason, matched_policy_id, matched_policy_version, signals,
			requested_estimated_cost_nanos, estimated_cost_nanos, reserved_nanos, estimate_basis, status, period_start, period_end,
			expires_at, settled_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $6, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, 'ceiling', $19, $20, $21, $22, $23, $24)`,
		decisionID, string(decision.environment), decision.customerID, decision.customerUserID, decision.feature, falProvider,
		decision.requestedModel, decision.model, decision.attributes, decision.overrides, string(decision.outcome), string(decision.reason),
		decision.matchedPolicyID, decision.matchedPolicyVersion, encodedSignals, decision.requestedCost, decision.estimatedCost,
		int64(decision.reserved), decision.status, septemberStart, octoberStart, decision.expiresAt, decision.settledAt, decision.createdAt)
	if err != nil {
		t.Fatalf("insert %s decision: %v", decision.outcome, err)
	}
	return decisionID
}

func (harness *harness) stampLedgerEntry(t *testing.T, ledgerEntryID uuid.UUID, decisionID *uuid.UUID, createdAt time.Time) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(), "UPDATE ledger_entries SET decision_id = $2, created_at = $3 WHERE ledger_entry_id = $1",
		ledgerEntryID, decisionID, createdAt)
	if err != nil {
		t.Fatalf("stamp ledger entry: %v", err)
	}
}

func (harness *harness) insertCustomerUser(t *testing.T, environment httpapi.Environment, customerID uuid.UUID, externalID string) uuid.UUID {
	t.Helper()
	customerUserID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO customer_users (customer_user_id, environment, customer_id, external_id) VALUES ($1, $2, $3, $4)",
		customerUserID, string(environment), customerID, externalID)
	if err != nil {
		t.Fatalf("insert customer user %s: %v", externalID, err)
	}
	return customerUserID
}

func (harness *harness) storePolicy(t *testing.T, environment httpapi.Environment, name string, feature *string, status policies.Status) uuid.UUID {
	t.Helper()
	policyID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO policies (policy_id, environment, name, level, feature, condition_group, action, enforcement, on_unreachable,
			on_uncosted, status, version)
		VALUES ($1, $2, $3, 'everyone', $4, '{"all":[]}', '{"outcome":"deny"}', 'soft', 'allow', 'allow', $5, 1)`,
		policyID, string(environment), name, feature, string(status))
	if err != nil {
		t.Fatalf("insert policy %s: %v", name, err)
	}
	return policyID
}

func (harness *harness) storePlan(t *testing.T, environment httpapi.Environment, name string, holdTimes string, status string) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO plans (plan_id, environment, name, target_margin_basis_points, hold_times, status) VALUES ($1, $2, $3, 4000, $4, $5)",
		identifiers.New(), string(environment), name, holdTimes, status)
	if err != nil {
		t.Fatalf("insert plan %s: %v", name, err)
	}
}

func (harness *harness) insertUsageEstimate(t *testing.T, environment httpapi.Environment, feature string) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO usage_estimates (environment, feature, provider, model, meter, p95_quantity_micros, sample_count, refreshed_at)
		VALUES ($1, $2, $3, $4, 'output_seconds', 8000000, 50, $5)`,
		string(environment), feature, falProvider, veoModel, testNow)
	if err != nil {
		t.Fatalf("insert usage estimate of %s: %v", feature, err)
	}
}

func encodedDecision(decisionID uuid.UUID) string {
	return identifiers.Encode(identifiers.PrefixDecision, decisionID)
}

func encodedLedgerEntry(ledgerEntryID uuid.UUID) string {
	return identifiers.Encode(identifiers.PrefixLedgerEntry, ledgerEntryID)
}
