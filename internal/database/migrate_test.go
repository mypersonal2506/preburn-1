package database_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/preburn/preburn/db"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/logging"
)

const (
	checkViolation  = "23514"
	uniqueViolation = "23505"
)

type migrationsAppliedRecord struct {
	Message       string  `json:"msg"`
	Versions      []int64 `json:"versions"`
	RiverVersions []int   `json:"river_versions"`
}

type installationRow struct {
	InstallationID   int16
	Name             string
	SetupTokenHash   []byte
	SetupCompletedAt *time.Time
}

type environmentSettingsRow struct {
	Environment               string
	DefaultPlanID             *string
	StripeCustomerMetadataKey string
}

type meterRow struct {
	Meter          string
	Unit           string
	HasDescription bool
}

type schemaObject struct {
	object string
	query  string
}

var foundationObjectQueries = []schemaObject{
	{object: "extension citext", query: "SELECT EXISTS (SELECT FROM pg_extension WHERE extname = 'citext')"},
	{object: "type environment", query: "SELECT to_regtype('environment') IS NOT NULL"},
	{object: "type record_status", query: "SELECT to_regtype('record_status') IS NOT NULL"},
	{object: "table installation", query: "SELECT to_regclass('installation') IS NOT NULL"},
	{object: "table environment_settings", query: "SELECT to_regclass('environment_settings') IS NOT NULL"},
}

var identityObjectQueries = []schemaObject{
	{object: "table members", query: "SELECT to_regclass('members') IS NOT NULL"},
	{object: "table member_sessions", query: "SELECT to_regclass('member_sessions') IS NOT NULL"},
	{object: "index member_sessions_member_id_index", query: "SELECT to_regclass('member_sessions_member_id_index') IS NOT NULL"},
	{object: "table member_links", query: "SELECT to_regclass('member_links') IS NOT NULL"},
	{object: "index member_links_member_id_purpose_index", query: "SELECT to_regclass('member_links_member_id_purpose_index') IS NOT NULL"},
	{object: "table api_keys", query: "SELECT to_regclass('api_keys') IS NOT NULL"},
	{object: "index api_keys_environment_status_index", query: "SELECT to_regclass('api_keys_environment_status_index') IS NOT NULL"},
}

var catalogObjectQueries = []schemaObject{
	{object: "table meters", query: "SELECT to_regclass('meters') IS NOT NULL"},
	{object: "table pricing_rules", query: "SELECT to_regclass('pricing_rules') IS NOT NULL"},
	{object: "index pricing_rules_provider_model_meter_index", query: "SELECT to_regclass('pricing_rules_provider_model_meter_index') IS NOT NULL"},
	{object: "table provider_model_aliases", query: "SELECT to_regclass('provider_model_aliases') IS NOT NULL"},
	{object: "table pricing_overrides", query: "SELECT to_regclass('pricing_overrides') IS NOT NULL"},
	{object: "table plans", query: "SELECT to_regclass('plans') IS NOT NULL"},
	{object: "table customers", query: "SELECT to_regclass('customers') IS NOT NULL"},
	{object: "index customers_environment_created_at_index", query: "SELECT to_regclass('customers_environment_created_at_index') IS NOT NULL"},
	{object: "index customers_environment_lower_external_id_index", query: "SELECT to_regclass('customers_environment_lower_external_id_index') IS NOT NULL"},
	{object: "table customer_users", query: "SELECT to_regclass('customer_users') IS NOT NULL"},
	{object: "constraint environment_settings_default_plan_id_fkey", query: "SELECT EXISTS (SELECT FROM pg_constraint WHERE conname = 'environment_settings_default_plan_id_fkey')"},
}

var decisionsObjectQueries = []schemaObject{
	{object: "table policies", query: "SELECT to_regclass('policies') IS NOT NULL"},
	{object: "table decisions", query: "SELECT to_regclass('decisions') IS NOT NULL"},
	{object: "index decisions_environment_created_at_index", query: "SELECT to_regclass('decisions_environment_created_at_index') IS NOT NULL"},
	{object: "index decisions_environment_customer_id_created_at_index", query: "SELECT to_regclass('decisions_environment_customer_id_created_at_index') IS NOT NULL"},
	{object: "index decisions_environment_matched_policy_id_created_at_index", query: "SELECT to_regclass('decisions_environment_matched_policy_id_created_at_index') IS NOT NULL"},
	{object: "index decisions_reserved_expires_at_index", query: "SELECT to_regclass('decisions_reserved_expires_at_index') IS NOT NULL"},
	{object: "table ledger_entries", query: "SELECT to_regclass('ledger_entries') IS NOT NULL"},
	{object: "index ledger_entries_environment_customer_id_period_start_index", query: "SELECT to_regclass('ledger_entries_environment_customer_id_period_start_index') IS NOT NULL"},
	{object: "index ledger_entries_environment_occurred_at_index", query: "SELECT to_regclass('ledger_entries_environment_occurred_at_index') IS NOT NULL"},
	{object: "index ledger_entries_uncosted_environment_provider_model_index", query: "SELECT to_regclass('ledger_entries_uncosted_environment_provider_model_index') IS NOT NULL"},
	{object: "table revenue_entries", query: "SELECT to_regclass('revenue_entries') IS NOT NULL"},
	{object: "index revenue_entries_environment_customer_id_period_start_index", query: "SELECT to_regclass('revenue_entries_environment_customer_id_period_start_index') IS NOT NULL"},
	{object: "table period_rollups", query: "SELECT to_regclass('period_rollups') IS NOT NULL"},
	{object: "table usage_estimates", query: "SELECT to_regclass('usage_estimates') IS NOT NULL"},
}

var seededMeters = []meterRow{
	{Meter: "audio_minutes", Unit: "minute", HasDescription: true},
	{Meter: "cache_write_input_tokens", Unit: "token", HasDescription: true},
	{Meter: "cached_input_tokens", Unit: "token", HasDescription: true},
	{Meter: "characters", Unit: "character", HasDescription: true},
	{Meter: "gpu_seconds", Unit: "second", HasDescription: true},
	{Meter: "images", Unit: "image", HasDescription: true},
	{Meter: "input_audio_tokens", Unit: "token", HasDescription: true},
	{Meter: "input_seconds", Unit: "second", HasDescription: true},
	{Meter: "input_tokens", Unit: "token", HasDescription: true},
	{Meter: "megapixels", Unit: "megapixel", HasDescription: true},
	{Meter: "output_audio_tokens", Unit: "token", HasDescription: true},
	{Meter: "output_seconds", Unit: "second", HasDescription: true},
	{Meter: "output_tokens", Unit: "token", HasDescription: true},
	{Meter: "reasoning_tokens", Unit: "token", HasDescription: true},
	{Meter: "requests", Unit: "request", HasDescription: true},
	{Meter: "search_requests", Unit: "request", HasDescription: true},
}

func TestMigrateAppliesPendingMigrationsOnce(t *testing.T) {
	ctx := t.Context()
	pool := databasetest.NewEmptyPool(t)
	var firstOutput, secondOutput bytes.Buffer

	if err := database.Migrate(ctx, pool, logging.New(&firstOutput, slog.LevelInfo)); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := database.Migrate(ctx, pool, logging.New(&secondOutput, slog.LevelInfo)); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	wantFirst := []migrationsAppliedRecord{{Message: string(logging.DatabaseMigrationsApplied), Versions: []int64{1, 2, 3, 4}, RiverVersions: riverVersions(t)}}
	if diff := cmp.Diff(wantFirst, migrationsApplied(t, &firstOutput)); diff != "" {
		t.Errorf("first run event mismatch (-want +got):\n%s", diff)
	}
	wantSecond := []migrationsAppliedRecord{{Message: string(logging.DatabaseMigrationsApplied), Versions: []int64{}, RiverVersions: []int{}}}
	if diff := cmp.Diff(wantSecond, migrationsApplied(t, &secondOutput)); diff != "" {
		t.Errorf("second run event mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(allObjects(foundationObjectQueries), presentObjects(t, pool, foundationObjectQueries)); diff != "" {
		t.Errorf("foundation objects mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(allObjects(identityObjectQueries), presentObjects(t, pool, identityObjectQueries)); diff != "" {
		t.Errorf("identity objects mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(allObjects(catalogObjectQueries), presentObjects(t, pool, catalogObjectQueries)); diff != "" {
		t.Errorf("catalog objects mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(allObjects(decisionsObjectQueries), presentObjects(t, pool, decisionsObjectQueries)); diff != "" {
		t.Errorf("decisions objects mismatch (-want +got):\n%s", diff)
	}
}

func TestMigrateSerializesConcurrentRuns(t *testing.T) {
	ctx := t.Context()
	pool := databasetest.NewEmptyPool(t)
	var output bytes.Buffer
	logger := logging.New(&output, slog.LevelInfo)
	results := make(chan error, 2)

	for range 2 {
		go func() {
			results <- database.Migrate(ctx, pool, logger)
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent Migrate: %v", err)
		}
	}

	want := []migrationsAppliedRecord{
		{Message: string(logging.DatabaseMigrationsApplied), Versions: []int64{1, 2, 3, 4}, RiverVersions: riverVersions(t)},
		{Message: string(logging.DatabaseMigrationsApplied), Versions: []int64{}, RiverVersions: []int{}},
	}
	byAppliedCount := cmpopts.SortSlices(func(first, second migrationsAppliedRecord) bool {
		return len(first.Versions) > len(second.Versions)
	})
	if diff := cmp.Diff(want, migrationsApplied(t, &output), byAppliedCount); diff != "" {
		t.Errorf("events mismatch (-want +got):\n%s", diff)
	}
}

func TestMigrateSeedsFoundationRows(t *testing.T) {
	ctx := t.Context()
	pool := databasetest.NewEmptyPool(t)
	if err := database.Migrate(ctx, pool, logging.New(&bytes.Buffer{}, slog.LevelInfo)); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	assertSeedRows(t, pool)

	_, err := pool.Exec(ctx, "INSERT INTO installation (installation_id) VALUES (2)")
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != checkViolation {
		t.Errorf("second installation row error=%v, want SQLSTATE %s", err, checkViolation)
	}
}

func TestFoundationMigrationDownAndUp(t *testing.T) {
	ctx := t.Context()
	pool := databasetest.NewPool(t)
	provider := newGooseProvider(t, pool)

	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	for _, candidates := range [][]schemaObject{foundationObjectQueries, identityObjectQueries, catalogObjectQueries, decisionsObjectQueries} {
		if diff := cmp.Diff([]string(nil), presentObjects(t, pool, candidates)); diff != "" {
			t.Errorf("objects left after down (-want +got):\n%s", diff)
		}
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if diff := cmp.Diff(allObjects(foundationObjectQueries), presentObjects(t, pool, foundationObjectQueries)); diff != "" {
		t.Errorf("foundation objects after up mismatch (-want +got):\n%s", diff)
	}
	assertSeedRows(t, pool)
}

func TestIdentityMigrationDownAndUp(t *testing.T) {
	ctx := t.Context()
	pool := databasetest.NewPool(t)
	provider := newGooseProvider(t, pool)

	if _, err := provider.DownTo(ctx, 1); err != nil {
		t.Fatalf("migrate down to 1: %v", err)
	}
	if diff := cmp.Diff([]string(nil), presentObjects(t, pool, identityObjectQueries)); diff != "" {
		t.Errorf("identity objects left after down (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(allObjects(foundationObjectQueries), presentObjects(t, pool, foundationObjectQueries)); diff != "" {
		t.Errorf("foundation objects after identity down mismatch (-want +got):\n%s", diff)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if diff := cmp.Diff(allObjects(identityObjectQueries), presentObjects(t, pool, identityObjectQueries)); diff != "" {
		t.Errorf("identity objects after up mismatch (-want +got):\n%s", diff)
	}
}

func TestCatalogMigrationDownAndUp(t *testing.T) {
	ctx := t.Context()
	pool := databasetest.NewPool(t)
	provider := newGooseProvider(t, pool)

	if _, err := provider.DownTo(ctx, 2); err != nil {
		t.Fatalf("migrate down to 2: %v", err)
	}
	if diff := cmp.Diff([]string(nil), presentObjects(t, pool, catalogObjectQueries)); diff != "" {
		t.Errorf("catalog objects left after down (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(allObjects(identityObjectQueries), presentObjects(t, pool, identityObjectQueries)); diff != "" {
		t.Errorf("identity objects after catalog down mismatch (-want +got):\n%s", diff)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if diff := cmp.Diff(allObjects(catalogObjectQueries), presentObjects(t, pool, catalogObjectQueries)); diff != "" {
		t.Errorf("catalog objects after up mismatch (-want +got):\n%s", diff)
	}
	assertSeededMeters(t, pool)
}

func TestCatalogMigrationSeedsMeters(t *testing.T) {
	assertSeededMeters(t, databasetest.NewPool(t))
}

func TestPlanTargetMarginBasisPointsRange(t *testing.T) {
	pool := databasetest.NewPool(t)
	const insertPlan = "INSERT INTO plans (plan_id, environment, name, target_margin_basis_points, status) VALUES (gen_random_uuid(), 'test', $1, $2, 'active')"
	cases := []struct {
		name                    string
		targetMarginBasisPoints int
		wantCode                string
	}{
		{name: "zero", targetMarginBasisPoints: 0},
		{name: "largest", targetMarginBasisPoints: 9999},
		{name: "whole revenue", targetMarginBasisPoints: 10000, wantCode: checkViolation},
		{name: "negative", targetMarginBasisPoints: -1, wantCode: checkViolation},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), insertPlan, testCase.name, testCase.targetMarginBasisPoints)
			var postgresError *pgconn.PgError
			gotCode := ""
			if errors.As(err, &postgresError) {
				gotCode = postgresError.Code
			} else if err != nil {
				t.Fatalf("insert plan: %v", err)
			}
			if gotCode != testCase.wantCode {
				t.Errorf("insert plan with %d basis points SQLSTATE %q, want %q", testCase.targetMarginBasisPoints, gotCode, testCase.wantCode)
			}
		})
	}
}

func TestPricingOverrideEffectivePeriod(t *testing.T) {
	pool := databasetest.NewPool(t)
	const insertOverride = `INSERT INTO pricing_overrides (pricing_override_id, environment, provider, model, meter, unit_price_nanos, unit_quantity, effective_from, effective_to)
		VALUES (gen_random_uuid(), 'test', 'acme', 'acme-video-1', 'output_seconds', 500000000, 1, '2026-09-01T00:00:00Z', $1)`
	cases := []struct {
		name           string
		effectiveTo    *string
		wantConstraint string
	}{
		{name: "no end"},
		{name: "end after start", effectiveTo: new("2026-09-02T00:00:00Z")},
		{name: "ended at its start", effectiveTo: new("2026-09-01T00:00:00Z")},
		{name: "end before start", effectiveTo: new("2026-08-31T00:00:00Z"), wantConstraint: "pricing_overrides_effective_period_check"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), insertOverride, testCase.effectiveTo)
			if gotConstraint := violatedConstraint(t, err); gotConstraint != testCase.wantConstraint {
				t.Errorf("insert override with %s violated %q, want %q", testCase.name, gotConstraint, testCase.wantConstraint)
			}
		})
	}
}

func TestDecisionsMigrationDownAndUp(t *testing.T) {
	ctx := t.Context()
	pool := databasetest.NewPool(t)
	provider := newGooseProvider(t, pool)

	if _, err := provider.DownTo(ctx, 3); err != nil {
		t.Fatalf("migrate down to 3: %v", err)
	}
	if diff := cmp.Diff([]string(nil), presentObjects(t, pool, decisionsObjectQueries)); diff != "" {
		t.Errorf("decisions objects left after down (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(allObjects(catalogObjectQueries), presentObjects(t, pool, catalogObjectQueries)); diff != "" {
		t.Errorf("catalog objects after decisions down mismatch (-want +got):\n%s", diff)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if diff := cmp.Diff(allObjects(decisionsObjectQueries), presentObjects(t, pool, decisionsObjectQueries)); diff != "" {
		t.Errorf("decisions objects after up mismatch (-want +got):\n%s", diff)
	}
}

func TestPolicyScopeIDsMatchLevel(t *testing.T) {
	pool := databasetest.NewPool(t)
	planID := insertStarterPlan(t, pool)
	customerID := insertCustomer(t, pool, "test")
	const insertPolicy = `INSERT INTO policies (policy_id, environment, name, level, plan_id, customer_id, condition_group, action, enforcement, on_unreachable, on_uncosted, status, version)
		VALUES (gen_random_uuid(), 'test', $1, $2, $3, $4, '{"all": []}', '{"outcome": "deny"}', 'hard', 'allow', 'deny', 'active', 1)`
	cases := []struct {
		name           string
		level          string
		planID         *uuid.UUID
		customerID     *uuid.UUID
		wantConstraint string
	}{
		{name: "everyone", level: "everyone"},
		{name: "plan", level: "plan", planID: &planID},
		{name: "customer", level: "customer", customerID: &customerID},
		{name: "everyone with a plan", level: "everyone", planID: &planID, wantConstraint: "policies_level_plan_id_check"},
		{name: "plan without a plan", level: "plan", wantConstraint: "policies_level_plan_id_check"},
		{name: "plan with a customer", level: "plan", planID: &planID, customerID: &customerID, wantConstraint: "policies_level_customer_id_check"},
		{name: "customer without a customer", level: "customer", wantConstraint: "policies_level_customer_id_check"},
		{name: "unknown level", level: "team", wantConstraint: "policies_level_check"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), insertPolicy, testCase.name, testCase.level, testCase.planID, testCase.customerID)
			if gotConstraint := violatedConstraint(t, err); gotConstraint != testCase.wantConstraint {
				t.Errorf("insert %s policy violated %q, want %q", testCase.name, gotConstraint, testCase.wantConstraint)
			}
		})
	}
}

func TestDecisionConstraints(t *testing.T) {
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, "test")
	const insertDecision = `INSERT INTO decisions (decision_id, environment, customer_id, feature, requested_provider, requested_model, provider, model, attributes, overrides, outcome, reason, signals, reserved_nanos, estimate_basis, status, period_start, period_end, expires_at)
		VALUES (gen_random_uuid(), 'test', $1, 'text_to_video', 'fal_ai', 'veo3', 'fal_ai', 'veo3', '{}', '{}', $2, $3, '{}', $4, 'ceiling', 'reserved', '2026-09-01T00:00:00Z', $5, '2026-09-26T12:10:00Z')`
	cases := []struct {
		name           string
		outcome        string
		reason         string
		reservedNanos  int64
		periodEnd      string
		wantConstraint string
	}{
		{name: "valid", outcome: "allow", reason: "no_policy_matched", reservedNanos: 1_500_000_000, periodEnd: "2026-10-01T00:00:00Z"},
		{name: "unknown outcome", outcome: "block", reason: "policy_matched", periodEnd: "2026-10-01T00:00:00Z", wantConstraint: "decisions_outcome_check"},
		{name: "unknown reason", outcome: "deny", reason: "blocked", periodEnd: "2026-10-01T00:00:00Z", wantConstraint: "decisions_reason_check"},
		{name: "negative reservation", outcome: "allow", reason: "no_policy_matched", reservedNanos: -1, periodEnd: "2026-10-01T00:00:00Z", wantConstraint: "decisions_reserved_nanos_check"},
		{name: "empty period", outcome: "allow", reason: "no_policy_matched", periodEnd: "2026-09-01T00:00:00Z", wantConstraint: "decisions_period_check"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), insertDecision, customerID, testCase.outcome, testCase.reason, testCase.reservedNanos, testCase.periodEnd)
			if gotConstraint := violatedConstraint(t, err); gotConstraint != testCase.wantConstraint {
				t.Errorf("insert %s decision violated %q, want %q", testCase.name, gotConstraint, testCase.wantConstraint)
			}
		})
	}
}

func TestLedgerEntryIdempotencyKeyIsUniquePerEnvironment(t *testing.T) {
	pool := databasetest.NewPool(t)
	testCustomerID := insertCustomer(t, pool, "test")
	liveCustomerID := insertCustomer(t, pool, "live")
	costNanos := int64(2_000_000_000)
	cases := []struct {
		name           string
		environment    string
		customerID     uuid.UUID
		wantConstraint string
	}{
		{name: "first report", environment: "test", customerID: testCustomerID},
		{name: "same key in the same environment", environment: "test", customerID: testCustomerID, wantConstraint: "ledger_entries_environment_idempotency_key_key"},
		{name: "same key in the other environment", environment: "live", customerID: liveCustomerID},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := insertLedgerEntry(t.Context(), pool, testCase.environment, testCase.customerID, "report-1", &costNanos, "costed")
			if gotConstraint := violatedConstraint(t, err); gotConstraint != testCase.wantConstraint {
				t.Errorf("insert %s violated %q, want %q", testCase.name, gotConstraint, testCase.wantConstraint)
			}
		})
	}
}

func TestLedgerEntryCostMatchesCostStatus(t *testing.T) {
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, "test")
	costNanos := int64(2_000_000_000)
	negativeCostNanos := int64(-1)
	cases := []struct {
		name           string
		costNanos      *int64
		costStatus     string
		wantConstraint string
	}{
		{name: "costed", costNanos: &costNanos, costStatus: "costed"},
		{name: "uncosted", costStatus: "uncosted"},
		{name: "costed without cost", costStatus: "costed", wantConstraint: "ledger_entries_uncosted_cost_check"},
		{name: "uncosted with cost", costNanos: &costNanos, costStatus: "uncosted", wantConstraint: "ledger_entries_uncosted_cost_check"},
		{name: "negative cost", costNanos: &negativeCostNanos, costStatus: "costed", wantConstraint: "ledger_entries_cost_nanos_check"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := insertLedgerEntry(t.Context(), pool, "test", customerID, testCase.name, testCase.costNanos, testCase.costStatus)
			if gotConstraint := violatedConstraint(t, err); gotConstraint != testCase.wantConstraint {
				t.Errorf("insert %s ledger entry violated %q, want %q", testCase.name, gotConstraint, testCase.wantConstraint)
			}
		})
	}
}

func TestRevenueEntryAmountNanosRejectsNegative(t *testing.T) {
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, "test")
	const insertRevenueEntry = `INSERT INTO revenue_entries (revenue_entry_id, environment, customer_id, period_start, period_end, kind, amount_nanos, source, source_reference, occurred_at)
		VALUES (gen_random_uuid(), 'test', $1, '2026-09-01T00:00:00Z', $2, 'subscription', $3, 'api', $4, '2026-09-01T00:00:00Z')`
	cases := []struct {
		name           string
		periodEnd      string
		amountNanos    int64
		wantConstraint string
	}{
		{name: "zero", periodEnd: "2026-10-01T00:00:00Z", amountNanos: 0},
		{name: "positive", periodEnd: "2026-10-01T00:00:00Z", amountNanos: 30_000_000_000},
		{name: "zero length period", periodEnd: "2026-09-01T00:00:00Z", amountNanos: 1_000_000_000},
		{name: "negative", periodEnd: "2026-10-01T00:00:00Z", amountNanos: -1, wantConstraint: "revenue_entries_amount_nanos_check"},
		{name: "period end before start", periodEnd: "2026-08-31T00:00:00Z", amountNanos: 1_000_000_000, wantConstraint: "revenue_entries_period_check"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), insertRevenueEntry, customerID, testCase.periodEnd, testCase.amountNanos, testCase.name)
			if gotConstraint := violatedConstraint(t, err); gotConstraint != testCase.wantConstraint {
				t.Errorf("insert %s revenue entry violated %q, want %q", testCase.name, gotConstraint, testCase.wantConstraint)
			}
		})
	}
}

func TestMemberEmailIsUniqueIgnoringCase(t *testing.T) {
	ctx := t.Context()
	pool := databasetest.NewPool(t)
	const insertMember = "INSERT INTO members (member_id, email, display_name, status) VALUES (gen_random_uuid(), $1, 'Member', 'active')"

	if _, err := pool.Exec(ctx, insertMember, "a@x.io"); err != nil {
		t.Fatalf("insert first member: %v", err)
	}
	_, err := pool.Exec(ctx, insertMember, "A@x.io")
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != uniqueViolation {
		t.Errorf("second member error=%v, want SQLSTATE %s", err, uniqueViolation)
	}
}

func assertSeedRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()
	installationRows, err := pool.Query(ctx, "SELECT installation_id, name, setup_token_hash, setup_completed_at FROM installation")
	if err != nil {
		t.Fatalf("query installation: %v", err)
	}
	installations, err := pgx.CollectRows(installationRows, pgx.RowToStructByPos[installationRow])
	if err != nil {
		t.Fatalf("read installation: %v", err)
	}
	if diff := cmp.Diff([]installationRow{{InstallationID: 1, Name: "Preburn"}}, installations); diff != "" {
		t.Errorf("installation rows mismatch (-want +got):\n%s", diff)
	}

	settingsRows, err := pool.Query(ctx, "SELECT environment::text AS environment_name, default_plan_id::text, stripe_customer_metadata_key FROM environment_settings ORDER BY environment")
	if err != nil {
		t.Fatalf("query environment_settings: %v", err)
	}
	settings, err := pgx.CollectRows(settingsRows, pgx.RowToStructByPos[environmentSettingsRow])
	if err != nil {
		t.Fatalf("read environment_settings: %v", err)
	}
	wantSettings := []environmentSettingsRow{
		{Environment: "test", StripeCustomerMetadataKey: "preburn_customer_id"},
		{Environment: "live", StripeCustomerMetadataKey: "preburn_customer_id"},
	}
	if diff := cmp.Diff(wantSettings, settings); diff != "" {
		t.Errorf("environment_settings rows mismatch (-want +got):\n%s", diff)
	}
}

func assertSeededMeters(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(t.Context(), `SELECT meter, unit, description <> '' FROM meters ORDER BY meter COLLATE "C"`)
	if err != nil {
		t.Fatalf("query meters: %v", err)
	}
	meters, err := pgx.CollectRows(rows, pgx.RowToStructByPos[meterRow])
	if err != nil {
		t.Fatalf("read meters: %v", err)
	}
	if diff := cmp.Diff(seededMeters, meters); diff != "" {
		t.Errorf("seeded meters mismatch (-want +got):\n%s", diff)
	}
}

func insertStarterPlan(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var planID uuid.UUID
	err := pool.QueryRow(t.Context(), "INSERT INTO plans (plan_id, environment, name, target_margin_basis_points, status) VALUES (gen_random_uuid(), 'test', 'Starter', 4000, 'active') RETURNING plan_id").Scan(&planID)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	return planID
}

func insertCustomer(t *testing.T, pool *pgxpool.Pool, environment string) uuid.UUID {
	t.Helper()
	var customerID uuid.UUID
	err := pool.QueryRow(t.Context(), "INSERT INTO customers (customer_id, environment, external_id, status) VALUES (gen_random_uuid(), $1, 'customer', 'active') RETURNING customer_id", environment).Scan(&customerID)
	if err != nil {
		t.Fatalf("insert %s customer: %v", environment, err)
	}
	return customerID
}

func insertLedgerEntry(ctx context.Context, pool *pgxpool.Pool, environment string, customerID uuid.UUID, idempotencyKey string, costNanos *int64, costStatus string) error {
	const statement = `INSERT INTO ledger_entries (ledger_entry_id, environment, customer_id, idempotency_key, feature, provider, model, attributes, usage, cost_nanos, cost_breakdown, cost_status, decision_source, period_start, period_end, occurred_at)
		VALUES (gen_random_uuid(), $1, $2, $3, 'text_to_video', 'fal_ai', 'veo3', '{}', '{"output_seconds": 8000000}', $4, '{}', $5, 'server', '2026-09-01T00:00:00Z', '2026-10-01T00:00:00Z', '2026-09-26T12:00:00Z')`
	_, err := pool.Exec(ctx, statement, environment, customerID, idempotencyKey, costNanos, costStatus)
	return err
}

func violatedConstraint(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.ConstraintName == "" {
		t.Fatalf("statement error is not a constraint violation: %v", err)
	}
	return postgresError.ConstraintName
}

func newGooseProvider(t *testing.T, pool *pgxpool.Pool) *goose.Provider {
	t.Helper()
	files, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		t.Fatalf("open embedded migrations: %v", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, stdlib.OpenDBFromPool(pool), files)
	if err != nil {
		t.Fatalf("create goose provider: %v", err)
	}
	t.Cleanup(func() {
		if err := provider.Close(); err != nil {
			t.Errorf("close goose provider: %v", err)
		}
	})
	return provider
}

func presentObjects(t *testing.T, pool *pgxpool.Pool, candidates []schemaObject) []string {
	t.Helper()
	var present []string
	for _, candidate := range candidates {
		var exists bool
		if err := pool.QueryRow(t.Context(), candidate.query).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", candidate.object, err)
		}
		if exists {
			present = append(present, candidate.object)
		}
	}
	return present
}

func allObjects(candidates []schemaObject) []string {
	objects := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		objects = append(objects, candidate.object)
	}
	return objects
}

func migrationsApplied(t *testing.T, output *bytes.Buffer) []migrationsAppliedRecord {
	t.Helper()
	var records []migrationsAppliedRecord
	decoder := json.NewDecoder(output)
	for decoder.More() {
		var record migrationsAppliedRecord
		if err := decoder.Decode(&record); err != nil {
			t.Fatalf("decode log line: %v", err)
		}
		if record.Message == string(logging.DatabaseMigrationsApplied) {
			records = append(records, record)
		}
	}
	return records
}

func riverVersions(t *testing.T) []int {
	t.Helper()
	migrator, err := rivermigrate.New(riverpgxv5.New(nil), nil)
	if err != nil {
		t.Fatalf("create river migrator: %v", err)
	}
	var versions []int
	for _, migration := range migrator.AllVersions() {
		versions = append(versions, migration.Version)
	}
	return versions
}
