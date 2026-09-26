package policies_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
)

const (
	paceAboveOne          = `{"all": [{"signal": "pace", "operator": "gt", "value": "1"}]}`
	decisionReasonDefault = "no_policy_matched"
	decisionReasonLimit   = "hard_limit_reached"
)

type period struct {
	start time.Time
	end   time.Time
}

var (
	september = period{start: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC), end: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)}
	august    = period{start: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC), end: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)}
)

func TestPreviewCountsCustomersAboveAPaceThreshold(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	fixtures := harness.insertPreviewFixtures(t)
	tests := []struct {
		name    string
		changes map[string]string
		want    policies.PreviewResult
	}{
		{
			name:    "everyone above pace 1",
			changes: map[string]string{"when": paceAboveOne},
			want:    policies.PreviewResult{EvaluatedCustomerCount: 5, MatchedCustomerCount: 3},
		},
		{
			name:    "one plan above pace 1",
			changes: map[string]string{"when": paceAboveOne, "level": `"plan"`, "plan_id": quoted(identifiers.Encode(identifiers.PrefixPlan, fixtures.smallPlanID))},
			want:    policies.PreviewResult{EvaluatedCustomerCount: 4, MatchedCustomerCount: 2},
		},
		{
			name:    "one customer whose cost is in an earlier period",
			changes: map[string]string{"when": paceAboveOne, "level": `"customer"`, "customer_id": quoted(identifiers.Encode(identifiers.PrefixCustomer, fixtures.lightCustomerID))},
			want:    policies.PreviewResult{EvaluatedCustomerCount: 1, MatchedCustomerCount: 0},
		},
		{
			name:    "a disabled draft for one feature",
			changes: map[string]string{"when": paceAboveOne, "status": `"disabled"`, "feature": `"text_to_video"`},
			want:    policies.PreviewResult{EvaluatedCustomerCount: 5, MatchedCustomerCount: 3},
		},
		{
			name:    "decision counts",
			changes: map[string]string{"when": `{"all": [{"signal": "period_decision_count", "operator": "gte", "value": "4"}]}`},
			want:    policies.PreviewResult{EvaluatedCustomerCount: 5, MatchedCustomerCount: 1},
		},
		{
			name:    "a request condition that every match needs",
			changes: map[string]string{"when": `{"all": [{"signal": "pace", "operator": "gt", "value": "1"}, {"signal": "request_estimated_cost", "operator": "gt", "value": "0.50"}]}`},
			want:    policies.PreviewResult{EvaluatedCustomerCount: 5, MatchedCustomerCount: 3, RequestDependent: true},
		},
		{
			name:    "a nested request condition that can match alone",
			changes: map[string]string{"when": `{"any": [{"signal": "pace", "operator": "gt", "value": "5"}, {"all": [{"signal": "request_estimated_cost", "operator": "gt", "value": "0.50"}]}]}`},
			want:    policies.PreviewResult{EvaluatedCustomerCount: 5, MatchedCustomerCount: 5, RequestDependent: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, http.MethodPost, previewPath, httpapi.EnvironmentTest, documentJSON(test.changes))

			assertStatus(t, recorder, http.StatusOK)
			want := map[string]any{
				"evaluated_customer_count": float64(test.want.EvaluatedCustomerCount),
				"matched_customer_count":   float64(test.want.MatchedCustomerCount),
				"request_dependent":        test.want.RequestDependent,
			}
			if diff := cmp.Diff(want, decodeBody(t, recorder)); diff != "" {
				t.Errorf("preview mismatch (-want +got):\n%s", diff)
			}
		})
	}
	listed := decodeBody(t, harness.memberRequest(t, http.MethodGet, policiesPath, httpapi.EnvironmentTest, ""))
	if items := listed["items"]; !cmp.Equal(items, []any{}) {
		t.Errorf("policies after previews = %v, want none", items)
	}
}

func TestPreviewRejectsInvalidDrafts(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	recorder := harness.memberRequest(t, http.MethodPost, previewPath, httpapi.EnvironmentTest, documentJSON(map[string]string{"when": `{"any": []}`}))

	problem := assertProblem(t, recorder, http.StatusUnprocessableEntity, "policy_invalid")
	if diff := cmp.Diff([]string{"body.when.any"}, problemLocations(t, problem)); diff != "" {
		t.Errorf("locations mismatch (-want +got):\n%s", diff)
	}
}

func TestPreviewRefusesEnvironmentsAboveTheCustomerMaximum(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	harness.insertBulkCustomers(t, httpapi.EnvironmentTest, policies.PreviewCustomerMaximum, "active")
	harness.insertBulkCustomers(t, httpapi.EnvironmentTest, 1, "disabled")

	atMaximum := harness.memberRequest(t, http.MethodPost, previewPath, httpapi.EnvironmentTest, documentJSON(nil))

	assertStatus(t, atMaximum, http.StatusOK)
	if evaluated := decodeBody(t, atMaximum)["evaluated_customer_count"]; evaluated != float64(policies.PreviewCustomerMaximum) {
		t.Errorf("evaluated_customer_count = %v, want %d", evaluated, policies.PreviewCustomerMaximum)
	}
	harness.insertBulkCustomers(t, httpapi.EnvironmentTest, 1, "active")
	aboveMaximum := harness.memberRequest(t, http.MethodPost, previewPath, httpapi.EnvironmentTest, documentJSON(nil))
	assertProblem(t, aboveMaximum, http.StatusUnprocessableEntity, "preview_too_large")
}

type previewFixtures struct {
	smallPlanID     uuid.UUID
	lightCustomerID uuid.UUID
}

func (harness *harness) insertPreviewFixtures(t *testing.T) previewFixtures {
	t.Helper()
	smallPlanID := harness.insertPlan(t, httpapi.EnvironmentTest, dollars(10))
	largePlanID := harness.insertPlan(t, httpapi.EnvironmentTest, dollars(100))
	livePlanID := harness.insertPlan(t, httpapi.EnvironmentLive, dollars(10))
	heavyCustomerID := harness.insertCustomer(t, httpapi.EnvironmentTest, &smallPlanID, "active")
	steadyCustomerID := harness.insertCustomer(t, httpapi.EnvironmentTest, &smallPlanID, "active")
	lightCustomerID := harness.insertCustomer(t, httpapi.EnvironmentTest, &smallPlanID, "active")
	harness.insertCustomer(t, httpapi.EnvironmentTest, &smallPlanID, "active")
	disabledCustomerID := harness.insertCustomer(t, httpapi.EnvironmentTest, &smallPlanID, "disabled")
	largeCustomerID := harness.insertCustomer(t, httpapi.EnvironmentTest, &largePlanID, "active")
	liveCustomerID := harness.insertCustomer(t, httpapi.EnvironmentLive, &livePlanID, "active")

	harness.insertLedgerEntry(t, httpapi.EnvironmentTest, heavyCustomerID, dollars(20), september, true)
	harness.insertLedgerEntry(t, httpapi.EnvironmentTest, steadyCustomerID, dollars(6), september, true)
	harness.insertLedgerEntry(t, httpapi.EnvironmentTest, steadyCustomerID, dollars(4), september, false)
	harness.insertLedgerEntry(t, httpapi.EnvironmentTest, lightCustomerID, dollars(5), september, true)
	harness.insertLedgerEntry(t, httpapi.EnvironmentTest, lightCustomerID, dollars(100), august, true)
	harness.insertLedgerEntry(t, httpapi.EnvironmentTest, disabledCustomerID, dollars(50), september, true)
	harness.insertLedgerEntry(t, httpapi.EnvironmentTest, largeCustomerID, dollars(150), september, true)
	harness.insertLedgerEntry(t, httpapi.EnvironmentLive, liveCustomerID, dollars(50), september, true)
	for range 3 {
		harness.insertDecision(t, steadyCustomerID, decisionReasonDefault, september)
		harness.insertDecision(t, heavyCustomerID, decisionReasonLimit, september)
	}
	harness.insertDecision(t, steadyCustomerID, decisionReasonLimit, september)
	harness.insertDecision(t, heavyCustomerID, decisionReasonLimit, september)
	harness.insertDecision(t, lightCustomerID, decisionReasonDefault, august)
	harness.insertDecision(t, lightCustomerID, decisionReasonDefault, august)
	harness.insertDecision(t, lightCustomerID, decisionReasonDefault, august)
	harness.insertDecision(t, lightCustomerID, decisionReasonDefault, august)
	return previewFixtures{smallPlanID: smallPlanID, lightCustomerID: lightCustomerID}
}

func (harness *harness) insertLedgerEntry(t *testing.T, environment httpapi.Environment, customerID uuid.UUID, cost money.Amount, customerPeriod period, withDecision bool) {
	t.Helper()
	var decisionID *uuid.UUID
	if withDecision {
		decisionID = new(identifiers.New())
	}
	ledgerEntryID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO ledger_entries (ledger_entry_id, environment, customer_id, decision_id, idempotency_key, feature, provider, model, attributes,
			usage, cost_nanos, cost_breakdown, cost_status, decision_source, period_start, period_end, occurred_at)
		VALUES ($1, $2, $3, $4, $5, 'text_to_video', 'fal_ai', 'fal-ai/veo3.1/fast', '{}', '{}', $6, '{}', 'costed', 'server', $7, $8, $7)`,
		ledgerEntryID, string(environment), customerID, decisionID, ledgerEntryID.String(), cost, customerPeriod.start, customerPeriod.end)
	if err != nil {
		t.Fatalf("insert ledger entry: %v", err)
	}
}

func (harness *harness) insertDecision(t *testing.T, customerID uuid.UUID, reason string, customerPeriod period) {
	t.Helper()
	outcome := "allow"
	if reason == decisionReasonLimit {
		outcome = "deny"
	}
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO decisions (decision_id, environment, customer_id, feature, requested_provider, requested_model, provider, model, attributes,
			overrides, outcome, reason, signals, reserved_nanos, estimate_basis, status, period_start, period_end, expires_at)
		VALUES ($1, 'test', $2, 'text_to_video', 'fal_ai', 'fal-ai/veo3.1/fast', 'fal_ai', 'fal-ai/veo3.1/fast', '{}',
			'{}', $3, $4, '{}', 0, 'none', 'settled', $5, $6, $6)`,
		identifiers.New(), customerID, outcome, reason, customerPeriod.start, customerPeriod.end)
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
}

func (harness *harness) insertBulkCustomers(t *testing.T, environment httpapi.Environment, count int, status string) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO customers (customer_id, environment, external_id, status)
		SELECT gen_random_uuid(), $1, 'bulk-' || gen_random_uuid(), $3::record_status
		FROM generate_series(1, $2::integer)`,
		string(environment), count, status)
	if err != nil {
		t.Fatalf("insert %d customers: %v", count, err)
	}
}
