package pricing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing/queries"
)

const (
	importLockName          = "pricing_import"
	sourceKeySeparator      = "/"
	tokenUnitQuantity       = 1_000_000
	singleUnitQuantity      = 1
	contextTierAttribute    = "context_tier"
	liteLLMSampleSpecKey    = "sample_spec"
	liteLLMProviderField    = "litellm_provider"
	liteLLMSearchField      = "search_context_cost_per_query"
	liteLLMSearchPriceField = "search_context_size_medium"
	vendoredSnapshotPath    = "litellm/model_prices_and_context_window.json"
	vendoredSourcePath      = "litellm/SOURCE"
	vendoredFetchDayField   = "fetched_on"
)

// ImportSummary counts what one catalog import did. Every source key the
// import lists counts once in Created, Changed, Updated or Unchanged, and
// every open rule whose key it no longer lists counts in Deprecated. A
// skipped import counts nothing.
type ImportSummary struct {
	// Source is the rule source the import wrote.
	Source RuleSource
	// Skipped is true when a LiteLLM import wrote nothing, because the
	// database holds rules from a snapshot fetched at or after its own.
	Skipped bool
	// Created counts the source keys without an open rule that got one.
	Created int
	// Changed counts the open rules whose unit price, minimum charge or
	// billing increment changed. Each was closed and a rule with the new
	// price opened.
	Changed int
	// Updated counts the open rules whose source URL alone changed. Each was
	// updated in place.
	Updated int
	// Deprecated counts the open rules whose source key the import no longer
	// lists. Each was closed and marked deprecated.
	Deprecated int
	// Unchanged counts the open rules the import left as they were.
	Unchanged int
	// Aliases counts the model aliases written, 0 for a LiteLLM import.
	Aliases int
}

type importedRule struct {
	sourceKey        string
	provider         string
	model            string
	meter            Meter
	conditions       []byte
	unitPrice        money.UnitPrice
	minimumCharge    *money.Amount
	billingIncrement *money.Quantity
	sourceURL        *string
}

// LiteLLMSnapshot is one LiteLLM model_prices_and_context_window file and
// the time it was fetched from LiteLLM, which orders snapshots.
type LiteLLMSnapshot struct {
	// Content is the JSON text of the file.
	Content []byte
	// FetchedAt is when the file was downloaded from LiteLLM.
	FetchedAt time.Time
}

type liteLLMPriceField struct {
	name         string
	meter        Meter
	unitQuantity int64
}

type liteLLMContextTier struct {
	fieldSuffix string
	value       string
}

type liteLLMPrice struct {
	meter       Meter
	contextTier string
	unitPrice   money.UnitPrice
}

type importWork func(ctx context.Context, transactionQueries *queries.Queries, now time.Time) (ImportSummary, error)

// ErrInvalidLiteLLMSnapshot reports a LiteLLM price snapshot that is not one
// JSON object of model entries, prices no entry, or holds an entry whose
// cost fields or litellm_provider break LiteLLM's schema. The refresh job
// also returns it for a download above 20 MiB, and VendoredLiteLLMSnapshot
// for a SOURCE file without exactly one fetched_on date.
var ErrInvalidLiteLLMSnapshot = errors.New("invalid litellm snapshot")

var liteLLMPriceFields = []liteLLMPriceField{
	{name: "input_cost_per_token", meter: MeterInputTokens, unitQuantity: tokenUnitQuantity},
	{name: "cache_read_input_token_cost", meter: MeterCachedInputTokens, unitQuantity: tokenUnitQuantity},
	{name: "cache_creation_input_token_cost", meter: MeterCacheWriteInputTokens, unitQuantity: tokenUnitQuantity},
	{name: "output_cost_per_token", meter: MeterOutputTokens, unitQuantity: tokenUnitQuantity},
	{name: "output_cost_per_reasoning_token", meter: MeterReasoningTokens, unitQuantity: tokenUnitQuantity},
	{name: "input_cost_per_audio_token", meter: MeterInputAudioTokens, unitQuantity: tokenUnitQuantity},
	{name: "output_cost_per_audio_token", meter: MeterOutputAudioTokens, unitQuantity: tokenUnitQuantity},
	{name: "output_cost_per_image", meter: MeterImages, unitQuantity: singleUnitQuantity},
	{name: "input_cost_per_second", meter: MeterInputSeconds, unitQuantity: singleUnitQuantity},
	{name: "output_cost_per_second", meter: MeterOutputSeconds, unitQuantity: singleUnitQuantity},
}

var liteLLMContextTiers = []liteLLMContextTier{
	{fieldSuffix: "", value: ""},
	{fieldSuffix: "_above_200k_tokens", value: "above_200k"},
	{fieldSuffix: "_above_272k_tokens", value: "above_272k"},
}

// VendoredLiteLLMSnapshot returns the LiteLLM snapshot that files holds at
// litellm/model_prices_and_context_window.json, fetched at the start of the
// UTC day its litellm/SOURCE file names on the fetched_on line, such as
// fetched_on: 2026-09-26. A SOURCE file without exactly one such date
// returns an error wrapping ErrInvalidLiteLLMSnapshot.
func VendoredLiteLLMSnapshot(files fs.FS) (LiteLLMSnapshot, error) {
	content, err := fs.ReadFile(files, vendoredSnapshotPath)
	if err != nil {
		return LiteLLMSnapshot{}, fmt.Errorf("read vendored litellm snapshot: %w", err)
	}
	source, err := fs.ReadFile(files, vendoredSourcePath)
	if err != nil {
		return LiteLLMSnapshot{}, fmt.Errorf("read vendored litellm source: %w", err)
	}
	fetchedAt, err := vendoredFetchDay(source)
	if err != nil {
		return LiteLLMSnapshot{}, fmt.Errorf("%w: %s: %w", ErrInvalidLiteLLMSnapshot, vendoredSourcePath, err)
	}
	return LiteLLMSnapshot{Content: content, FetchedAt: fetchedAt}, nil
}

// ImportCurated writes the rules of the curated catalog files as source
// curated and replaces every model alias with the aliases of files. It runs
// in one transaction under the Postgres advisory lock pricing_import, so
// concurrent imports run one after the other and each sees the rows of the
// one before.
//
// Each rule is identified by its source key,
// <provider>/<model>/<meter>/<canonical conditions>. A key without an open
// rule gets a new active rule from now. A key whose unit price, minimum
// charge or billing increment changed has its open rule closed at now and a
// new rule opened from now. A key whose source URL alone changed is updated
// in place. An open rule whose key the files no longer list is closed at now
// and marked deprecated. Every rule it writes or closes records now in
// imported_at. It logs pricing.catalog_imported with the counts.
func ImportCurated(ctx context.Context, pool *pgxpool.Pool, files catalogfiles.Catalog, timeSource clock.Clock, logger *logging.Logger) (ImportSummary, error) {
	rules, err := curatedRules(files)
	if err != nil {
		return ImportSummary{}, err
	}
	return runImport(ctx, pool, timeSource, logger, func(ctx context.Context, transactionQueries *queries.Queries, now time.Time) (ImportSummary, error) {
		summary, err := reconcileRules(ctx, transactionQueries, RuleSourceCurated, rules, now, now)
		if err != nil {
			return ImportSummary{}, err
		}
		summary.Aliases, err = replaceAliases(ctx, transactionQueries, files.Aliases)
		return summary, err
	})
}

// ImportLiteLLM writes the prices of a LiteLLM model_prices_and_context_window
// snapshot as source litellm, with the lock, transaction, rule changes and
// log event of ImportCurated. It leaves the model aliases alone. Every rule
// it writes or closes records snapshot.FetchedAt in imported_at, so the
// newest imported_at of the litellm rules tells when the snapshot the
// database holds was fetched. When that is at or after snapshot.FetchedAt,
// it writes nothing, logs pricing.catalog_import_skipped and returns a
// summary with Skipped set, so an older snapshot never replaces a newer one.
//
// Numbers are decoded with json.Decoder.UseNumber and converted from their
// decimal text in math/big, never through float64. A price finer than one
// nano-USD per unit quantity is rounded half up. Token prices are stored per
// 1,000,000 tokens and every other price per 1 unit. The mapped fields are
// input_cost_per_token, cache_read_input_token_cost,
// cache_creation_input_token_cost, output_cost_per_token,
// output_cost_per_reasoning_token (only where it differs from
// output_cost_per_token), input_cost_per_audio_token,
// output_cost_per_audio_token, output_cost_per_image, input_cost_per_second
// and output_cost_per_second, each also with the suffixes
// _above_200k_tokens and _above_272k_tokens, which add the condition
// context_tier above_200k or above_272k. search_context_cost_per_query
// prices search_requests at its search_context_size_medium value.
//
// The provider is the entry's litellm_provider and the model is the entry
// key without a leading <provider>/. When a bare key and a <provider>/ key
// name the same model, the <provider>/ entry wins. The sample_spec entry and
// entries without a mapped cost field are skipped. An invalid snapshot
// returns an error wrapping ErrInvalidLiteLLMSnapshot and writes nothing.
func ImportLiteLLM(ctx context.Context, pool *pgxpool.Pool, snapshot LiteLLMSnapshot, timeSource clock.Clock, logger *logging.Logger) (ImportSummary, error) {
	rules, err := liteLLMRules(snapshot.Content)
	if err != nil {
		return ImportSummary{}, err
	}
	return runImport(ctx, pool, timeSource, logger, func(ctx context.Context, transactionQueries *queries.Queries, now time.Time) (ImportSummary, error) {
		holdsSameOrNewer, err := holdsSnapshotFetchedSince(ctx, transactionQueries, snapshot.FetchedAt)
		if err != nil {
			return ImportSummary{}, err
		}
		if holdsSameOrNewer {
			return ImportSummary{Source: RuleSourceLiteLLM, Skipped: true}, nil
		}
		return reconcileRules(ctx, transactionQueries, RuleSourceLiteLLM, rules, now, snapshot.FetchedAt)
	})
}

func runImport(ctx context.Context, pool *pgxpool.Pool, timeSource clock.Clock, logger *logging.Logger, work importWork) (ImportSummary, error) {
	release, err := database.AcquireAdvisoryLock(ctx, pool, importLockName)
	if err != nil {
		return ImportSummary{}, err
	}
	defer release()
	now := timeSource.Now()
	var summary ImportSummary
	err = database.InTransaction(ctx, pool, func(ctx context.Context, transaction pgx.Tx) error {
		imported, err := work(ctx, queries.New(transaction), now)
		summary = imported
		return err
	})
	if err != nil {
		return ImportSummary{}, err
	}
	if summary.Skipped {
		logger.Info(ctx, logging.PricingCatalogImportSkipped, slog.String("source", string(summary.Source)))
		return summary, nil
	}
	logger.Info(ctx, logging.PricingCatalogImported,
		slog.String("source", string(summary.Source)),
		slog.Int("created", summary.Created),
		slog.Int("changed", summary.Changed),
		slog.Int("updated", summary.Updated),
		slog.Int("deprecated", summary.Deprecated),
		slog.Int("unchanged", summary.Unchanged),
		slog.Int("aliases", summary.Aliases),
	)
	return summary, nil
}

func holdsSnapshotFetchedSince(ctx context.Context, transactionQueries *queries.Queries, fetchedAt time.Time) (bool, error) {
	latest, err := transactionQueries.SelectLatestImportedAt(ctx, string(RuleSourceLiteLLM))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("select latest litellm import: %w", err)
	}
	return !latest.Before(fetchedAt), nil
}

func reconcileRules(ctx context.Context, transactionQueries *queries.Queries, source RuleSource, rules []importedRule, now, importedAt time.Time) (ImportSummary, error) {
	openRows, err := transactionQueries.SelectOpenPricingRules(ctx, string(source))
	if err != nil {
		return ImportSummary{}, fmt.Errorf("select open %s rules: %w", source, err)
	}
	openRules := make(map[string]queries.SelectOpenPricingRulesRow, len(openRows))
	for _, row := range openRows {
		openRules[row.SourceKey] = row
	}
	summary := ImportSummary{Source: source}
	for _, rule := range rules {
		open, found := openRules[rule.sourceKey]
		delete(openRules, rule.sourceKey)
		switch {
		case !found:
			summary.Created++
			err = writeRule(ctx, transactionQueries, source, rule, now, importedAt)
		case !rule.hasPriceOf(open):
			summary.Changed++
			err = replaceRule(ctx, transactionQueries, source, rule, open.PricingRuleID, now, importedAt)
		case !equalOptional(rule.sourceURL, open.SourceURL):
			summary.Updated++
			err = writeRule(ctx, transactionQueries, source, rule, open.EffectiveFrom, importedAt)
		default:
			summary.Unchanged++
		}
		if err != nil {
			return ImportSummary{}, fmt.Errorf("import %s rule %s: %w", source, rule.sourceKey, err)
		}
	}
	for _, sourceKey := range slices.Sorted(maps.Keys(openRules)) {
		if err := closeRule(ctx, transactionQueries, openRules[sourceKey].PricingRuleID, now, importedAt, RuleStatusDeprecated); err != nil {
			return ImportSummary{}, fmt.Errorf("deprecate %s rule %s: %w", source, sourceKey, err)
		}
		summary.Deprecated++
	}
	return summary, nil
}

func replaceRule(ctx context.Context, transactionQueries *queries.Queries, source RuleSource, rule importedRule, openRuleID uuid.UUID, now, importedAt time.Time) error {
	if err := closeRule(ctx, transactionQueries, openRuleID, now, importedAt, RuleStatusActive); err != nil {
		return err
	}
	return writeRule(ctx, transactionQueries, source, rule, now, importedAt)
}

func writeRule(ctx context.Context, transactionQueries *queries.Queries, source RuleSource, rule importedRule, effectiveFrom, importedAt time.Time) error {
	return transactionQueries.UpsertPricingRule(ctx, queries.UpsertPricingRuleParams{
		PricingRuleID:          identifiers.New(),
		Provider:               rule.provider,
		Model:                  rule.model,
		Meter:                  string(rule.meter),
		Conditions:             rule.conditions,
		UnitPriceNanos:         rule.unitPrice.Nanos,
		UnitQuantity:           rule.unitPrice.UnitQuantity,
		MinimumChargeNanos:     rule.minimumCharge,
		BillingIncrementMicros: rule.billingIncrement,
		EffectiveFrom:          effectiveFrom,
		Source:                 string(source),
		SourceKey:              rule.sourceKey,
		SourceURL:              rule.sourceURL,
		ImportedAt:             importedAt,
	})
}

func closeRule(ctx context.Context, transactionQueries *queries.Queries, ruleID uuid.UUID, now, importedAt time.Time, status RuleStatus) error {
	return transactionQueries.ClosePricingRule(ctx, queries.ClosePricingRuleParams{
		EffectiveTo:   now,
		Status:        string(status),
		ImportedAt:    importedAt,
		PricingRuleID: ruleID,
	})
}

func replaceAliases(ctx context.Context, transactionQueries *queries.Queries, aliases []catalogfiles.Alias) (int, error) {
	if err := transactionQueries.DeleteProviderModelAliases(ctx); err != nil {
		return 0, fmt.Errorf("delete model aliases: %w", err)
	}
	for _, alias := range aliases {
		err := transactionQueries.InsertProviderModelAlias(ctx, queries.InsertProviderModelAliasParams{Provider: alias.Provider, Alias: alias.Alias, Model: alias.Model})
		if err != nil {
			return 0, fmt.Errorf("insert model alias %s/%s: %w", alias.Provider, alias.Alias, err)
		}
	}
	return len(aliases), nil
}

func (summary ImportSummary) wroteRules() bool {
	return summary.Created+summary.Changed+summary.Updated+summary.Deprecated > 0
}

func (rule importedRule) hasPriceOf(open queries.SelectOpenPricingRulesRow) bool {
	return rule.unitPrice == (money.UnitPrice{Nanos: open.UnitPriceNanos, UnitQuantity: open.UnitQuantity}) &&
		equalOptional(rule.minimumCharge, open.MinimumChargeNanos) &&
		equalOptional(rule.billingIncrement, open.BillingIncrementMicros)
}

func equalOptional[Value comparable](first, second *Value) bool {
	if first == nil || second == nil {
		return first == second
	}
	return *first == *second
}

func vendoredFetchDay(source []byte) (time.Time, error) {
	var days []string
	for line := range strings.Lines(string(source)) {
		field, value, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(field) == vendoredFetchDayField {
			days = append(days, strings.TrimSpace(value))
		}
	}
	if len(days) != 1 {
		return time.Time{}, fmt.Errorf("want one %s line, found %d", vendoredFetchDayField, len(days))
	}
	return time.Parse(time.DateOnly, days[0])
}

func curatedRules(files catalogfiles.Catalog) ([]importedRule, error) {
	rules := []importedRule{}
	for _, model := range files.CuratedModels {
		sourceURL := model.SourceURL
		for _, rule := range model.Rules {
			meter, err := ParseMeter(rule.Meter)
			if err != nil {
				return nil, fmt.Errorf("curated model %s/%s: %w", model.Provider, model.Model, err)
			}
			conditions, err := conditionsJSON(rule.Conditions)
			if err != nil {
				return nil, fmt.Errorf("curated model %s/%s: %w", model.Provider, model.Model, err)
			}
			rules = append(rules, importedRule{
				sourceKey:        sourceKey(model.Provider, model.Model, meter, rule.Conditions),
				provider:         model.Provider,
				model:            model.Model,
				meter:            meter,
				conditions:       conditions,
				unitPrice:        rule.UnitPrice,
				minimumCharge:    rule.MinimumCharge,
				billingIncrement: rule.BillingIncrement,
				sourceURL:        &sourceURL,
			})
		}
	}
	return rules, nil
}

func liteLLMRules(snapshot []byte) ([]importedRule, error) {
	entries, err := decodeLiteLLMSnapshot(snapshot)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidLiteLLMSnapshot, err)
	}
	selected := map[modelKey][]importedRule{}
	for _, key := range slices.Sorted(maps.Keys(entries)) {
		if key == liteLLMSampleSpecKey {
			continue
		}
		prices, err := liteLLMPrices(entries[key])
		if err != nil {
			return nil, fmt.Errorf("%w: entry %s: %w", ErrInvalidLiteLLMSnapshot, key, err)
		}
		if len(prices) == 0 {
			continue
		}
		provider, isString := entries[key][liteLLMProviderField].(string)
		if !isString || provider == "" {
			return nil, fmt.Errorf("%w: entry %s: %s must be a provider name", ErrInvalidLiteLLMSnapshot, key, liteLLMProviderField)
		}
		model, qualified := strings.CutPrefix(key, provider+"/")
		identity := modelKey{provider: provider, model: model}
		if _, alreadySelected := selected[identity]; alreadySelected && !qualified {
			continue
		}
		selected[identity], err = liteLLMModelRules(identity, prices)
		if err != nil {
			return nil, fmt.Errorf("entry %s: %w", key, err)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w: no entry has a mapped cost field", ErrInvalidLiteLLMSnapshot)
	}
	return slices.Concat(slices.Collect(maps.Values(selected))...), nil
}

func decodeLiteLLMSnapshot(snapshot []byte) (map[string]map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(snapshot))
	decoder.UseNumber()
	var entries map[string]map[string]any
	if err := decoder.Decode(&entries); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("content after the top-level object")
	}
	return entries, nil
}

func liteLLMPrices(entry map[string]any) ([]liteLLMPrice, error) {
	prices := []liteLLMPrice{}
	for _, tier := range liteLLMContextTiers {
		tierPrices := map[Meter]money.UnitPrice{}
		for _, field := range liteLLMPriceFields {
			value, present := entry[field.name+tier.fieldSuffix]
			if !present {
				continue
			}
			unitPrice, err := liteLLMUnitPrice(value, field.unitQuantity)
			if err != nil {
				return nil, fmt.Errorf("%s%s: %w", field.name, tier.fieldSuffix, err)
			}
			tierPrices[field.meter] = unitPrice
		}
		reasoning, hasReasoning := tierPrices[MeterReasoningTokens]
		output, hasOutput := tierPrices[MeterOutputTokens]
		if hasReasoning && hasOutput && reasoning == output {
			delete(tierPrices, MeterReasoningTokens)
		}
		for meter, unitPrice := range tierPrices {
			prices = append(prices, liteLLMPrice{meter: meter, contextTier: tier.value, unitPrice: unitPrice})
		}
	}
	searchPrices, present := entry[liteLLMSearchField]
	if !present {
		return prices, nil
	}
	searchSizes, isObject := searchPrices.(map[string]any)
	if !isObject {
		return nil, fmt.Errorf("%s must be an object", liteLLMSearchField)
	}
	unitPrice, err := liteLLMUnitPrice(searchSizes[liteLLMSearchPriceField], singleUnitQuantity)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", liteLLMSearchField, liteLLMSearchPriceField, err)
	}
	return append(prices, liteLLMPrice{meter: MeterSearchRequests, unitPrice: unitPrice}), nil
}

func liteLLMUnitPrice(value any, unitQuantity int64) (money.UnitPrice, error) {
	number, isNumber := value.(json.Number)
	if !isNumber {
		return money.UnitPrice{}, errors.New("must be a number")
	}
	price, valid := new(big.Rat).SetString(number.String())
	if !valid || price.Sign() < 0 {
		return money.UnitPrice{}, fmt.Errorf("%s is not a non-negative decimal", number)
	}
	scaled := price.Mul(price, new(big.Rat).SetInt64(nanosPerUSD*unitQuantity))
	nanos, remainder := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
	if doubledRemainder := remainder.Lsh(remainder, 1); doubledRemainder.Cmp(scaled.Denom()) >= 0 {
		nanos.Add(nanos, big.NewInt(1))
	}
	if !nanos.IsInt64() {
		return money.UnitPrice{}, fmt.Errorf("%w: %s USD per unit", money.ErrOverflow, number)
	}
	return money.UnitPrice{Nanos: money.Amount(nanos.Int64()), UnitQuantity: unitQuantity}, nil
}

func liteLLMModelRules(identity modelKey, prices []liteLLMPrice) ([]importedRule, error) {
	rules := make([]importedRule, 0, len(prices))
	for _, price := range prices {
		conditions := catalogfiles.Attributes{}
		if price.contextTier != "" {
			conditions[contextTierAttribute] = price.contextTier
		}
		conditionsText, err := conditionsJSON(conditions)
		if err != nil {
			return nil, err
		}
		rules = append(rules, importedRule{
			sourceKey:  sourceKey(identity.provider, identity.model, price.meter, conditions),
			provider:   identity.provider,
			model:      identity.model,
			meter:      price.meter,
			conditions: conditionsText,
			unitPrice:  price.unitPrice,
		})
	}
	return rules, nil
}

func sourceKey(provider, model string, meter Meter, conditions catalogfiles.Attributes) string {
	return strings.Join([]string{provider, model, string(meter), conditions.Canonical()}, sourceKeySeparator)
}

func conditionsJSON(conditions catalogfiles.Attributes) ([]byte, error) {
	attributes := make(Attributes, len(conditions))
	for key, value := range conditions {
		switch typed := value.(type) {
		case string:
			attributes[key] = StringAttribute(typed)
		case bool:
			attributes[key] = BooleanAttribute(typed)
		default:
			return nil, fmt.Errorf("condition %s holds %T, want a string or a boolean", key, value)
		}
	}
	return json.Marshal(attributes)
}
