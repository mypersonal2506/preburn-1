package pricing_test

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/preburn/preburn/catalog"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	liteLLMSnapshotPath     = "litellm/model_prices_and_context_window.json"
	liteLLMFixtureRuleCount = 22
	shippedSnapshotMinimum  = 1_000
	perMillionTokenQuantity = 1_000_000
)

const liteLLMFixture = `{
	"sample_spec": {
		"input_cost_per_token": 0.0,
		"litellm_provider": "one of https://docs.litellm.ai/docs/providers",
		"search_context_cost_per_query": {"search_context_size_low": 0.0, "search_context_size_medium": 0.0, "search_context_size_high": 0.0}
	},
	"fallback_generalizations": {"rules": [{"name": "example", "model_info": {"litellm_provider": "openai"}}]},
	"gpt-example": {
		"litellm_provider": "openai",
		"mode": "chat",
		"max_tokens": 128000,
		"input_cost_per_token": 1.5e-07,
		"cache_read_input_token_cost": 7.5e-08,
		"cache_creation_input_token_cost": 1.875e-07,
		"output_cost_per_token": 6e-07,
		"output_cost_per_reasoning_token": 0.0000006,
		"input_cost_per_token_above_272k_tokens": 3E-7,
		"output_cost_per_token_above_272k_tokens": 0.0000012,
		"input_cost_per_token_batches": 7.5e-08,
		"input_cost_per_token_priority": 3e-07,
		"search_context_cost_per_query": {"search_context_size_low": 0.025, "search_context_size_medium": 0.03, "search_context_size_high": 0.05}
	},
	"gemini/gemini-example": {
		"litellm_provider": "gemini",
		"input_cost_per_token": 1.25e-06,
		"input_cost_per_token_above_200k_tokens": 2.5e-06,
		"cache_read_input_token_cost": 3.125e-07,
		"cache_read_input_token_cost_above_200k_tokens": 6.25e-07,
		"output_cost_per_token": 1e-05,
		"output_cost_per_token_above_200k_tokens": 1.5e-05,
		"output_cost_per_reasoning_token": 1.2e-05,
		"input_cost_per_audio_token": 1e-06
	},
	"gemini-example": {
		"litellm_provider": "gemini",
		"input_cost_per_token": 9e-06
	},
	"gpt-realtime-example": {
		"litellm_provider": "openai",
		"input_cost_per_audio_token": 3.2e-05,
		"output_cost_per_audio_token": 6.4e-05
	},
	"image-example": {
		"litellm_provider": "openai",
		"output_cost_per_image": 0.04
	},
	"vertex_ai/veo-example": {
		"litellm_provider": "vertex_ai-video-models",
		"output_cost_per_second": 0.4,
		"output_cost_per_second_1080p": 0.5
	},
	"whisper-example": {
		"litellm_provider": "openai",
		"input_cost_per_second": 0.0001
	},
	"databricks/sub-nano-example": {
		"litellm_provider": "databricks",
		"input_cost_per_token": 3.0001999999999996E-7,
		"output_cost_per_token": 0
	},
	"no-cost-example": {
		"litellm_provider": "openai",
		"mode": "chat",
		"max_tokens": 8192
	}
}`

func TestImportLiteLLMMapsSnapshotFields(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)

	summary, err := pricing.ImportLiteLLM(t.Context(), pool, fetchedSnapshot(liteLLMFixture, importStart), clock.NewManual(importStart), discardLogger())

	if err != nil {
		t.Fatalf("ImportLiteLLM: %v", err)
	}
	wantSummary := pricing.ImportSummary{Source: pricing.RuleSourceLiteLLM, Created: liteLLMFixtureRuleCount}
	if diff := cmp.Diff(wantSummary, summary); diff != "" {
		t.Errorf("summary mismatch (-want +got):\n%s", diff)
	}
	wantRules := []storedRule{
		liteLLMRule("databricks", "sub-nano-example", "input_tokens", "", 300_020_000, perMillionTokenQuantity),
		liteLLMRule("databricks", "sub-nano-example", "output_tokens", "", 0, perMillionTokenQuantity),
		liteLLMRule("gemini", "gemini-example", "cached_input_tokens", "", 312_500_000, perMillionTokenQuantity),
		liteLLMRule("gemini", "gemini-example", "cached_input_tokens", "above_200k", 625_000_000, perMillionTokenQuantity),
		liteLLMRule("gemini", "gemini-example", "input_audio_tokens", "", 1_000_000_000, perMillionTokenQuantity),
		liteLLMRule("gemini", "gemini-example", "input_tokens", "", 1_250_000_000, perMillionTokenQuantity),
		liteLLMRule("gemini", "gemini-example", "input_tokens", "above_200k", 2_500_000_000, perMillionTokenQuantity),
		liteLLMRule("gemini", "gemini-example", "output_tokens", "", 10_000_000_000, perMillionTokenQuantity),
		liteLLMRule("gemini", "gemini-example", "output_tokens", "above_200k", 15_000_000_000, perMillionTokenQuantity),
		liteLLMRule("gemini", "gemini-example", "reasoning_tokens", "", 12_000_000_000, perMillionTokenQuantity),
		liteLLMRule("openai", "gpt-example", "cache_write_input_tokens", "", 187_500_000, perMillionTokenQuantity),
		liteLLMRule("openai", "gpt-example", "cached_input_tokens", "", 75_000_000, perMillionTokenQuantity),
		liteLLMRule("openai", "gpt-example", "input_tokens", "", 150_000_000, perMillionTokenQuantity),
		liteLLMRule("openai", "gpt-example", "input_tokens", "above_272k", 300_000_000, perMillionTokenQuantity),
		liteLLMRule("openai", "gpt-example", "output_tokens", "", 600_000_000, perMillionTokenQuantity),
		liteLLMRule("openai", "gpt-example", "output_tokens", "above_272k", 1_200_000_000, perMillionTokenQuantity),
		liteLLMRule("openai", "gpt-example", "search_requests", "", 30_000_000, 1),
		liteLLMRule("openai", "gpt-realtime-example", "input_audio_tokens", "", 32_000_000_000, perMillionTokenQuantity),
		liteLLMRule("openai", "gpt-realtime-example", "output_audio_tokens", "", 64_000_000_000, perMillionTokenQuantity),
		liteLLMRule("openai", "image-example", "images", "", 40_000_000, 1),
		liteLLMRule("openai", "whisper-example", "input_seconds", "", 100_000, 1),
		liteLLMRule("vertex_ai-video-models", "vertex_ai/veo-example", "output_seconds", "", 400_000_000, 1),
	}
	if diff := cmp.Diff(wantRules, selectStoredRules(t, pool, "litellm"), ignoreRuleID); diff != "" {
		t.Errorf("rules mismatch (-want +got):\n%s", diff)
	}
}

func TestImportLiteLLMRejectsInvalidSnapshot(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	tests := []struct {
		name     string
		snapshot string
	}{
		{name: "not JSON", snapshot: `{"gpt-example": `},
		{name: "not an object", snapshot: `[1, 2]`},
		{name: "null", snapshot: `null`},
		{name: "no entries", snapshot: `{}`},
		{name: "no priced entry", snapshot: `{"no-cost-example": {"litellm_provider": "openai", "max_tokens": 8192}}`},
		{name: "content after the object", snapshot: `{} {}`},
		{name: "entry not an object", snapshot: `{"gpt-example": 1}`},
		{name: "cost without provider", snapshot: `{"gpt-example": {"input_cost_per_token": 1e-06}}`},
		{name: "provider not a string", snapshot: `{"gpt-example": {"litellm_provider": 1, "input_cost_per_token": 1e-06}}`},
		{name: "cost as a string", snapshot: `{"gpt-example": {"litellm_provider": "openai", "input_cost_per_token": "1e-06"}}`},
		{name: "cost null", snapshot: `{"gpt-example": {"litellm_provider": "openai", "output_cost_per_token": null}}`},
		{name: "negative cost", snapshot: `{"gpt-example": {"litellm_provider": "openai", "input_cost_per_token": -1e-06}}`},
		{name: "search cost not an object", snapshot: `{"gpt-example": {"litellm_provider": "openai", "search_context_cost_per_query": 0.03}}`},
		{name: "search cost without medium size", snapshot: `{"gpt-example": {"litellm_provider": "openai", "search_context_cost_per_query": {"search_context_size_low": 0.03}}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := pricing.ImportLiteLLM(t.Context(), pool, fetchedSnapshot(test.snapshot, importStart), clock.NewManual(importStart), discardLogger())

			if !errors.Is(err, pricing.ErrInvalidLiteLLMSnapshot) {
				t.Errorf("ImportLiteLLM error = %v, want ErrInvalidLiteLLMSnapshot", err)
			}
		})
	}
	if rules := selectStoredRules(t, pool, "litellm"); len(rules) != 0 {
		t.Errorf("invalid snapshots stored %d rules, want 0", len(rules))
	}
}

func TestImportLiteLLMShippedSnapshot(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	snapshot, err := pricing.VendoredLiteLLMSnapshot(catalog.Files)
	if err != nil {
		t.Fatalf("read shipped snapshot: %v", err)
	}
	files, err := catalogfiles.Load(catalog.Files)
	if err != nil {
		t.Fatalf("load shipped catalog: %v", err)
	}

	summary, err := pricing.ImportLiteLLM(t.Context(), pool, snapshot, clock.NewManual(importStart), discardLogger())

	if err != nil {
		t.Fatalf("ImportLiteLLM: %v", err)
	}
	if summary.Created <= shippedSnapshotMinimum {
		t.Errorf("shipped snapshot created %d rules, want more than %d", summary.Created, shippedSnapshotMinimum)
	}
	for _, model := range files.LiteLLMModels {
		var ruleCount int
		err := pool.QueryRow(t.Context(),
			"SELECT count(*) FROM pricing_rules WHERE source = 'litellm' AND provider = $1 AND model = $2",
			model.Provider, model.Model,
		).Scan(&ruleCount)
		if err != nil {
			t.Fatalf("count rules of %s: %v", model, err)
		}
		if ruleCount == 0 {
			t.Errorf("catalog files name LiteLLM model %s, which the shipped snapshot does not price", model)
		}
	}
}

func TestConcurrentImportsSerialize(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	timeSource := clock.NewManual(importStart)
	summaries := make([]pricing.ImportSummary, 2)
	importErrors := make([]error, 2)
	var group sync.WaitGroup

	for index := range summaries {
		group.Go(func() {
			summaries[index], importErrors[index] = pricing.ImportLiteLLM(t.Context(), pool, fetchedSnapshot(liteLLMFixture, importStart), timeSource, discardLogger())
		})
	}
	group.Wait()

	for index, err := range importErrors {
		if err != nil {
			t.Fatalf("import %d: %v", index, err)
		}
	}
	wantSummaries := []pricing.ImportSummary{
		{Source: pricing.RuleSourceLiteLLM, Created: liteLLMFixtureRuleCount},
		{Source: pricing.RuleSourceLiteLLM, Skipped: true},
	}
	sortByCreated := cmpopts.SortSlices(func(first, second pricing.ImportSummary) bool { return first.Created > second.Created })
	if diff := cmp.Diff(wantSummaries, summaries, sortByCreated); diff != "" {
		t.Errorf("summaries mismatch (-want +got):\n%s", diff)
	}
	if rules := selectStoredRules(t, pool, "litellm"); len(rules) != liteLLMFixtureRuleCount {
		t.Errorf("stored %d rules, want %d", len(rules), liteLLMFixtureRuleCount)
	}
}

func TestVendoredLiteLLMSnapshotReadsFetchDay(t *testing.T) {
	t.Parallel()
	content, err := catalog.Files.ReadFile(liteLLMSnapshotPath)
	if err != nil {
		t.Fatalf("read shipped snapshot: %v", err)
	}

	snapshot, err := pricing.VendoredLiteLLMSnapshot(catalog.Files)

	if err != nil {
		t.Fatalf("VendoredLiteLLMSnapshot: %v", err)
	}
	if want := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC); !snapshot.FetchedAt.Equal(want) || snapshot.FetchedAt.Location() != time.UTC {
		t.Errorf("FetchedAt = %s, want %s", snapshot.FetchedAt, want)
	}
	if !bytes.Equal(snapshot.Content, content) {
		t.Errorf("Content holds %d bytes, want the %d bytes of %s", len(snapshot.Content), len(content), liteLLMSnapshotPath)
	}
}

func TestVendoredLiteLLMSnapshotRejectsSourceWithoutFetchDay(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
	}{
		{name: "missing fetched_on", source: "repository: https://github.com/BerriAI/litellm\ncommit: 90873c46\n"},
		{name: "fetched_on that is not a date", source: "fetched_on: yesterday\n"},
		{name: "fetched_on twice", source: "fetched_on: 2026-09-26\nfetched_on: 2026-09-27\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := fstest.MapFS{
				liteLLMSnapshotPath: {Data: []byte(liteLLMFixture)},
				"litellm/SOURCE":    {Data: []byte(test.source)},
			}

			_, err := pricing.VendoredLiteLLMSnapshot(files)

			if !errors.Is(err, pricing.ErrInvalidLiteLLMSnapshot) {
				t.Errorf("VendoredLiteLLMSnapshot error = %v, want ErrInvalidLiteLLMSnapshot", err)
			}
		})
	}
}

func TestImportLiteLLMSkipsSnapshotsNoNewerThanTheImportedOne(t *testing.T) {
	t.Parallel()
	refreshed := `{"gpt-example": {"litellm_provider": "openai", "input_cost_per_token": 2e-07}}`
	refreshedRule := liteLLMRule("openai", "gpt-example", "input_tokens", "", 200_000_000, perMillionTokenQuantity)
	tests := []struct {
		name             string
		storedFetchedAt  time.Time
		fetchedAt        time.Time
		wantSummary      pricing.ImportSummary
		wantOpenRuleKeys int
	}{
		{
			name:             "older snapshot imported",
			storedFetchedAt:  importStart.Add(-24 * time.Hour),
			fetchedAt:        importStart,
			wantSummary:      pricing.ImportSummary{Source: pricing.RuleSourceLiteLLM, Created: liteLLMFixtureRuleCount - 1, Changed: 1},
			wantOpenRuleKeys: liteLLMFixtureRuleCount,
		},
		{
			name:             "same snapshot imported",
			storedFetchedAt:  importStart,
			fetchedAt:        importStart,
			wantSummary:      pricing.ImportSummary{Source: pricing.RuleSourceLiteLLM, Skipped: true},
			wantOpenRuleKeys: 1,
		},
		{
			name:             "newer snapshot imported",
			storedFetchedAt:  importStart.Add(time.Hour),
			fetchedAt:        importStart,
			wantSummary:      pricing.ImportSummary{Source: pricing.RuleSourceLiteLLM, Skipped: true},
			wantOpenRuleKeys: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := databasetest.NewPool(t)
			timeSource := clock.NewManual(importStart)
			if _, err := pricing.ImportLiteLLM(t.Context(), pool, fetchedSnapshot(refreshed, test.storedFetchedAt), timeSource, discardLogger()); err != nil {
				t.Fatalf("import stored snapshot: %v", err)
			}
			timeSource.Set(secondImport)

			summary, err := pricing.ImportLiteLLM(t.Context(), pool, fetchedSnapshot(liteLLMFixture, test.fetchedAt), timeSource, discardLogger())

			if err != nil {
				t.Fatalf("ImportLiteLLM: %v", err)
			}
			if diff := cmp.Diff(test.wantSummary, summary); diff != "" {
				t.Errorf("summary mismatch (-want +got):\n%s", diff)
			}
			var openRules int
			if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM pricing_rules WHERE source = 'litellm' AND effective_to IS NULL").Scan(&openRules); err != nil {
				t.Fatalf("count open rules: %v", err)
			}
			if openRules != test.wantOpenRuleKeys {
				t.Errorf("open rules = %d, want %d", openRules, test.wantOpenRuleKeys)
			}
			if test.wantSummary.Skipped {
				refreshedRule.ImportedAt = test.storedFetchedAt
				if diff := cmp.Diff([]storedRule{refreshedRule}, selectStoredRules(t, pool, "litellm"), ignoreRuleID); diff != "" {
					t.Errorf("rules after a skipped import mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestImportLiteLLMRecordsSnapshotFetchTime(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	timeSource := clock.NewManual(importStart)
	firstFetch := importStart.Add(-2 * time.Hour)
	secondFetch := secondImport.Add(-time.Minute)
	first := `{
		"gpt-example": {"litellm_provider": "openai", "input_cost_per_token": 1e-07, "output_cost_per_token": 4e-07},
		"image-example": {"litellm_provider": "openai", "output_cost_per_image": 0.04}
	}`
	second := `{"gpt-example": {"litellm_provider": "openai", "input_cost_per_token": 2e-07, "output_cost_per_token": 4e-07}}`
	if _, err := pricing.ImportLiteLLM(t.Context(), pool, fetchedSnapshot(first, firstFetch), timeSource, discardLogger()); err != nil {
		t.Fatalf("first ImportLiteLLM: %v", err)
	}
	timeSource.Set(secondImport)

	summary, err := pricing.ImportLiteLLM(t.Context(), pool, fetchedSnapshot(second, secondFetch), timeSource, discardLogger())

	if err != nil {
		t.Fatalf("second ImportLiteLLM: %v", err)
	}
	wantSummary := pricing.ImportSummary{Source: pricing.RuleSourceLiteLLM, Changed: 1, Deprecated: 1, Unchanged: 1}
	if diff := cmp.Diff(wantSummary, summary); diff != "" {
		t.Errorf("summary mismatch (-want +got):\n%s", diff)
	}
	closedInput := liteLLMRule("openai", "gpt-example", "input_tokens", "", 100_000_000, perMillionTokenQuantity)
	closedInput.EffectiveTo, closedInput.ImportedAt = pointer(secondImport), secondFetch
	openedInput := liteLLMRule("openai", "gpt-example", "input_tokens", "", 200_000_000, perMillionTokenQuantity)
	openedInput.EffectiveFrom, openedInput.ImportedAt = secondImport, secondFetch
	keptOutput := liteLLMRule("openai", "gpt-example", "output_tokens", "", 400_000_000, perMillionTokenQuantity)
	keptOutput.ImportedAt = firstFetch
	deprecatedImage := liteLLMRule("openai", "image-example", "images", "", 40_000_000, 1)
	deprecatedImage.EffectiveTo, deprecatedImage.Status, deprecatedImage.ImportedAt = pointer(secondImport), "deprecated", secondFetch
	wantRules := []storedRule{closedInput, openedInput, keptOutput, deprecatedImage}
	if diff := cmp.Diff(wantRules, selectStoredRules(t, pool, "litellm"), ignoreRuleID); diff != "" {
		t.Errorf("rules mismatch (-want +got):\n%s", diff)
	}
}

func fetchedSnapshot(content string, fetchedAt time.Time) pricing.LiteLLMSnapshot {
	return pricing.LiteLLMSnapshot{Content: []byte(content), FetchedAt: fetchedAt}
}

func liteLLMRule(provider, model, meter, contextTier string, nanos, unitQuantity int64) storedRule {
	conditions, canonical := `{}`, ""
	if contextTier != "" {
		conditions, canonical = `{"context_tier": "`+contextTier+`"}`, "context_tier="+contextTier
	}
	return storedRule{
		SourceKey:      provider + "/" + model + "/" + meter + "/" + canonical,
		Provider:       provider,
		Model:          model,
		Meter:          meter,
		Conditions:     conditions,
		UnitPriceNanos: nanos,
		UnitQuantity:   unitQuantity,
		EffectiveFrom:  importStart,
		Status:         "active",
		ImportedAt:     importStart,
	}
}
