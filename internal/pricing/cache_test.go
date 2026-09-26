package pricing_test

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing"
)

const unknownVideoModel = "acme-video-1"

func TestRuleSetExpiresAfterFiveMinutes(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	ruleSets := harness.service.RuleSets()
	assertUnknownModelCost(t, ruleSets, harness.clock.Now(), nil)
	harness.insertUnknownModelOverride(t, httpapi.EnvironmentTest)

	harness.clock.Advance(5*time.Minute - time.Nanosecond)
	assertUnknownModelCost(t, ruleSets, harness.clock.Now(), nil)
	harness.clock.Advance(time.Nanosecond)
	assertUnknownModelCost(t, ruleSets, harness.clock.Now(), pointer(money.Amount(4_000_000_000)))
}

func TestRuleSetsKeepEnvironmentsApart(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	harness.insertUnknownModelOverride(t, httpapi.EnvironmentLive)

	assertEnvironmentCost(t, harness.service.RuleSets(), httpapi.EnvironmentTest, harness.clock.Now(), nil)
	assertEnvironmentCost(t, harness.service.RuleSets(), httpapi.EnvironmentLive, harness.clock.Now(), pointer(money.Amount(4_000_000_000)))
}

func TestPricingInvalidationClearsRuleSets(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	ruleSets := harness.service.RuleSets()
	assertUnknownModelCost(t, ruleSets, harness.clock.Now(), nil)
	harness.insertUnknownModelOverride(t, httpapi.EnvironmentTest)

	ruleSets.Invalidate(cache.Invalidation{Kind: cache.InvalidationKindPlan, Environment: string(httpapi.EnvironmentTest)})
	assertUnknownModelCost(t, ruleSets, harness.clock.Now(), nil)
	ruleSets.Invalidate(cache.Invalidation{Kind: cache.InvalidationKindPricing, Environment: string(httpapi.EnvironmentTest)})
	assertUnknownModelCost(t, ruleSets, harness.clock.Now(), pointer(money.Amount(4_000_000_000)))
}

func TestOverrideInAnotherProcessClearsRuleSetsThroughInvalidation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	otherProcess := pricing.NewService(harness.pool, harness.cache, harness.jobs, harness.files, harness.clock)
	assertUnknownModelCost(t, otherProcess.RuleSets(), harness.clock.Now(), nil)
	subscribe(t, harness.cache, otherProcess.RuleSets())

	harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)

	waitFor(t, "override priced in the other process", func() bool {
		return unknownModelCost(t, otherProcess.RuleSets(), harness.clock.Now()) != nil
	})
}

func TestRuleSetLoadRacingOverrideChangeIsNotCached(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	hook := &queryHook{queryName: "ListRatingOverrides"}
	hooked := harness.newHookedService(t, hook)
	hook.run = func(ctx context.Context) {
		_, err := hooked.CreateOverride(context.WithoutCancel(ctx), httpapi.EnvironmentTest, pricing.CreateOverrideInput{
			Provider:     "acme",
			Model:        unknownVideoModel,
			Meter:        string(pricing.MeterOutputSeconds),
			UnitPrice:    "0.50",
			UnitQuantity: 1,
		})
		if err != nil {
			t.Errorf("create override during the rule set load: %v", err)
		}
	}
	hook.armed.Store(true)

	assertUnknownModelCost(t, hooked.RuleSets(), harness.clock.Now(), nil)
	assertUnknownModelCost(t, hooked.RuleSets(), harness.clock.Now(), pointer(money.Amount(4_000_000_000)))
}

func TestRuleSetRatesLateReportsAtThePriceInEffect(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	priceChange := serviceStart.Add(time.Hour)
	_, err := pricing.ImportCurated(t.Context(), harness.pool, withVeoDefaultPrice(harness.files, 200_000_000), clock.NewManual(priceChange), discardLogger())
	if err != nil {
		t.Fatalf("import changed price: %v", err)
	}
	ruleSets := harness.service.RuleSets()
	beforeChange := serviceStart.Add(30 * time.Minute)
	harness.clock.Set(priceChange.Add(time.Hour))

	assertRatedCost(t, "veo before the price change", veoCost(t, ruleSets, beforeChange), pointer(money.Amount(1_200_000_000)))
	assertRatedCost(t, "veo after the price change", veoCost(t, ruleSets, harness.clock.Now()), pointer(money.Amount(1_600_000_000)))
	harness.clock.Set(priceChange.Add(90*24*time.Hour - time.Nanosecond))
	assertRatedCost(t, "veo before the price change at the end of the window", veoCost(t, ruleSets, beforeChange), pointer(money.Amount(1_200_000_000)))
	harness.clock.Set(priceChange.Add(90 * 24 * time.Hour))
	ruleSets.Clear()
	assertRatedCost(t, "veo before the price change after the window", veoCost(t, ruleSets, beforeChange), nil)
}

func TestRuleSetRatesLateReportsWithEndedOverride(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)
	harness.clock.Advance(time.Minute)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, overridePath(created["id"]), httpapi.EnvironmentTest, ""), http.StatusNoContent)
	harness.clock.Advance(time.Minute)
	ruleSets := harness.service.RuleSets()

	assertUnknownModelCost(t, ruleSets, serviceStart.Add(30*time.Second), pointer(money.Amount(4_000_000_000)))
	assertUnknownModelCost(t, ruleSets, harness.clock.Now(), nil)
}

func assertUnknownModelCost(t *testing.T, ruleSets *pricing.RuleSetCache, at time.Time, want *money.Amount) {
	t.Helper()
	assertEnvironmentCost(t, ruleSets, httpapi.EnvironmentTest, at, want)
}

func assertEnvironmentCost(t *testing.T, ruleSets *pricing.RuleSetCache, environment httpapi.Environment, at time.Time, want *money.Amount) {
	t.Helper()
	assertRatedCost(t, unknownVideoModel+" in "+string(environment), environmentCost(t, ruleSets, environment, at), want)
}

func assertRatedCost(t *testing.T, description string, got *money.Amount, want *money.Amount) {
	t.Helper()
	if (got == nil) != (want == nil) || (got != nil && *got != *want) {
		t.Errorf("cost of %s = %v, want %v", description, formatOptionalAmount(got), formatOptionalAmount(want))
	}
}

func unknownModelCost(t *testing.T, ruleSets *pricing.RuleSetCache, at time.Time) *money.Amount {
	t.Helper()
	return environmentCost(t, ruleSets, httpapi.EnvironmentTest, at)
}

func environmentCost(t *testing.T, ruleSets *pricing.RuleSetCache, environment httpapi.Environment, at time.Time) *money.Amount {
	t.Helper()
	return ratedCost(t, ruleSets, environment, pricing.RatingRequest{
		Provider:   "acme",
		Model:      unknownVideoModel,
		Usage:      map[pricing.Meter]money.Quantity{pricing.MeterOutputSeconds: 8_000_000},
		OccurredAt: at,
	})
}

func veoCost(t *testing.T, ruleSets *pricing.RuleSetCache, occurredAt time.Time) *money.Amount {
	t.Helper()
	return ratedCost(t, ruleSets, httpapi.EnvironmentTest, pricing.RatingRequest{
		Provider:   curatedProvider,
		Model:      veoModel,
		Attributes: pricing.Attributes{"audio": pricing.BooleanAttribute(true), "resolution": pricing.StringAttribute("720p")},
		Usage:      map[pricing.Meter]money.Quantity{pricing.MeterOutputSeconds: 8_000_000},
		OccurredAt: occurredAt,
	})
}

func ratedCost(t *testing.T, ruleSets *pricing.RuleSetCache, environment httpapi.Environment, request pricing.RatingRequest) *money.Amount {
	t.Helper()
	ruleSet, err := ruleSets.Get(t.Context(), environment)
	if err != nil {
		t.Fatalf("get rule set of %s: %v", environment, err)
	}
	rated, err := pricing.Rate(request, ruleSet)
	if err != nil {
		t.Fatalf("rate %s/%s: %v", request.Provider, request.Model, err)
	}
	return rated.Cost
}

func withVeoDefaultPrice(files catalogfiles.Catalog, nanos money.Amount) catalogfiles.Catalog {
	files.CuratedModels = slices.Clone(files.CuratedModels)
	for modelIndex, model := range files.CuratedModels {
		if model.Model != veoModel {
			continue
		}
		model.Rules = slices.Clone(model.Rules)
		for ruleIndex, rule := range model.Rules {
			if len(rule.Conditions) == 0 {
				model.Rules[ruleIndex].UnitPrice.Nanos = nanos
			}
		}
		files.CuratedModels[modelIndex] = model
	}
	return files
}

func formatOptionalAmount(amount *money.Amount) string {
	if amount == nil {
		return "uncosted"
	}
	return money.FormatAmount(*amount)
}
