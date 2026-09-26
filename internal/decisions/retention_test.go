package decisions_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
)

const (
	retentionDays         = 30
	expiredDecisionsCount = 1001
)

func TestRetentionJobDeclaresKindQueueAndTimeout(t *testing.T) {
	t.Parallel()
	arguments := decisions.RetentionArgs{}

	if kind := arguments.Kind(); kind != "decision_retention" {
		t.Errorf("kind = %s, want decision_retention", kind)
	}
	if queue := arguments.InsertOpts().Queue; queue != jobs.QueueMaintenance {
		t.Errorf("queue = %s, want %s", queue, jobs.QueueMaintenance)
	}
	worker := decisions.NewRetentionWorker(nil, retentionDays, nil, nil)
	if timeout := worker.Timeout(&river.Job[decisions.RetentionArgs]{}); timeout != time.Hour {
		t.Errorf("timeout = %s, want 1h", timeout)
	}
}

func TestRetentionDeletesOnlyDecisionsOlderThanTheWindowAndKeepsLedgerEntries(t *testing.T) {
	t.Parallel()
	harness := newCounterJobsHarness(t)
	testPeriod := harness.currentCustomer(t, httpapi.EnvironmentTest)
	livePeriod := harness.currentCustomer(t, httpapi.EnvironmentLive)
	now := harness.clock.Now()
	cutoff := now.AddDate(0, 0, -retentionDays)

	harness.clock.Set(cutoff.Add(-time.Second))
	expired := harness.checkedDecisions(t, testPeriod, 2)
	expiredLive := harness.checkedDecisions(t, livePeriod, 1)
	settledExpired := harness.checkedDecisions(t, testPeriod, 1)[0]
	settledEntryID := harness.insertLedgerEntry(t, testPeriod, chatFeature, &settledExpired.id, nil, "decision:"+settledExpired.id.String(), new(money.Amount(700)))
	harness.insertBulkDecisions(t, testPeriod, expiredDecisionsCount, cutoff.Add(-time.Hour))
	harness.clock.Set(cutoff)
	atCutoff := harness.checkedDecisions(t, testPeriod, 1)
	harness.clock.Set(now)
	recent := harness.checkedDecisions(t, testPeriod, 1)
	recentLive := harness.checkedDecisions(t, livePeriod, 1)
	var logOutput bytes.Buffer
	worker := decisions.NewRetentionWorker(harness.pool, retentionDays, harness.clock, logging.New(&logOutput, slog.LevelDebug))

	err := worker.Work(t.Context(), &river.Job[decisions.RetentionArgs]{JobRow: &rivertype.JobRow{ID: 1}})

	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	want := decisionIDs(atCutoff, recent, recentLive)
	sortIDs := cmpopts.SortSlices(func(left, right uuid.UUID) bool { return left.String() < right.String() })
	if diff := cmp.Diff(want, harness.remainingDecisionIDs(t), sortIDs); diff != "" {
		t.Errorf("remaining decisions mismatch (-want +got):\n%s", diff)
	}
	if count := harness.ledgerEntryCount(t, settledEntryID); count != 1 {
		t.Errorf("ledger entries of the deleted decision = %d, want 1", count)
	}
	deletedCount := len(expired) + len(expiredLive) + 1 + expiredDecisionsCount
	var logLine map[string]any
	if err := json.Unmarshal(logOutput.Bytes(), &logLine); err != nil {
		t.Fatalf("decode log line %q: %v", logOutput.String(), err)
	}
	wantLog := map[string]any{"msg": "decisions.retention_completed", "deleted_decisions": float64(deletedCount)}
	if diff := cmp.Diff(wantLog, logLine, cmpopts.IgnoreMapEntries(func(key string, _ any) bool { return key == "time" || key == "level" })); diff != "" {
		t.Errorf("log line mismatch (-want +got):\n%s", diff)
	}
}

func (harness *counterJobsHarness) checkedDecisions(t *testing.T, period customerPeriod, count int) []checkedDecision {
	t.Helper()
	checked := make([]checkedDecision, 0, count)
	for range count {
		decision := checkedDecision{period: period, id: uuid.New(), feature: chatFeature, expiresAt: harness.clock.Now().Add(decisionHoldTime)}
		harness.insertDecision(t, decision, "allow", "no_policy_matched", "released")
		checked = append(checked, decision)
	}
	return checked
}

func (harness *counterJobsHarness) insertBulkDecisions(t *testing.T, period customerPeriod, count int, createdAt time.Time) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO decisions (decision_id, environment, customer_id, feature, requested_provider, requested_model,
			provider, model, attributes, overrides, outcome, reason, signals, reserved_nanos, estimate_basis, status,
			period_start, period_end, expires_at, created_at)
		SELECT gen_random_uuid(), $1, $2, $3, $4, $5, $4, $5, '{}', '{}', 'allow', 'no_policy_matched', '{}', 0, 'none', 'released',
			$6, $7, $8, $8
		FROM generate_series(1, $9::integer)`,
		string(period.environment), period.customerID, chatFeature, fixtureProvider, fixtureModel, period.start, period.end, createdAt, count,
	)
	if err != nil {
		t.Fatalf("insert bulk decisions: %v", err)
	}
}

func (harness *counterJobsHarness) remainingDecisionIDs(t *testing.T) []uuid.UUID {
	t.Helper()
	rows, err := harness.pool.Query(t.Context(), "SELECT decision_id FROM decisions")
	if err != nil {
		t.Fatalf("select decisions: %v", err)
	}
	defer rows.Close()
	var remaining []uuid.UUID
	for rows.Next() {
		var decisionID uuid.UUID
		if err := rows.Scan(&decisionID); err != nil {
			t.Fatalf("scan decision id: %v", err)
		}
		remaining = append(remaining, decisionID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read decisions: %v", err)
	}
	return remaining
}

func (harness *counterJobsHarness) ledgerEntryCount(t *testing.T, ledgerEntryID uuid.UUID) int {
	t.Helper()
	var count int
	if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM ledger_entries WHERE ledger_entry_id = $1", ledgerEntryID).Scan(&count); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	return count
}

func decisionIDs(groups ...[]checkedDecision) []uuid.UUID {
	var collected []uuid.UUID
	for _, group := range groups {
		for _, decision := range group {
			collected = append(collected, decision.id)
		}
	}
	return collected
}
