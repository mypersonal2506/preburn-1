package pricing_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing"
)

var (
	catalogStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	summerStart  = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	augustStart  = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	requestTime  = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	octoberStart = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
)

func TestRate(t *testing.T) {
	gptInput := catalogRule(1, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(150_000_000))
	gptOutput := catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterOutputTokens, nil, perMillionTokens(600_000_000))
	veoSilent := catalogRule(1, "google", "veo-3", pricing.MeterOutputSeconds, pricing.Attributes{"audio": pricing.BooleanAttribute(false)}, perUnit(200_000_000))
	veoWithAudio := catalogRule(2, "google", "veo-3", pricing.MeterOutputSeconds, pricing.Attributes{"audio": pricing.BooleanAttribute(true)}, perUnit(400_000_000))
	veoAnyVideo := catalogRule(1, "google", "veo-3", pricing.MeterOutputSeconds, nil, perUnit(100_000_000))
	veoFullHD := catalogRule(2, "google", "veo-3", pricing.MeterOutputSeconds, fullHD(), perUnit(200_000_000))
	endedAdjustment := override(11, "google", "veo-3", pricing.MeterOutputSeconds, fullHD(), perUnit(300_000_000))
	endedAdjustment.EffectiveTo = &augustStart

	tests := []struct {
		name      string
		rules     []pricing.Rule
		overrides []pricing.Override
		aliases   []pricing.ModelAlias
		request   pricing.RatingRequest
		want      pricing.RatedRequest
	}{
		{
			name:    "single meter token pricing",
			rules:   []pricing.Rule{gptInput},
			request: ratingRequest("openai", "gpt-4o-mini", nil, usage(pricing.MeterInputTokens, 1_234_000_000)),
			want:    costed(185_100, pricedLine(pricing.MeterInputTokens, 1_234_000_000, perMillionTokens(150_000_000), 185_100, 1)),
		},
		{
			name:  "token lines sum into the request cost",
			rules: []pricing.Rule{gptOutput, gptInput},
			request: ratingRequest("openai", "gpt-4o-mini", nil, map[pricing.Meter]money.Quantity{
				pricing.MeterOutputTokens: 500_000_000,
				pricing.MeterInputTokens:  1_000_000_000,
			}),
			want: costed(450_000,
				pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(150_000_000), 150_000, 1),
				pricedLine(pricing.MeterOutputTokens, 500_000_000, perMillionTokens(600_000_000), 300_000, 2),
			),
		},
		{
			name:    "per second video priced by the audio condition",
			rules:   []pricing.Rule{veoSilent, veoWithAudio},
			request: ratingRequest("google", "veo-3", pricing.Attributes{"audio": pricing.BooleanAttribute(true)}, usage(pricing.MeterOutputSeconds, 8_000_000)),
			want:    costed(3_200_000_000, pricedLine(pricing.MeterOutputSeconds, 8_000_000, perUnit(400_000_000), 3_200_000_000, 2)),
		},
		{
			name:    "boolean condition does not match a string attribute",
			rules:   []pricing.Rule{veoSilent, veoWithAudio},
			request: ratingRequest("google", "veo-3", pricing.Attributes{"audio": pricing.StringAttribute("true")}, usage(pricing.MeterOutputSeconds, 8_000_000)),
			want:    uncosted(missingLine(pricing.MeterOutputSeconds, 8_000_000)),
		},
		{
			name: "most condition keys wins",
			rules: []pricing.Rule{
				veoAnyVideo,
				veoFullHD,
				catalogRule(3, "google", "veo-3", pricing.MeterOutputSeconds, pricing.Attributes{
					"resolution": pricing.StringAttribute("1080p"),
					"audio":      pricing.BooleanAttribute(true),
				}, perUnit(300_000_000)),
			},
			request: ratingRequest("google", "veo-3", pricing.Attributes{
				"resolution": pricing.StringAttribute("1080p"),
				"audio":      pricing.BooleanAttribute(true),
				"seed":       pricing.IntegerAttribute(42),
			}, usage(pricing.MeterOutputSeconds, 2_000_000)),
			want: costed(600_000_000, pricedLine(pricing.MeterOutputSeconds, 2_000_000, perUnit(300_000_000), 600_000_000, 3)),
		},
		{
			name:    "rule with resolution does not match a request without resolution",
			rules:   []pricing.Rule{veoFullHD},
			request: ratingRequest("google", "veo-3", pricing.Attributes{"audio": pricing.BooleanAttribute(true)}, usage(pricing.MeterOutputSeconds, 2_000_000)),
			want:    uncosted(missingLine(pricing.MeterOutputSeconds, 2_000_000)),
		},
		{
			name:    "unconditional rule prices a request the specific rule does not match",
			rules:   []pricing.Rule{veoAnyVideo, veoFullHD},
			request: ratingRequest("google", "veo-3", pricing.Attributes{"resolution": pricing.StringAttribute("720p")}, usage(pricing.MeterOutputSeconds, 2_000_000)),
			want:    costed(200_000_000, pricedLine(pricing.MeterOutputSeconds, 2_000_000, perUnit(100_000_000), 200_000_000, 1)),
		},
		{
			name:    "alias resolves to the catalog model",
			rules:   []pricing.Rule{gptInput},
			aliases: []pricing.ModelAlias{{Provider: "openai", Alias: "gpt-4o-mini-2024-07-18", Model: "gpt-4o-mini"}},
			request: ratingRequest("openai", "gpt-4o-mini-2024-07-18", nil, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    costed(150_000, pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(150_000_000), 150_000, 1)),
		},
		{
			name:    "alias of another provider does not apply",
			rules:   []pricing.Rule{gptInput},
			aliases: []pricing.ModelAlias{{Provider: "azure", Alias: "gpt-4o-mini-2024-07-18", Model: "gpt-4o-mini"}},
			request: ratingRequest("openai", "gpt-4o-mini-2024-07-18", nil, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    uncosted(missingLine(pricing.MeterInputTokens, 1_000_000_000)),
		},
		{
			name: "curated beats litellm with equal condition counts",
			rules: []pricing.Rule{
				fromLiteLLM(catalogRule(1, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(200_000_000))),
				catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(150_000_000)),
			},
			request: ratingRequest("openai", "gpt-4o-mini", nil, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    costed(150_000, pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(150_000_000), 150_000, 2)),
		},
		{
			name: "more condition keys beat the curated source",
			rules: []pricing.Rule{
				fromLiteLLM(catalogRule(1, "openai", "gpt-4o-mini", pricing.MeterInputTokens, pricing.Attributes{"service_tier": pricing.StringAttribute("batch")}, perMillionTokens(75_000_000))),
				catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(150_000_000)),
			},
			request: ratingRequest("openai", "gpt-4o-mini", pricing.Attributes{"service_tier": pricing.StringAttribute("batch")}, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    costed(75_000, pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(75_000_000), 75_000, 1)),
		},
		{
			name: "latest effective from wins a tie",
			rules: []pricing.Rule{
				catalogRule(1, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(200_000_000)),
				effective(catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(150_000_000)), summerStart, nil),
			},
			request: ratingRequest("openai", "gpt-4o-mini", nil, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    costed(150_000, pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(150_000_000), 150_000, 2)),
		},
		{
			name: "rule that ended before the request is skipped",
			rules: []pricing.Rule{
				effective(gptInput, catalogStart, &augustStart),
				fromLiteLLM(catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(200_000_000))),
			},
			request: ratingRequest("openai", "gpt-4o-mini", nil, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    costed(200_000, pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(200_000_000), 200_000, 2)),
		},
		{
			name: "rule ending at the request time is skipped",
			rules: []pricing.Rule{
				effective(gptInput, catalogStart, &requestTime),
				fromLiteLLM(catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(200_000_000))),
			},
			request: ratingRequest("openai", "gpt-4o-mini", nil, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    costed(200_000, pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(200_000_000), 200_000, 2)),
		},
		{
			name: "rule starting after the request is skipped",
			rules: []pricing.Rule{
				effective(gptInput, octoberStart, nil),
				fromLiteLLM(catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(200_000_000))),
			},
			request: ratingRequest("openai", "gpt-4o-mini", nil, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    costed(200_000, pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(200_000_000), 200_000, 2)),
		},
		{
			name: "rule starting at the request time applies",
			rules: []pricing.Rule{
				effective(gptInput, requestTime, nil),
				fromLiteLLM(catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(200_000_000))),
			},
			request: ratingRequest("openai", "gpt-4o-mini", nil, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    costed(150_000, pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(150_000_000), 150_000, 1)),
		},
		{
			name:      "adjustment override replaces the catalog price",
			rules:     []pricing.Rule{catalogRule(1, "google", "veo-3", pricing.MeterOutputSeconds, fullHD(), perUnit(400_000_000))},
			overrides: []pricing.Override{override(11, "google", "veo-3", pricing.MeterOutputSeconds, fullHD(), perUnit(300_000_000))},
			request:   ratingRequest("google", "veo-3", fullHD(), usage(pricing.MeterOutputSeconds, 8_000_000)),
			want:      costed(2_400_000_000, overrideLine(pricing.MeterOutputSeconds, 8_000_000, perUnit(300_000_000), 2_400_000_000, 11)),
		},
		{
			name: "adjustment keeps the rule billing increment and minimum charge",
			rules: []pricing.Rule{
				withMinimumCharge(withBillingIncrement(veoAnyVideo, 1_000_000), 1_000_000_000),
			},
			overrides: []pricing.Override{override(11, "google", "veo-3", pricing.MeterOutputSeconds, nil, perUnit(300_000_000))},
			request:   ratingRequest("google", "veo-3", nil, usage(pricing.MeterOutputSeconds, 2_500_000)),
			want:      costed(1_000_000_000, overrideLine(pricing.MeterOutputSeconds, 3_000_000, perUnit(300_000_000), 1_000_000_000, 11)),
		},
		{
			name: "adjustment billing increment and minimum charge replace the rule ones",
			rules: []pricing.Rule{
				withMinimumCharge(withBillingIncrement(veoAnyVideo, 1_000_000), 1_000_000_000),
			},
			overrides: []pricing.Override{{
				ID:               testID(11),
				Environment:      httpapi.EnvironmentLive,
				Provider:         "google",
				Model:            "veo-3",
				Meter:            pricing.MeterOutputSeconds,
				UnitPrice:        perUnit(300_000_000),
				MinimumCharge:    new(money.Amount(500_000_000)),
				BillingIncrement: new(money.Quantity(5_000_000)),
				EffectiveFrom:    catalogStart,
			}},
			request: ratingRequest("google", "veo-3", nil, usage(pricing.MeterOutputSeconds, 2_500_000)),
			want:    costed(1_500_000_000, overrideLine(pricing.MeterOutputSeconds, 5_000_000, perUnit(300_000_000), 1_500_000_000, 11)),
		},
		{
			name:      "ended adjustment leaves the catalog price",
			rules:     []pricing.Rule{catalogRule(1, "google", "veo-3", pricing.MeterOutputSeconds, fullHD(), perUnit(400_000_000))},
			overrides: []pricing.Override{endedAdjustment},
			request:   ratingRequest("google", "veo-3", fullHD(), usage(pricing.MeterOutputSeconds, 8_000_000)),
			want:      costed(3_200_000_000, pricedLine(pricing.MeterOutputSeconds, 8_000_000, perUnit(400_000_000), 3_200_000_000, 1)),
		},
		{
			name:      "adjustment leaves rules with other conditions alone",
			rules:     []pricing.Rule{veoAnyVideo, veoFullHD},
			overrides: []pricing.Override{override(11, "google", "veo-3", pricing.MeterOutputSeconds, nil, perUnit(50_000_000))},
			request:   ratingRequest("google", "veo-3", fullHD(), usage(pricing.MeterOutputSeconds, 2_000_000)),
			want:      costed(400_000_000, pricedLine(pricing.MeterOutputSeconds, 2_000_000, perUnit(200_000_000), 400_000_000, 2)),
		},
		{
			name:      "adjustment prices the rule with its conditions",
			rules:     []pricing.Rule{veoAnyVideo, veoFullHD},
			overrides: []pricing.Override{override(11, "google", "veo-3", pricing.MeterOutputSeconds, nil, perUnit(50_000_000))},
			request:   ratingRequest("google", "veo-3", nil, usage(pricing.MeterOutputSeconds, 2_000_000)),
			want:      costed(100_000_000, overrideLine(pricing.MeterOutputSeconds, 2_000_000, perUnit(50_000_000), 100_000_000, 11)),
		},
		{
			name:      "override equal to a rule closed before the request prices as a standalone",
			rules:     []pricing.Rule{veoAnyVideo, effective(veoFullHD, catalogStart, &augustStart)},
			overrides: []pricing.Override{override(11, "google", "veo-3", pricing.MeterOutputSeconds, fullHD(), perUnit(300_000_000))},
			request:   ratingRequest("google", "veo-3", fullHD(), usage(pricing.MeterOutputSeconds, 8_000_000)),
			want:      costed(2_400_000_000, overrideLine(pricing.MeterOutputSeconds, 8_000_000, perUnit(300_000_000), 2_400_000_000, 11)),
		},
		{
			name:      "override equal to a rule starting after the request prices as a standalone",
			rules:     []pricing.Rule{veoAnyVideo, effective(veoFullHD, octoberStart, nil)},
			overrides: []pricing.Override{override(11, "google", "veo-3", pricing.MeterOutputSeconds, fullHD(), perUnit(300_000_000))},
			request:   ratingRequest("google", "veo-3", fullHD(), usage(pricing.MeterOutputSeconds, 8_000_000)),
			want:      costed(2_400_000_000, overrideLine(pricing.MeterOutputSeconds, 8_000_000, perUnit(300_000_000), 2_400_000_000, 11)),
		},
		{
			name: "override adjusts the rule that replaced a closed one",
			rules: []pricing.Rule{
				effective(veoFullHD, catalogStart, &augustStart),
				effective(catalogRule(3, "google", "veo-3", pricing.MeterOutputSeconds, fullHD(), perUnit(250_000_000)), augustStart, nil),
			},
			overrides: []pricing.Override{override(11, "google", "veo-3", pricing.MeterOutputSeconds, fullHD(), perUnit(300_000_000))},
			request:   ratingRequest("google", "veo-3", fullHD(), usage(pricing.MeterOutputSeconds, 8_000_000)),
			want:      costed(2_400_000_000, overrideLine(pricing.MeterOutputSeconds, 8_000_000, perUnit(300_000_000), 2_400_000_000, 11)),
		},
		{
			name:      "standalone override prices a model the catalog lacks",
			rules:     []pricing.Rule{veoAnyVideo},
			overrides: []pricing.Override{override(11, "acme", "acme-video-1", pricing.MeterOutputSeconds, nil, perUnit(250_000_000))},
			request:   ratingRequest("acme", "acme-video-1", nil, usage(pricing.MeterOutputSeconds, 4_000_000)),
			want:      costed(1_000_000_000, overrideLine(pricing.MeterOutputSeconds, 4_000_000, perUnit(250_000_000), 1_000_000_000, 11)),
		},
		{
			name:      "standalone override is matched before catalog rules",
			rules:     []pricing.Rule{veoFullHD},
			overrides: []pricing.Override{override(11, "google", "veo-3", pricing.MeterOutputSeconds, nil, perUnit(250_000_000))},
			request:   ratingRequest("google", "veo-3", fullHD(), usage(pricing.MeterOutputSeconds, 4_000_000)),
			want:      costed(1_000_000_000, overrideLine(pricing.MeterOutputSeconds, 4_000_000, perUnit(250_000_000), 1_000_000_000, 11)),
		},
		{
			name: "standalone override with the most condition keys wins",
			overrides: []pricing.Override{
				override(11, "acme", "acme-video-1", pricing.MeterOutputSeconds, nil, perUnit(250_000_000)),
				override(12, "acme", "acme-video-1", pricing.MeterOutputSeconds, fullHD(), perUnit(350_000_000)),
			},
			request: ratingRequest("acme", "acme-video-1", fullHD(), usage(pricing.MeterOutputSeconds, 4_000_000)),
			want:    costed(1_400_000_000, overrideLine(pricing.MeterOutputSeconds, 4_000_000, perUnit(350_000_000), 1_400_000_000, 12)),
		},
		{
			name: "native unit override prices 20 credits at 0.0083 USD per credit",
			overrides: []pricing.Override{{
				ID:            testID(11),
				Environment:   httpapi.EnvironmentLive,
				Provider:      "kling",
				Model:         "kling-image-v2",
				Meter:         pricing.MeterImages,
				UnitPrice:     money.UnitPrice{Nanos: 20_000_000_000, UnitQuantity: 1},
				NativeUnit:    &pricing.NativeUnit{Label: "credits", Price: 8_300_000},
				EffectiveFrom: catalogStart,
			}},
			request: ratingRequest("kling", "kling-image-v2", nil, usage(pricing.MeterImages, 3_000_000)),
			want:    costed(498_000_000, overrideLine(pricing.MeterImages, 3_000_000, perUnit(166_000_000), 498_000_000, 11)),
		},
		{
			name:    "billing increment of 1 second bills 8.2 seconds as 9",
			rules:   []pricing.Rule{withBillingIncrement(catalogRule(1, "google", "veo-3", pricing.MeterOutputSeconds, nil, perUnit(400_000_000)), 1_000_000)},
			request: ratingRequest("google", "veo-3", nil, usage(pricing.MeterOutputSeconds, 8_200_000)),
			want:    costed(3_600_000_000, pricedLine(pricing.MeterOutputSeconds, 9_000_000, perUnit(400_000_000), 3_600_000_000, 1)),
		},
		{
			name:    "minimum charge applies to a small usage",
			rules:   []pricing.Rule{withMinimumCharge(catalogRule(1, "elevenlabs", "eleven-multilingual-v2", pricing.MeterCharacters, nil, perMillionTokens(30_000_000_000)), 10_000_000)},
			request: ratingRequest("elevenlabs", "eleven-multilingual-v2", nil, usage(pricing.MeterCharacters, 100_000_000)),
			want:    costed(10_000_000, pricedLine(pricing.MeterCharacters, 100_000_000, perMillionTokens(30_000_000_000), 10_000_000, 1)),
		},
		{
			name:    "minimum charge leaves a larger cost unchanged",
			rules:   []pricing.Rule{withMinimumCharge(catalogRule(1, "elevenlabs", "eleven-multilingual-v2", pricing.MeterCharacters, nil, perMillionTokens(30_000_000_000)), 10_000_000)},
			request: ratingRequest("elevenlabs", "eleven-multilingual-v2", nil, usage(pricing.MeterCharacters, 1_000_000_000)),
			want:    costed(30_000_000, pricedLine(pricing.MeterCharacters, 1_000_000_000, perMillionTokens(30_000_000_000), 30_000_000, 1)),
		},
		{
			name: "zero billed quantity skips the minimum charge",
			rules: []pricing.Rule{
				withMinimumCharge(withBillingIncrement(catalogRule(1, "elevenlabs", "eleven-multilingual-v2", pricing.MeterCharacters, nil, perMillionTokens(30_000_000_000)), 1_000_000_000), 10_000_000),
			},
			request: ratingRequest("elevenlabs", "eleven-multilingual-v2", nil, usage(pricing.MeterCharacters, 0)),
			want:    costed(0, pricedLine(pricing.MeterCharacters, 0, perMillionTokens(30_000_000_000), 0, 1)),
		},
		{
			name:  "missing meter makes the request uncosted with the other lines priced",
			rules: []pricing.Rule{gptInput, gptOutput},
			request: ratingRequest("openai", "gpt-4o-mini", nil, map[pricing.Meter]money.Quantity{
				pricing.MeterInputTokens:       1_000_000_000,
				pricing.MeterOutputTokens:      500_000_000,
				pricing.MeterCachedInputTokens: 200_000_000,
			}),
			want: uncosted(
				missingLine(pricing.MeterCachedInputTokens, 200_000_000),
				pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(150_000_000), 150_000, 1),
				pricedLine(pricing.MeterOutputTokens, 500_000_000, perMillionTokens(600_000_000), 300_000, 2),
			),
		},
		{
			name:  "zero quantity of an unpriced meter is skipped",
			rules: []pricing.Rule{gptInput},
			request: ratingRequest("openai", "gpt-4o-mini", nil, map[pricing.Meter]money.Quantity{
				pricing.MeterInputTokens:       1_000_000_000,
				pricing.MeterCachedInputTokens: 0,
			}),
			want: costed(150_000, pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(150_000_000), 150_000, 1)),
		},
		{
			name:    "zero quantity of an unknown model is costed at zero",
			rules:   []pricing.Rule{gptInput},
			request: ratingRequest("openai", "gpt-unknown", nil, usage(pricing.MeterInputTokens, 0)),
			want:    costed(0, []pricing.RatedLine{}...),
		},
		{
			name:    "unknown model is uncosted",
			rules:   []pricing.Rule{gptInput},
			request: ratingRequest("openai", "gpt-unknown", nil, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    uncosted(missingLine(pricing.MeterInputTokens, 1_000_000_000)),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ruleSet, err := pricing.NewRuleSet(test.rules, test.overrides, test.aliases)
			if err != nil {
				t.Fatalf("NewRuleSet error = %v", err)
			}
			got, err := pricing.Rate(test.request, ruleSet)
			if err != nil {
				t.Fatalf("Rate error = %v", err)
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Rate mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRateOverflow(t *testing.T) {
	rule := catalogRule(1, "openai", "gpt-4o-mini-search", pricing.MeterSearchRequests, nil, perUnit(999_999_999_000_000_000))
	ruleSet, err := pricing.NewRuleSet([]pricing.Rule{rule}, nil, nil)
	if err != nil {
		t.Fatalf("NewRuleSet error = %v", err)
	}
	request := ratingRequest("openai", "gpt-4o-mini-search", nil, usage(pricing.MeterSearchRequests, 999_999_999_000_000))
	if _, err := pricing.Rate(request, ruleSet); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("Rate error = %v, want %v", err, money.ErrOverflow)
	}
}

func TestKeyPrices(t *testing.T) {
	tests := []struct {
		name      string
		rules     []pricing.Rule
		overrides []pricing.Override
		aliases   []pricing.ModelAlias
		model     string
		defaults  pricing.Attributes
		want      []pricing.KeyPrice
	}{
		{
			name: "token model returns input and output prices",
			rules: []pricing.Rule{
				catalogRule(1, "openai", "gpt-4o-mini", pricing.MeterOutputTokens, nil, perMillionTokens(600_000_000)),
				catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(150_000_000)),
				fromLiteLLM(catalogRule(3, "openai", "gpt-4o-mini", pricing.MeterInputTokens, pricing.Attributes{"service_tier": pricing.StringAttribute("batch")}, perMillionTokens(75_000_000))),
			},
			model:    "gpt-4o-mini",
			defaults: pricing.Attributes{"service_tier": pricing.StringAttribute("standard")},
			want: []pricing.KeyPrice{
				{Meter: pricing.MeterInputTokens, UnitPrice: perMillionTokens(150_000_000)},
				{Meter: pricing.MeterOutputTokens, UnitPrice: perMillionTokens(600_000_000)},
			},
		},
		{
			name: "rules are chosen by the default attributes",
			rules: []pricing.Rule{
				catalogRule(1, "openai", "sora-2", pricing.MeterOutputSeconds, pricing.Attributes{"resolution": pricing.StringAttribute("720p")}, perUnit(100_000_000)),
				catalogRule(2, "openai", "sora-2", pricing.MeterOutputSeconds, pricing.Attributes{"resolution": pricing.StringAttribute("1080p")}, perUnit(300_000_000)),
			},
			model:    "sora-2",
			defaults: pricing.Attributes{"resolution": pricing.StringAttribute("720p")},
			want:     []pricing.KeyPrice{{Meter: pricing.MeterOutputSeconds, UnitPrice: perUnit(100_000_000)}},
		},
		{
			name: "meter whose rules the defaults do not match is left out",
			rules: []pricing.Rule{
				catalogRule(1, "openai", "sora-2", pricing.MeterOutputSeconds, pricing.Attributes{"resolution": pricing.StringAttribute("1080p")}, perUnit(300_000_000)),
			},
			model:    "sora-2",
			defaults: pricing.Attributes{"resolution": pricing.StringAttribute("720p")},
			want:     []pricing.KeyPrice{},
		},
		{
			name: "rule in effect at the given time is chosen",
			rules: []pricing.Rule{
				effective(catalogRule(1, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(200_000_000)), catalogStart, &augustStart),
				effective(catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(150_000_000)), augustStart, nil),
			},
			model: "gpt-4o-mini",
			want:  []pricing.KeyPrice{{Meter: pricing.MeterInputTokens, UnitPrice: perMillionTokens(150_000_000)}},
		},
		{
			name:  "alias, adjustment and standalone override",
			rules: []pricing.Rule{catalogRule(1, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(150_000_000))},
			overrides: []pricing.Override{
				override(11, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(100_000_000)),
				override(12, "openai", "gpt-4o-mini", pricing.MeterOutputTokens, nil, perMillionTokens(500_000_000)),
			},
			aliases: []pricing.ModelAlias{{Provider: "openai", Alias: "gpt-4o-mini-2024-07-18", Model: "gpt-4o-mini"}},
			model:   "gpt-4o-mini-2024-07-18",
			want: []pricing.KeyPrice{
				{Meter: pricing.MeterInputTokens, UnitPrice: perMillionTokens(100_000_000)},
				{Meter: pricing.MeterOutputTokens, UnitPrice: perMillionTokens(500_000_000)},
			},
		},
		{
			name:  "unknown model has no key prices",
			rules: []pricing.Rule{catalogRule(1, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(150_000_000))},
			model: "gpt-unknown",
			want:  []pricing.KeyPrice{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ruleSet, err := pricing.NewRuleSet(test.rules, test.overrides, test.aliases)
			if err != nil {
				t.Fatalf("NewRuleSet error = %v", err)
			}
			got := pricing.KeyPrices("openai", test.model, test.defaults, ruleSet, requestTime)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("KeyPrices mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNewRuleSetRejectsInvalidInput(t *testing.T) {
	gptInput := catalogRule(1, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(150_000_000))
	unknownSource := gptInput
	unknownSource.Source = "manual"
	unknownStatus := gptInput
	unknownStatus.Status = "retired"
	liveOverride := override(11, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(100_000_000))
	testOverride := override(12, "openai", "gpt-4o-mini", pricing.MeterOutputTokens, nil, perMillionTokens(500_000_000))
	testOverride.Environment = httpapi.EnvironmentTest
	overflowingOverride := override(11, "kling", "kling-v2", pricing.MeterOutputSeconds, nil, perUnit(999_999_999_000_000_000))
	overflowingOverride.NativeUnit = &pricing.NativeUnit{Label: "credits", Price: 999_999_999_000_000_000}

	tests := []struct {
		name      string
		rules     []pricing.Rule
		overrides []pricing.Override
		aliases   []pricing.ModelAlias
		wantError error
	}{
		{name: "unknown rule source", rules: []pricing.Rule{unknownSource}, wantError: pricing.ErrInvalidRuleSet},
		{name: "unknown rule status", rules: []pricing.Rule{unknownStatus}, wantError: pricing.ErrInvalidRuleSet},
		{name: "overrides of two environments", overrides: []pricing.Override{liveOverride, testOverride}, wantError: pricing.ErrInvalidRuleSet},
		{
			name: "alias defined twice",
			aliases: []pricing.ModelAlias{
				{Provider: "openai", Alias: "gpt-4o-mini-2024-07-18", Model: "gpt-4o-mini"},
				{Provider: "openai", Alias: "gpt-4o-mini-2024-07-18", Model: "gpt-4o"},
			},
			wantError: pricing.ErrInvalidRuleSet,
		},
		{name: "native unit price overflow", overrides: []pricing.Override{overflowingOverride}, wantError: money.ErrOverflow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := pricing.NewRuleSet(test.rules, test.overrides, test.aliases)
			if !errors.Is(err, test.wantError) {
				t.Errorf("NewRuleSet error = %v, want %v", err, test.wantError)
			}
		})
	}
}

func TestOverrideEffectiveUnitPrice(t *testing.T) {
	tests := []struct {
		name       string
		unitPrice  money.UnitPrice
		nativeUnit *pricing.NativeUnit
		want       money.UnitPrice
		wantError  error
	}{
		{
			name:      "USD price is unchanged",
			unitPrice: perUnit(400_000_000),
			want:      perUnit(400_000_000),
		},
		{
			name:       "20 credits at 0.0083 USD per credit",
			unitPrice:  money.UnitPrice{Nanos: 20_000_000_000, UnitQuantity: 5},
			nativeUnit: &pricing.NativeUnit{Label: "credits", Price: 8_300_000},
			want:       money.UnitPrice{Nanos: 166_000_000, UnitQuantity: 5},
		},
		{
			name:       "half a nano rounds up",
			unitPrice:  perUnit(1),
			nativeUnit: &pricing.NativeUnit{Label: "credits", Price: 500_000_000},
			want:       perUnit(1),
		},
		{
			name:       "just below half a nano rounds down",
			unitPrice:  perUnit(1),
			nativeUnit: &pricing.NativeUnit{Label: "credits", Price: 499_999_999},
			want:       perUnit(0),
		},
		{
			name:       "result above int64",
			unitPrice:  perUnit(999_999_999_000_000_000),
			nativeUnit: &pricing.NativeUnit{Label: "credits", Price: 999_999_999_000_000_000},
			wantError:  money.ErrOverflow,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			priced := override(11, "kling", "kling-v2", pricing.MeterOutputSeconds, nil, test.unitPrice)
			priced.NativeUnit = test.nativeUnit
			got, err := priced.EffectiveUnitPrice()
			if !errors.Is(err, test.wantError) {
				t.Fatalf("EffectiveUnitPrice error = %v, want %v", err, test.wantError)
			}
			if got != test.want {
				t.Errorf("EffectiveUnitPrice = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestParseMeter(t *testing.T) {
	meters := []pricing.Meter{
		pricing.MeterInputTokens,
		pricing.MeterCachedInputTokens,
		pricing.MeterCacheWriteInputTokens,
		pricing.MeterOutputTokens,
		pricing.MeterReasoningTokens,
		pricing.MeterInputAudioTokens,
		pricing.MeterOutputAudioTokens,
		pricing.MeterInputSeconds,
		pricing.MeterOutputSeconds,
		pricing.MeterGPUSeconds,
		pricing.MeterCharacters,
		pricing.MeterImages,
		pricing.MeterMegapixels,
		pricing.MeterAudioMinutes,
		pricing.MeterRequests,
		pricing.MeterSearchRequests,
	}
	names := map[string]bool{}
	for _, meter := range meters {
		got, err := pricing.ParseMeter(string(meter))
		if err != nil || got != meter {
			t.Errorf("ParseMeter(%q) = %q, %v, want %q, nil", meter, got, err, meter)
		}
		names[string(meter)] = true
	}
	if len(names) != 16 {
		t.Errorf("distinct meters = %d, want 16", len(names))
	}
	for _, name := range []string{"", "input_token", "Input_Tokens", "seconds"} {
		if _, err := pricing.ParseMeter(name); !errors.Is(err, pricing.ErrUnknownMeter) {
			t.Errorf("ParseMeter(%q) error = %v, want %v", name, err, pricing.ErrUnknownMeter)
		}
	}
}

func testID(number byte) uuid.UUID {
	return uuid.UUID{15: number}
}

func perMillionTokens(nanos money.Amount) money.UnitPrice {
	return money.UnitPrice{Nanos: nanos, UnitQuantity: 1_000_000}
}

func perUnit(nanos money.Amount) money.UnitPrice {
	return money.UnitPrice{Nanos: nanos, UnitQuantity: 1}
}

func fullHD() pricing.Attributes {
	return pricing.Attributes{"resolution": pricing.StringAttribute("1080p")}
}

func catalogRule(number byte, provider, model string, meter pricing.Meter, conditions pricing.Attributes, price money.UnitPrice) pricing.Rule {
	return pricing.Rule{
		ID:            testID(number),
		Provider:      provider,
		Model:         model,
		Meter:         meter,
		Conditions:    conditions,
		UnitPrice:     price,
		EffectiveFrom: catalogStart,
		Status:        pricing.RuleStatusActive,
		Source:        pricing.RuleSourceCurated,
	}
}

func fromLiteLLM(rule pricing.Rule) pricing.Rule {
	rule.Source = pricing.RuleSourceLiteLLM
	return rule
}

func effective(rule pricing.Rule, from time.Time, to *time.Time) pricing.Rule {
	rule.EffectiveFrom = from
	rule.EffectiveTo = to
	return rule
}

func withBillingIncrement(rule pricing.Rule, increment money.Quantity) pricing.Rule {
	rule.BillingIncrement = &increment
	return rule
}

func withMinimumCharge(rule pricing.Rule, minimum money.Amount) pricing.Rule {
	rule.MinimumCharge = &minimum
	return rule
}

func override(number byte, provider, model string, meter pricing.Meter, conditions pricing.Attributes, price money.UnitPrice) pricing.Override {
	return pricing.Override{
		ID:            testID(number),
		Environment:   httpapi.EnvironmentLive,
		Provider:      provider,
		Model:         model,
		Meter:         meter,
		Conditions:    conditions,
		UnitPrice:     price,
		EffectiveFrom: catalogStart,
	}
}

func usage(meter pricing.Meter, quantity money.Quantity) map[pricing.Meter]money.Quantity {
	return map[pricing.Meter]money.Quantity{meter: quantity}
}

func ratingRequest(provider, model string, attributes pricing.Attributes, usage map[pricing.Meter]money.Quantity) pricing.RatingRequest {
	return pricing.RatingRequest{Provider: provider, Model: model, Attributes: attributes, Usage: usage, OccurredAt: requestTime}
}

func pricedLine(meter pricing.Meter, quantity money.Quantity, price money.UnitPrice, cost money.Amount, ruleNumber byte) pricing.RatedLine {
	return pricing.RatedLine{Meter: meter, Quantity: quantity, UnitPrice: price, Cost: cost, RuleID: new(testID(ruleNumber))}
}

func overrideLine(meter pricing.Meter, quantity money.Quantity, price money.UnitPrice, cost money.Amount, overrideNumber byte) pricing.RatedLine {
	return pricing.RatedLine{Meter: meter, Quantity: quantity, UnitPrice: price, Cost: cost, OverrideID: new(testID(overrideNumber))}
}

func missingLine(meter pricing.Meter, quantity money.Quantity) pricing.RatedLine {
	return pricing.RatedLine{Meter: meter, Quantity: quantity, Missing: true}
}

func costed(cost money.Amount, lines ...pricing.RatedLine) pricing.RatedRequest {
	return pricing.RatedRequest{CostStatus: pricing.CostStatusCosted, Cost: &cost, Lines: lines}
}

func uncosted(lines ...pricing.RatedLine) pricing.RatedRequest {
	return pricing.RatedRequest{CostStatus: pricing.CostStatusUncosted, Lines: lines}
}
