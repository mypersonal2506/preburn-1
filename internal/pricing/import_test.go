package pricing_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/catalog"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	curatedProvider   = "fal_ai"
	veoModel          = "fal-ai/veo3.1/fast"
	veoAlias          = "fal-ai/veo3.1/fast/image-to-video"
	veoSourceURL      = "https://fal.ai/models/fal-ai/veo3.1/fast"
	fluxModel         = "fal-ai/flux/schnell"
	fluxSourceURL     = "https://fal.ai/models/fal-ai/flux/schnell"
	veoDefaultKey     = "fal_ai/fal-ai/veo3.1/fast/output_seconds/"
	veoSilent4KKey    = "fal_ai/fal-ai/veo3.1/fast/output_seconds/audio=false,resolution=4k"
	fluxMegapixelsKey = "fal_ai/fal-ai/flux/schnell/megapixels/"
)

var (
	importStart     = time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC)
	secondImport    = importStart.Add(time.Hour)
	thirdImport     = importStart.Add(2 * time.Hour)
	ignoreRuleID    = cmpopts.IgnoreFields(storedRule{}, "PricingRuleID")
	ignoreTimestamp = cmpopts.IgnoreMapEntries(func(key string, _ any) bool { return key == "time" || key == "level" })
)

type storedRule struct {
	PricingRuleID          uuid.UUID
	SourceKey              string
	Provider               string
	Model                  string
	Meter                  string
	Conditions             string
	UnitPriceNanos         int64
	UnitQuantity           int64
	MinimumChargeNanos     *int64
	BillingIncrementMicros *int64
	EffectiveFrom          time.Time
	EffectiveTo            *time.Time
	Status                 string
	SourceURL              *string
	ImportedAt             time.Time
}

type storedAlias struct {
	Provider string
	Alias    string
	Model    string
}

func TestImportCuratedWritesRulesAndAliases(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	var logOutput bytes.Buffer

	summary, err := pricing.ImportCurated(t.Context(), pool, curatedFixture(), clock.NewManual(importStart), logging.New(&logOutput, slog.LevelDebug))

	if err != nil {
		t.Fatalf("ImportCurated: %v", err)
	}
	wantSummary := pricing.ImportSummary{Source: pricing.RuleSourceCurated, Created: 3, Aliases: 1}
	if diff := cmp.Diff(wantSummary, summary); diff != "" {
		t.Errorf("summary mismatch (-want +got):\n%s", diff)
	}
	wantRules := []storedRule{
		curatedRule(fluxMegapixelsKey, fluxModel, "megapixels", `{}`, 3_000_000, fluxSourceURL, importStart),
		curatedRule(veoDefaultKey, veoModel, "output_seconds", `{}`, 150_000_000, veoSourceURL, importStart),
		curatedRule(veoSilent4KKey, veoModel, "output_seconds", `{"audio": false, "resolution": "4k"}`, 300_000_000, veoSourceURL, importStart),
	}
	wantRules[0].MinimumChargeNanos = pointer(int64(10_000_000))
	wantRules[0].BillingIncrementMicros = pointer(int64(1_000_000))
	if diff := cmp.Diff(wantRules, selectStoredRules(t, pool, "curated"), ignoreRuleID); diff != "" {
		t.Errorf("rules mismatch (-want +got):\n%s", diff)
	}
	wantAliases := []storedAlias{{Provider: curatedProvider, Alias: veoAlias, Model: veoModel}}
	if diff := cmp.Diff(wantAliases, selectStoredAliases(t, pool)); diff != "" {
		t.Errorf("aliases mismatch (-want +got):\n%s", diff)
	}
	var logLine map[string]any
	if err := json.Unmarshal(logOutput.Bytes(), &logLine); err != nil {
		t.Fatalf("decode log line %q: %v", logOutput.String(), err)
	}
	wantLog := map[string]any{
		"msg":        "pricing.catalog_imported",
		"source":     "curated",
		"created":    float64(3),
		"changed":    float64(0),
		"updated":    float64(0),
		"deprecated": float64(0),
		"unchanged":  float64(0),
		"aliases":    float64(1),
	}
	if diff := cmp.Diff(wantLog, logLine, ignoreTimestamp); diff != "" {
		t.Errorf("log line mismatch (-want +got):\n%s", diff)
	}
}

func TestImportCuratedShippedFilesTwiceChangesNothing(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	files, err := catalogfiles.Load(catalog.Files)
	if err != nil {
		t.Fatalf("load shipped catalog: %v", err)
	}
	timeSource := clock.NewManual(importStart)
	first, err := pricing.ImportCurated(t.Context(), pool, files, timeSource, discardLogger())
	if err != nil {
		t.Fatalf("first ImportCurated: %v", err)
	}
	rulesBefore := selectStoredRules(t, pool, "curated")
	aliasesBefore := selectStoredAliases(t, pool)
	timeSource.Set(secondImport)

	second, err := pricing.ImportCurated(t.Context(), pool, files, timeSource, discardLogger())

	if err != nil {
		t.Fatalf("second ImportCurated: %v", err)
	}
	if first.Created == 0 || first.Created != len(rulesBefore) {
		t.Errorf("first import created %d rules, stored %d", first.Created, len(rulesBefore))
	}
	wantSecond := pricing.ImportSummary{Source: pricing.RuleSourceCurated, Unchanged: first.Created, Aliases: len(files.Aliases)}
	if diff := cmp.Diff(wantSecond, second); diff != "" {
		t.Errorf("second summary mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(rulesBefore, selectStoredRules(t, pool, "curated")); diff != "" {
		t.Errorf("rules changed by the second import (-before +after):\n%s", diff)
	}
	if diff := cmp.Diff(aliasesBefore, selectStoredAliases(t, pool)); diff != "" {
		t.Errorf("aliases changed by the second import (-before +after):\n%s", diff)
	}
}

func TestImportCuratedChangedPriceOpensNewRule(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	timeSource := clock.NewManual(importStart)
	if _, err := pricing.ImportCurated(t.Context(), pool, curatedFixture(), timeSource, discardLogger()); err != nil {
		t.Fatalf("first ImportCurated: %v", err)
	}
	firstRules := selectStoredRules(t, pool, "curated")
	changed := curatedFixture()
	changed.CuratedModels[0].Rules[0].UnitPrice.Nanos = 200_000_000
	timeSource.Set(secondImport)

	summary, err := pricing.ImportCurated(t.Context(), pool, changed, timeSource, discardLogger())

	if err != nil {
		t.Fatalf("second ImportCurated: %v", err)
	}
	wantSummary := pricing.ImportSummary{Source: pricing.RuleSourceCurated, Changed: 1, Unchanged: 2, Aliases: 1}
	if diff := cmp.Diff(wantSummary, summary); diff != "" {
		t.Errorf("summary mismatch (-want +got):\n%s", diff)
	}
	closed := firstRules[1]
	closed.EffectiveTo = pointer(secondImport)
	opened := curatedRule(veoDefaultKey, veoModel, "output_seconds", `{}`, 200_000_000, veoSourceURL, secondImport)
	wantRules := []storedRule{firstRules[0], closed, opened, firstRules[2]}
	gotRules := selectStoredRules(t, pool, "curated")
	if diff := cmp.Diff(wantRules, gotRules, cmpopts.IgnoreFields(storedRule{}, "PricingRuleID", "ImportedAt")); diff != "" {
		t.Errorf("rules mismatch (-want +got):\n%s", diff)
	}
	if gotRules[1].PricingRuleID != firstRules[1].PricingRuleID || gotRules[2].PricingRuleID == firstRules[1].PricingRuleID {
		t.Errorf("rule ids %s and %s, want the closed rule to keep %s and the new rule to get a new id", gotRules[1].PricingRuleID, gotRules[2].PricingRuleID, firstRules[1].PricingRuleID)
	}
}

func TestImportCuratedRemovedModelBecomesDeprecated(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	timeSource := clock.NewManual(importStart)
	if _, err := pricing.ImportCurated(t.Context(), pool, curatedFixture(), timeSource, discardLogger()); err != nil {
		t.Fatalf("first ImportCurated: %v", err)
	}
	firstRules := selectStoredRules(t, pool, "curated")
	withoutFlux := curatedFixture()
	withoutFlux.CuratedModels = withoutFlux.CuratedModels[:1]
	timeSource.Set(secondImport)

	removed, err := pricing.ImportCurated(t.Context(), pool, withoutFlux, timeSource, discardLogger())

	if err != nil {
		t.Fatalf("second ImportCurated: %v", err)
	}
	wantRemoved := pricing.ImportSummary{Source: pricing.RuleSourceCurated, Deprecated: 1, Unchanged: 2, Aliases: 1}
	if diff := cmp.Diff(wantRemoved, removed); diff != "" {
		t.Errorf("summary after removal mismatch (-want +got):\n%s", diff)
	}
	deprecated := firstRules[0]
	deprecated.EffectiveTo = pointer(secondImport)
	deprecated.Status = "deprecated"
	deprecated.ImportedAt = secondImport
	wantRules := []storedRule{deprecated, firstRules[1], firstRules[2]}
	if diff := cmp.Diff(wantRules, selectStoredRules(t, pool, "curated")); diff != "" {
		t.Errorf("rules after removal mismatch (-want +got):\n%s", diff)
	}

	timeSource.Set(thirdImport)
	restored, err := pricing.ImportCurated(t.Context(), pool, curatedFixture(), timeSource, discardLogger())

	if err != nil {
		t.Fatalf("third ImportCurated: %v", err)
	}
	wantRestored := pricing.ImportSummary{Source: pricing.RuleSourceCurated, Created: 1, Unchanged: 2, Aliases: 1}
	if diff := cmp.Diff(wantRestored, restored); diff != "" {
		t.Errorf("summary after restoring mismatch (-want +got):\n%s", diff)
	}
	reopened := curatedRule(fluxMegapixelsKey, fluxModel, "megapixels", `{}`, 3_000_000, fluxSourceURL, thirdImport)
	reopened.MinimumChargeNanos = pointer(int64(10_000_000))
	reopened.BillingIncrementMicros = pointer(int64(1_000_000))
	wantRules = []storedRule{deprecated, reopened, firstRules[1], firstRules[2]}
	if diff := cmp.Diff(wantRules, selectStoredRules(t, pool, "curated"), cmpopts.IgnoreFields(storedRule{}, "PricingRuleID", "ImportedAt")); diff != "" {
		t.Errorf("rules after restoring mismatch (-want +got):\n%s", diff)
	}
}

func TestImportCuratedChangedSourceURLUpdatesInPlace(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	timeSource := clock.NewManual(importStart)
	if _, err := pricing.ImportCurated(t.Context(), pool, curatedFixture(), timeSource, discardLogger()); err != nil {
		t.Fatalf("first ImportCurated: %v", err)
	}
	firstRules := selectStoredRules(t, pool, "curated")
	moved := curatedFixture()
	moved.CuratedModels[0].SourceURL = "https://fal.ai/pricing"
	timeSource.Set(secondImport)

	summary, err := pricing.ImportCurated(t.Context(), pool, moved, timeSource, discardLogger())

	if err != nil {
		t.Fatalf("second ImportCurated: %v", err)
	}
	wantSummary := pricing.ImportSummary{Source: pricing.RuleSourceCurated, Updated: 2, Unchanged: 1, Aliases: 1}
	if diff := cmp.Diff(wantSummary, summary); diff != "" {
		t.Errorf("summary mismatch (-want +got):\n%s", diff)
	}
	wantRules := []storedRule{firstRules[0], firstRules[1], firstRules[2]}
	for index := 1; index <= 2; index++ {
		wantRules[index].SourceURL = pointer("https://fal.ai/pricing")
		wantRules[index].ImportedAt = secondImport
	}
	if diff := cmp.Diff(wantRules, selectStoredRules(t, pool, "curated")); diff != "" {
		t.Errorf("rules mismatch (-want +got):\n%s", diff)
	}
}

func TestImportCuratedReplacesAliases(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	timeSource := clock.NewManual(importStart)
	if _, err := pricing.ImportCurated(t.Context(), pool, curatedFixture(), timeSource, discardLogger()); err != nil {
		t.Fatalf("first ImportCurated: %v", err)
	}
	renamed := curatedFixture()
	renamed.Aliases = []catalogfiles.Alias{
		{Provider: curatedProvider, Alias: "fal-ai/flux/schnell/fast", Model: fluxModel},
		{Provider: curatedProvider, Alias: "fal-ai/veo3.1/fast/reference-to-video", Model: veoModel},
	}
	timeSource.Set(secondImport)

	summary, err := pricing.ImportCurated(t.Context(), pool, renamed, timeSource, discardLogger())

	if err != nil {
		t.Fatalf("second ImportCurated: %v", err)
	}
	if summary.Aliases != 2 {
		t.Errorf("summary aliases = %d, want 2", summary.Aliases)
	}
	wantAliases := []storedAlias{
		{Provider: curatedProvider, Alias: "fal-ai/flux/schnell/fast", Model: fluxModel},
		{Provider: curatedProvider, Alias: "fal-ai/veo3.1/fast/reference-to-video", Model: veoModel},
	}
	if diff := cmp.Diff(wantAliases, selectStoredAliases(t, pool)); diff != "" {
		t.Errorf("aliases mismatch (-want +got):\n%s", diff)
	}
}

func curatedFixture() catalogfiles.Catalog {
	minimumCharge := money.Amount(10_000_000)
	billingIncrement := money.Quantity(1_000_000)
	return catalogfiles.Catalog{
		CuratedModels: []catalogfiles.CuratedModel{
			{
				Provider:    curatedProvider,
				Model:       veoModel,
				DisplayName: "Veo 3.1 Fast",
				SourceURL:   veoSourceURL,
				VerifiedOn:  time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC),
				Rules: []catalogfiles.Rule{
					{Meter: "output_seconds", Conditions: catalogfiles.Attributes{}, UnitPrice: money.UnitPrice{Nanos: 150_000_000, UnitQuantity: 1}},
					{
						Meter:      "output_seconds",
						Conditions: catalogfiles.Attributes{"resolution": "4k", "audio": false},
						UnitPrice:  money.UnitPrice{Nanos: 300_000_000, UnitQuantity: 1},
					},
				},
			},
			{
				Provider:    curatedProvider,
				Model:       fluxModel,
				DisplayName: "FLUX.1 [schnell]",
				SourceURL:   fluxSourceURL,
				VerifiedOn:  time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC),
				Rules: []catalogfiles.Rule{
					{
						Meter:            "megapixels",
						Conditions:       catalogfiles.Attributes{},
						UnitPrice:        money.UnitPrice{Nanos: 3_000_000, UnitQuantity: 1},
						MinimumCharge:    &minimumCharge,
						BillingIncrement: &billingIncrement,
					},
				},
			},
		},
		Aliases: []catalogfiles.Alias{{Provider: curatedProvider, Alias: veoAlias, Model: veoModel}},
	}
}

func curatedRule(sourceKey, model, meter, conditions string, nanos int64, sourceURL string, effectiveFrom time.Time) storedRule {
	return storedRule{
		SourceKey:      sourceKey,
		Provider:       curatedProvider,
		Model:          model,
		Meter:          meter,
		Conditions:     conditions,
		UnitPriceNanos: nanos,
		UnitQuantity:   1,
		EffectiveFrom:  effectiveFrom,
		Status:         "active",
		SourceURL:      &sourceURL,
		ImportedAt:     effectiveFrom,
	}
}

func selectStoredRules(t *testing.T, pool *pgxpool.Pool, source string) []storedRule {
	t.Helper()
	rows, err := pool.Query(t.Context(),
		`SELECT pricing_rule_id, source_key, provider, model, meter, conditions::text, unit_price_nanos, unit_quantity,
			minimum_charge_nanos, billing_increment_micros, effective_from, effective_to, status, source_url, imported_at
		FROM pricing_rules
		WHERE source = $1
		ORDER BY source_key COLLATE "C", effective_from`,
		source,
	)
	if err != nil {
		t.Fatalf("select pricing rules: %v", err)
	}
	rules, err := pgx.CollectRows(rows, pgx.RowToStructByPos[storedRule])
	if err != nil {
		t.Fatalf("read pricing rules: %v", err)
	}
	return rules
}

func selectStoredAliases(t *testing.T, pool *pgxpool.Pool) []storedAlias {
	t.Helper()
	rows, err := pool.Query(t.Context(), "SELECT provider, alias, model FROM provider_model_aliases ORDER BY provider COLLATE \"C\", alias COLLATE \"C\"")
	if err != nil {
		t.Fatalf("select aliases: %v", err)
	}
	aliases, err := pgx.CollectRows(rows, pgx.RowToStructByPos[storedAlias])
	if err != nil {
		t.Fatalf("read aliases: %v", err)
	}
	return aliases
}

func discardLogger() *logging.Logger {
	return logging.New(io.Discard, slog.LevelDebug)
}

func pointer[Value any](value Value) *Value {
	return &value
}
