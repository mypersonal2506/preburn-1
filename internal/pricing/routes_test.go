package pricing_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
)

const (
	veoAdjustmentBody = `{"provider":"fal_ai","model":"fal-ai/veo3.1/fast","meter":"output_seconds","conditions":{},"unit_price":"0.12","unit_quantity":1}`
	standaloneBody    = `{"provider":"acme","model":"acme-video-1","meter":"output_seconds","unit_price":"0.50","unit_quantity":1}`
)

func TestMetersRouteListsMeterVocabulary(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	page := decodeBody(t, harness.memberRequest(t, http.MethodGet, metersPath, httpapi.EnvironmentTest, ""))

	items := pageItems(t, page)
	if len(items) != 16 {
		t.Fatalf("meters = %d, want 16", len(items))
	}
	want := map[string]any{"meter": "audio_minutes", "unit": "minute", "description": "Minutes of audio processed or generated."}
	if diff := cmp.Diff(want, items[0]); diff != "" {
		t.Errorf("first meter mismatch (-want +got):\n%s", diff)
	}
	if page["next_cursor"] != nil {
		t.Errorf("next_cursor = %v, want null", page["next_cursor"])
	}
}

func TestModelsRouteFiltersAndPages(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	tests := []struct {
		name  string
		query string
		want  [][2]string
	}{
		{name: "provider", query: "provider=runwayml", want: [][2]string{{"runwayml", "gen4.5"}, {"runwayml", "gen4_turbo"}, {"runwayml", "veo3.1_fast"}}},
		{
			name:  "search in model names in any letter case",
			query: "search=FLUX",
			want:  [][2]string{{"deepgram", "flux-general-en"}, {"fal_ai", "fal-ai/flux/dev"}, {"fal_ai", "fal-ai/flux/schnell"}},
		},
		{name: "search in display names", query: "search=" + url.QueryEscape("Kling 2.5"), want: [][2]string{{"fal_ai", "fal-ai/kling-video/v2.5-turbo/pro/text-to-video"}}},
		{name: "meter", query: "meter=megapixels", want: [][2]string{{"fal_ai", "fal-ai/flux/dev"}, {"fal_ai", "fal-ai/flux/schnell"}}},
		{name: "provider and meter", query: "provider=openai&meter=images", want: [][2]string{{"openai", "image-example"}}},
		{name: "no match", query: "search=missing-model", want: [][2]string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?"+test.query, httpapi.EnvironmentTest, ""))

			if diff := cmp.Diff(test.want, modelKeys(t, page)); diff != "" {
				t.Errorf("models mismatch (-want +got):\n%s", diff)
			}
		})
	}

	firstPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?provider=fal_ai&limit=4", httpapi.EnvironmentTest, ""))
	nextCursor, isString := firstPage["next_cursor"].(string)
	if !isString {
		t.Fatalf("first page next_cursor = %v, want a cursor", firstPage["next_cursor"])
	}
	secondPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?provider=fal_ai&limit=4&cursor="+nextCursor, httpapi.EnvironmentTest, ""))
	assertProblem(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?provider=fal_ai&limit=4&cursor="+nextCursor, httpapi.EnvironmentLive, ""), http.StatusUnprocessableEntity, "invalid_cursor")
	wantPages := [][2]string{
		{"fal_ai", "fal-ai/flux/dev"},
		{"fal_ai", "fal-ai/flux/schnell"},
		{"fal_ai", "fal-ai/kling-video/v2.5-turbo/pro/text-to-video"},
		{"fal_ai", "fal-ai/veo3.1/fast"},
		{"fal_ai", "fal-ai/veo3.1/lite"},
		{"fal_ai", "fal-ai/wan/v2.2-a14b/text-to-video"},
	}
	if diff := cmp.Diff(wantPages, append(modelKeys(t, firstPage), modelKeys(t, secondPage)...)); diff != "" {
		t.Errorf("paged models mismatch (-want +got):\n%s", diff)
	}
	if secondPage["next_cursor"] != nil {
		t.Errorf("second page next_cursor = %v, want null", secondPage["next_cursor"])
	}
}

func TestModelsRouteReturnsDisplayNamesAndKeyPrices(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	veoPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?provider=fal_ai&search=veo3.1/fast", httpapi.EnvironmentTest, ""))
	textPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?provider=openai&search=gpt-example", httpapi.EnvironmentTest, ""))

	wantVeo := []map[string]any{{
		"provider":     "fal_ai",
		"model":        "fal-ai/veo3.1/fast",
		"display_name": "Veo 3.1 Fast",
		"status":       "active",
		"key_prices":   []any{keyPrice("output_seconds", "0.150000000", 1)},
	}}
	if diff := cmp.Diff(wantVeo, pageItems(t, veoPage)); diff != "" {
		t.Errorf("veo model mismatch (-want +got):\n%s", diff)
	}
	wantText := []map[string]any{{
		"provider":     "openai",
		"model":        "gpt-example",
		"display_name": nil,
		"status":       "active",
		"key_prices": []any{
			keyPrice("cache_write_input_tokens", "0.187500000", 1_000_000),
			keyPrice("cached_input_tokens", "0.075000000", 1_000_000),
			keyPrice("input_tokens", "0.150000000", 1_000_000),
			keyPrice("output_tokens", "0.600000000", 1_000_000),
			keyPrice("search_requests", "0.030000000", 1),
		},
	}}
	if diff := cmp.Diff(wantText, pageItems(t, textPage)); diff != "" {
		t.Errorf("text model mismatch (-want +got):\n%s", diff)
	}
}

func TestModelsRouteKeyPricesFollowOverrides(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	harness.createOverride(t, httpapi.EnvironmentTest, veoAdjustmentBody)

	testItems := pageItems(t, decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?search=veo3.1/fast&provider=fal_ai", httpapi.EnvironmentTest, "")))
	liveItems := pageItems(t, decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?search=veo3.1/fast&provider=fal_ai", httpapi.EnvironmentLive, "")))

	if diff := cmp.Diff([]any{keyPrice("output_seconds", "0.120000000", 1)}, testItems[0]["key_prices"]); diff != "" {
		t.Errorf("test key prices mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{keyPrice("output_seconds", "0.150000000", 1)}, liveItems[0]["key_prices"]); diff != "" {
		t.Errorf("live key prices mismatch (-want +got):\n%s", diff)
	}
}

func TestModelsRouteHidesDeprecatedModels(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	harness.deprecateModel(t, "fal_ai", "fal-ai/flux/dev")

	visible := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?search=flux", httpapi.EnvironmentTest, ""))
	everything := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?search=flux&include_deprecated=true", httpapi.EnvironmentTest, ""))

	wantVisible := [][2]string{{"deepgram", "flux-general-en"}, {"fal_ai", "fal-ai/flux/schnell"}}
	if diff := cmp.Diff(wantVisible, modelKeys(t, visible)); diff != "" {
		t.Errorf("default listing mismatch (-want +got):\n%s", diff)
	}
	items := pageItems(t, everything)
	if len(items) != 3 {
		t.Fatalf("models with deprecated = %d, want 3", len(items))
	}
	deprecated := items[1]
	if deprecated["model"] != "fal-ai/flux/dev" || deprecated["status"] != "deprecated" || !cmp.Equal(deprecated["key_prices"], []any{}) {
		t.Errorf("deprecated model = %v, want fal-ai/flux/dev deprecated without key prices", deprecated)
	}
}

func TestModelsRouteListsStandaloneOverrideModels(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)

	testPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?provider=acme", httpapi.EnvironmentTest, ""))
	livePage := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?provider=acme", httpapi.EnvironmentLive, ""))
	searched := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?search=ACME-VIDEO&meter=output_seconds", httpapi.EnvironmentTest, ""))
	otherMeter := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?provider=acme&meter=images", httpapi.EnvironmentTest, ""))

	want := []map[string]any{{
		"provider":     "acme",
		"model":        "acme-video-1",
		"display_name": nil,
		"status":       "active",
		"key_prices":   []any{keyPrice("output_seconds", "0.500000000", 1)},
	}}
	if diff := cmp.Diff(want, pageItems(t, testPage)); diff != "" {
		t.Errorf("test models mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([][2]string{{"acme", "acme-video-1"}}, modelKeys(t, searched)); diff != "" {
		t.Errorf("searched models mismatch (-want +got):\n%s", diff)
	}
	for name, page := range map[string]map[string]any{"live": livePage, "other meter": otherMeter} {
		if items := pageItems(t, page); len(items) != 0 {
			t.Errorf("%s models = %v, want none", name, items)
		}
	}
	harness.clock.Advance(time.Minute)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, overridePath(created["id"]), httpapi.EnvironmentTest, ""), http.StatusNoContent)
	hidden := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?provider=acme", httpapi.EnvironmentTest, ""))
	ended := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?provider=acme&include_deprecated=true", httpapi.EnvironmentTest, ""))
	if items := pageItems(t, hidden); len(items) != 0 {
		t.Errorf("models after the override ended = %v, want none", items)
	}
	endedItems := pageItems(t, ended)
	if len(endedItems) != 1 || endedItems[0]["status"] != "deprecated" || !cmp.Equal(endedItems[0]["key_prices"], []any{}) {
		t.Errorf("models with deprecated = %v, want acme-video-1 deprecated without key prices", endedItems)
	}
}

func TestQuoteRoutePricesCuratedAndLiteLLMModels(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	veo := harness.quote(t, httpapi.EnvironmentTest, veoQuoteBody)
	text := harness.quote(t, httpapi.EnvironmentTest, `{"provider":"openai","model":"gpt-example","usage":{"input_tokens":"1000","output_tokens":"500"}}`)
	aliased := harness.quote(t, httpapi.EnvironmentTest, `{"provider":"fal_ai","model":"fal-ai/veo3.1/fast/image-to-video","attributes":{"audio":true},"usage":{"output_seconds":"8"}}`)

	ruleID := veoLine(t, veo)["pricing_rule_id"]
	if _, isString := ruleID.(string); !isString {
		t.Fatalf("pricing_rule_id = %v, want a rule id", ruleID)
	}
	wantVeo := map[string]any{
		"cost_status": "costed",
		"cost":        "1.200000000",
		"lines": []any{map[string]any{
			"meter":               "output_seconds",
			"quantity":            "8",
			"unit_price":          "0.150000000",
			"unit_quantity":       float64(1),
			"cost":                "1.200000000",
			"pricing_rule_id":     ruleID,
			"pricing_override_id": nil,
			"missing":             false,
		}},
	}
	if diff := cmp.Diff(wantVeo, veo); diff != "" {
		t.Errorf("veo quote mismatch (-want +got):\n%s", diff)
	}
	if _, err := identifiers.Decode(identifiers.PrefixPricingRule, ruleID.(string)); err != nil {
		t.Errorf("pricing_rule_id %v is not a pricing rule id: %v", ruleID, err)
	}
	assertCost(t, text, "0.000450000")
	assertCost(t, aliased, "1.200000000")
}

func TestQuoteRouteMarksUnpricedMetersMissing(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	quote := harness.quote(t, httpapi.EnvironmentTest, `{"provider":"openai","model":"gpt-example","usage":{"input_tokens":"1000","images":"2"}}`)

	want := map[string]any{
		"cost_status": "uncosted",
		"cost":        nil,
		"lines": []any{
			map[string]any{
				"meter":               "images",
				"quantity":            "2",
				"unit_price":          nil,
				"unit_quantity":       nil,
				"cost":                nil,
				"pricing_rule_id":     nil,
				"pricing_override_id": nil,
				"missing":             true,
			},
			map[string]any{
				"meter":               "input_tokens",
				"quantity":            "1000",
				"unit_price":          "0.150000000",
				"unit_quantity":       float64(1_000_000),
				"cost":                "0.000150000",
				"pricing_rule_id":     quoteLines(t, quote)[1]["pricing_rule_id"],
				"pricing_override_id": nil,
				"missing":             false,
			},
		},
	}
	if diff := cmp.Diff(want, quote); diff != "" {
		t.Errorf("quote mismatch (-want +got):\n%s", diff)
	}
}

func TestAdjustmentOverrideChangesNextQuote(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	assertCost(t, harness.quote(t, httpapi.EnvironmentTest, veoQuoteBody), "1.200000000")

	created := harness.createOverride(t, httpapi.EnvironmentTest, veoAdjustmentBody)

	overrideID, isString := created["id"].(string)
	if !isString {
		t.Fatalf("id = %v, want an override id", created["id"])
	}
	if _, err := identifiers.Decode(identifiers.PrefixPricingOverride, overrideID); err != nil {
		t.Errorf("id %s is not a pricing override id: %v", overrideID, err)
	}
	want := map[string]any{
		"id":                   overrideID,
		"type":                 "adjustment",
		"provider":             "fal_ai",
		"model":                "fal-ai/veo3.1/fast",
		"meter":                "output_seconds",
		"conditions":           map[string]any{},
		"unit_price":           "0.120000000",
		"unit_quantity":        float64(1),
		"native_unit":          nil,
		"native_unit_price":    nil,
		"effective_unit_price": "0.120000000",
		"minimum_charge":       nil,
		"billing_increment":    nil,
		"effective_from":       serviceStart.Format(time.RFC3339),
		"effective_to":         nil,
		"created_at":           serviceStart.Format(time.RFC3339),
		"updated_at":           serviceStart.Format(time.RFC3339),
	}
	if diff := cmp.Diff(want, created); diff != "" {
		t.Errorf("override mismatch (-want +got):\n%s", diff)
	}
	adjusted := harness.quote(t, httpapi.EnvironmentTest, veoQuoteBody)
	assertCost(t, adjusted, "0.960000000")
	line := veoLine(t, adjusted)
	if line["pricing_override_id"] != overrideID || line["pricing_rule_id"] != nil {
		t.Errorf("line rule and override = %v and %v, want override %s only", line["pricing_rule_id"], line["pricing_override_id"], overrideID)
	}
	assertCost(t, harness.quote(t, httpapi.EnvironmentLive, veoQuoteBody), "1.200000000")
}

func TestStandaloneOverridePricesUnknownModel(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	assertCost(t, harness.quote(t, httpapi.EnvironmentTest, unknownModelBody), nil)

	created := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)

	if created["type"] != "standalone" {
		t.Errorf("type = %v, want standalone", created["type"])
	}
	quote := harness.quote(t, httpapi.EnvironmentTest, unknownModelBody)
	assertCost(t, quote, "4.000000000")
	if quote["cost_status"] != "costed" {
		t.Errorf("cost_status = %v, want costed", quote["cost_status"])
	}
}

func TestOverrideTypeMustMatchCatalog(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	tests := []struct {
		name      string
		body      string
		status    int
		code      string
		locations []string
	}{
		{
			name:   "standalone duplicating a catalog rule",
			body:   `{"type":"standalone","provider":"fal_ai","model":"fal-ai/veo3.1/fast","meter":"output_seconds","conditions":{"audio":false},"unit_price":"0.05","unit_quantity":1}`,
			status: http.StatusConflict,
			code:   "pricing_rule_exists_use_adjustment",
		},
		{
			name:   "standalone duplicating a catalog rule through an alias",
			body:   `{"type":"standalone","provider":"fal_ai","model":"fal-ai/veo3.1/fast/image-to-video","meter":"output_seconds","unit_price":"0.05","unit_quantity":1}`,
			status: http.StatusConflict,
			code:   "pricing_rule_exists_use_adjustment",
		},
		{
			name:      "adjustment without a catalog rule",
			body:      `{"type":"adjustment","provider":"acme","model":"acme-video-1","meter":"output_seconds","unit_price":"0.05","unit_quantity":1}`,
			status:    http.StatusUnprocessableEntity,
			code:      "validation_failed",
			locations: []string{"body.type"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, http.MethodPost, overridesPath, httpapi.EnvironmentTest, test.body)

			assertProblem(t, recorder, test.status, test.code, test.locations...)
		})
	}
	everything := decodeBody(t, harness.memberRequest(t, http.MethodGet, overridesPath+"?include_ended=true", httpapi.EnvironmentTest, ""))
	if items := pageItems(t, everything); len(items) != 0 {
		t.Errorf("overrides after rejected creates = %v, want none", items)
	}
	explicit := harness.createOverride(t, httpapi.EnvironmentTest, `{"type":"standalone","provider":"acme","model":"acme-video-1","meter":"output_seconds","unit_price":"0.05","unit_quantity":1}`)
	if explicit["type"] != "standalone" {
		t.Errorf("type = %v, want standalone", explicit["type"])
	}
}

func TestStandaloneOverrideOfDeprecatedModelPricesRequests(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	harness.deprecateModel(t, curatedProvider, veoModel)
	assertCost(t, harness.quote(t, httpapi.EnvironmentTest, veoQuoteBody), nil)

	created := harness.createOverride(t, httpapi.EnvironmentTest, veoAdjustmentBody)

	if created["type"] != "standalone" {
		t.Errorf("type = %v, want standalone", created["type"])
	}
	assertCost(t, harness.quote(t, httpapi.EnvironmentTest, veoQuoteBody), "0.960000000")
}

func TestOverrideCreateResolvesAlias(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	created := harness.createOverride(t, httpapi.EnvironmentTest,
		`{"provider":"fal_ai","model":"fal-ai/veo3.1/fast/image-to-video","meter":"output_seconds","unit_price":"0.12","unit_quantity":1}`)

	if created["model"] != "fal-ai/veo3.1/fast" || created["type"] != "adjustment" {
		t.Errorf("model and type = %v and %v, want fal-ai/veo3.1/fast adjustment", created["model"], created["type"])
	}
	assertCost(t, harness.quote(t, httpapi.EnvironmentTest, veoQuoteBody), "0.960000000")
}

func TestNativeUnitOverridePricesInUSD(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	created := harness.createOverride(t, httpapi.EnvironmentTest,
		`{"provider":"acme","model":"acme-video-1","meter":"output_seconds","unit_price":"20","unit_quantity":1,"native_unit":"credits","native_unit_price":"0.0083"}`)

	want := map[string]any{
		"unit_price":           "20.000000000",
		"native_unit":          "credits",
		"native_unit_price":    "0.008300000",
		"effective_unit_price": "0.166000000",
	}
	got := map[string]any{
		"unit_price":           created["unit_price"],
		"native_unit":          created["native_unit"],
		"native_unit_price":    created["native_unit_price"],
		"effective_unit_price": created["effective_unit_price"],
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("native unit prices mismatch (-want +got):\n%s", diff)
	}
	assertCost(t, harness.quote(t, httpapi.EnvironmentTest, unknownModelBody), "1.328000000")
}

func TestDeleteEndsOverrideAndQuoteReverts(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createOverride(t, httpapi.EnvironmentTest, veoAdjustmentBody)
	assertCost(t, harness.quote(t, httpapi.EnvironmentTest, veoQuoteBody), "0.960000000")
	harness.clock.Advance(time.Minute)
	endedAt := serviceStart.Add(time.Minute)

	recorder := harness.memberRequest(t, http.MethodDelete, overridePath(created["id"]), httpapi.EnvironmentTest, "")

	assertStatus(t, recorder, http.StatusNoContent)
	assertCost(t, harness.quote(t, httpapi.EnvironmentTest, veoQuoteBody), "1.200000000")
	fetched := harness.memberRequest(t, http.MethodGet, overridePath(created["id"]), httpapi.EnvironmentTest, "")
	assertStatus(t, fetched, http.StatusOK)
	if effectiveTo := decodeBody(t, fetched)["effective_to"]; effectiveTo != endedAt.Format(time.RFC3339) {
		t.Errorf("effective_to = %v, want %s", effectiveTo, endedAt.Format(time.RFC3339))
	}
	harness.clock.Advance(time.Minute)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, overridePath(created["id"]), httpapi.EnvironmentTest, ""), http.StatusNoContent)
	if effectiveTo := decodeBody(t, harness.memberRequest(t, http.MethodGet, overridePath(created["id"]), httpapi.EnvironmentTest, ""))["effective_to"]; effectiveTo != endedAt.Format(time.RFC3339) {
		t.Errorf("effective_to after a second delete = %v, want %s", effectiveTo, endedAt.Format(time.RFC3339))
	}
	var rows int
	if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM pricing_overrides").Scan(&rows); err != nil {
		t.Fatalf("count overrides: %v", err)
	}
	if rows != 1 {
		t.Errorf("override rows = %d, want 1", rows)
	}
}

func TestOverrideChangesPublishPricingInvalidation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	subscription := subscribeInvalidationMessages(t, harness.cache)
	created := harness.createOverride(t, httpapi.EnvironmentLive, standaloneBody)
	want := cache.Invalidation{Kind: cache.InvalidationKindPricing, Environment: string(httpapi.EnvironmentLive), ID: created["id"].(string)}
	assertInvalidation(t, subscription, want)

	assertStatus(t, harness.memberRequest(t, http.MethodPatch, overridePath(created["id"]), httpapi.EnvironmentLive, `{"unit_price":"1"}`), http.StatusOK)
	assertInvalidation(t, subscription, want)
	harness.clock.Advance(time.Minute)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, overridePath(created["id"]), httpapi.EnvironmentLive, ""), http.StatusNoContent)
	assertInvalidation(t, subscription, want)
}

func TestUpdateOverrideRouteChangesPrices(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)
	path := overridePath(created["id"])
	scheduledEnd := serviceStart.Add(24 * time.Hour).Format(time.RFC3339)
	steps := []struct {
		name     string
		body     string
		want     map[string]any
		wantCost string
	}{
		{
			name:     "unit price",
			body:     `{"unit_price":"0.25"}`,
			want:     map[string]any{"unit_price": "0.250000000", "minimum_charge": nil, "billing_increment": nil, "native_unit": nil, "effective_to": nil},
			wantCost: "2.000000000",
		},
		{
			name:     "minimum charge and billing increment",
			body:     `{"minimum_charge":"3","billing_increment":"5","effective_to":"` + scheduledEnd + `"}`,
			want:     map[string]any{"unit_price": "0.250000000", "minimum_charge": "3.000000000", "billing_increment": "5", "native_unit": nil, "effective_to": scheduledEnd},
			wantCost: "3.000000000",
		},
		{
			name:     "native unit",
			body:     `{"unit_price":"20","native_unit":"credits","native_unit_price":"0.0083"}`,
			want:     map[string]any{"unit_price": "20.000000000", "minimum_charge": "3.000000000", "billing_increment": "5", "native_unit": "credits", "effective_to": scheduledEnd},
			wantCost: "3.000000000",
		},
		{
			name:     "clear optional fields",
			body:     `{"minimum_charge":null,"billing_increment":null,"native_unit":null,"native_unit_price":null,"effective_to":null,"unit_price":"0.5"}`,
			want:     map[string]any{"unit_price": "0.500000000", "minimum_charge": nil, "billing_increment": nil, "native_unit": nil, "effective_to": nil},
			wantCost: "4.000000000",
		},
	}
	for _, step := range steps {
		recorder := harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, step.body)

		assertStatus(t, recorder, http.StatusOK)
		body := decodeBody(t, recorder)
		got := map[string]any{}
		for field := range step.want {
			got[field] = body[field]
		}
		if diff := cmp.Diff(step.want, got); diff != "" {
			t.Errorf("%s: override mismatch (-want +got):\n%s", step.name, diff)
		}
		assertCost(t, harness.quote(t, httpapi.EnvironmentTest, unknownModelBody), step.wantCost)
	}
	kept := decodeBody(t, harness.memberRequest(t, http.MethodPatch, path, httpapi.EnvironmentTest, `{}`))
	if kept["unit_price"] != "0.500000000" || kept["model"] != "acme-video-1" {
		t.Errorf("empty update = %v, want the override unchanged", kept)
	}
}

func TestUpdateOverrideRouteKeepsHistory(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	scheduled := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)
	deleted := harness.createOverride(t, httpapi.EnvironmentTest, `{"provider":"acme","model":"acme-video-2","meter":"output_seconds","unit_price":"1","unit_quantity":1}`)
	scheduledEnd := serviceStart.Add(time.Hour).Format(time.RFC3339)
	assertStatus(t, harness.memberRequest(t, http.MethodPatch, overridePath(scheduled["id"]), httpapi.EnvironmentTest, `{"effective_to":"`+scheduledEnd+`"}`), http.StatusOK)
	harness.clock.Advance(30 * time.Minute)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, overridePath(deleted["id"]), httpapi.EnvironmentTest, ""), http.StatusNoContent)

	pastEnd := harness.memberRequest(t, http.MethodPatch, overridePath(scheduled["id"]), httpapi.EnvironmentTest, `{"effective_to":"`+serviceStart.Add(10*time.Minute).Format(time.RFC3339)+`"}`)
	assertProblem(t, pastEnd, http.StatusUnprocessableEntity, "validation_failed", "body.effective_to")
	reopened := harness.memberRequest(t, http.MethodPatch, overridePath(deleted["id"]), httpapi.EnvironmentTest, `{"effective_to":null}`)
	assertProblem(t, reopened, http.StatusConflict, "pricing_override_ended")
	harness.clock.Advance(30 * time.Minute)
	repriced := harness.memberRequest(t, http.MethodPatch, overridePath(scheduled["id"]), httpapi.EnvironmentTest, `{"unit_price":"2"}`)
	assertProblem(t, repriced, http.StatusConflict, "pricing_override_ended")

	for _, created := range []map[string]any{scheduled, deleted} {
		stored := decodeBody(t, harness.memberRequest(t, http.MethodGet, overridePath(created["id"]), httpapi.EnvironmentTest, ""))
		if stored["unit_price"] != created["unit_price"] || stored["effective_to"] == nil {
			t.Errorf("override after rejected updates = %v, want the price unchanged and an end", stored)
		}
	}
}

func TestUpdateOverridePriceAfterStartOpensSuccessor(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createOverride(t, httpapi.EnvironmentTest, veoAdjustmentBody)
	harness.clock.Advance(time.Hour)
	changedAt := serviceStart.Add(time.Hour).Format(time.RFC3339)
	scheduledEnd := serviceStart.Add(48 * time.Hour).Format(time.RFC3339)

	recorder := harness.memberRequest(t, http.MethodPatch, overridePath(created["id"]), httpapi.EnvironmentTest, `{"unit_price":"0.11","effective_to":"`+scheduledEnd+`"}`)

	assertStatus(t, recorder, http.StatusOK)
	successor := decodeBody(t, recorder)
	if successor["id"] == created["id"] {
		t.Errorf("successor id = %v, want a new id", successor["id"])
	}
	wantSuccessor := map[string]any{"type": "adjustment", "unit_price": "0.110000000", "effective_from": changedAt, "effective_to": scheduledEnd, "created_at": changedAt}
	assertOverrideFields(t, "successor", successor, wantSuccessor)
	previous := decodeBody(t, harness.memberRequest(t, http.MethodGet, overridePath(created["id"]), httpapi.EnvironmentTest, ""))
	wantPrevious := map[string]any{"unit_price": "0.120000000", "effective_from": serviceStart.Format(time.RFC3339), "effective_to": changedAt, "updated_at": changedAt}
	assertOverrideFields(t, "previous override", previous, wantPrevious)
	ruleSets := harness.service.RuleSets()
	assertRatedCost(t, "veo before the change", veoCost(t, ruleSets, serviceStart.Add(30*time.Minute)), pointer(money.Amount(960_000_000)))
	assertRatedCost(t, "veo after the change", veoCost(t, ruleSets, harness.clock.Now()), pointer(money.Amount(880_000_000)))
	stale := harness.memberRequest(t, http.MethodPatch, overridePath(created["id"]), httpapi.EnvironmentTest, `{"unit_price":"0.10"}`)
	assertProblem(t, stale, http.StatusConflict, "pricing_override_ended")
	harness.clock.Advance(time.Hour)
	rescheduled := decodeBody(t, harness.memberRequest(t, http.MethodPatch, overridePath(successor["id"]), httpapi.EnvironmentTest, `{"effective_to":null}`))
	assertOverrideFields(t, "rescheduled successor", rescheduled, map[string]any{"id": successor["id"], "unit_price": "0.110000000", "effective_to": nil})
	var rows int
	if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM pricing_overrides").Scan(&rows); err != nil {
		t.Fatalf("count overrides: %v", err)
	}
	if rows != 2 {
		t.Errorf("override rows = %d, want 2", rows)
	}
}

func TestUpdateOverridePriceBeforeStartChangesInPlace(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createOverride(t, httpapi.EnvironmentTest, veoAdjustmentBody)

	updated := decodeBody(t, harness.memberRequest(t, http.MethodPatch, overridePath(created["id"]), httpapi.EnvironmentTest, `{"unit_price":"0.11"}`))

	assertOverrideFields(t, "override", updated, map[string]any{"id": created["id"], "unit_price": "0.110000000", "effective_from": serviceStart.Format(time.RFC3339), "effective_to": nil})
}

func TestOverrideRoutesRejectInvalidInput(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)
	path := overridePath(created["id"])
	tests := []struct {
		name      string
		method    string
		target    string
		body      string
		locations []string
	}{
		{name: "unknown meter", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"seconds","unit_price":"1","unit_quantity":1}`, locations: []string{"body.meter"}},
		{name: "attribute outside the vocabulary", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","conditions":{"resolution":"8k"},"unit_price":"1","unit_quantity":1}`, locations: []string{"body.conditions.resolution"}},
		{name: "attribute of the wrong kind", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","conditions":{"audio":"yes"},"unit_price":"1","unit_quantity":1}`, locations: []string{"body.conditions.audio"}},
		{name: "attribute that is not a scalar", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","conditions":{"<b>secret-value</b>":1.5},"unit_price":"1","unit_quantity":1}`, locations: []string{"body.conditions"}},
		{name: "NUL in a condition value", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","conditions":{"region":"eu\u0000"},"unit_price":"1","unit_quantity":1}`, locations: []string{"body.conditions"}},
		{name: "NUL in a condition key", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","conditions":{"tier\u0000":"gold"},"unit_price":"1","unit_quantity":1}`, locations: []string{"body.conditions"}},
		{name: "negative price", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","unit_price":"-1","unit_quantity":1}`, locations: []string{"body.unit_price"}},
		{name: "price with ten decimals", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","unit_price":"0.0000000001","unit_quantity":1}`, locations: []string{"body.unit_price"}},
		{name: "unit quantity of zero", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","unit_price":"1","unit_quantity":0}`, locations: []string{"body.unit_quantity"}},
		{name: "native unit without its price", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","unit_price":"1","unit_quantity":1,"native_unit":"credits"}`, locations: []string{"body.native_unit_price"}},
		{name: "zero billing increment", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","unit_price":"1","unit_quantity":1,"billing_increment":"0"}`, locations: []string{"body.billing_increment"}},
		{name: "negative minimum charge", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"m","meter":"output_seconds","unit_price":"1","unit_quantity":1,"minimum_charge":"-1"}`, locations: []string{"body.minimum_charge"}},
		{name: "empty model", method: http.MethodPost, target: overridesPath, body: `{"provider":"acme","model":"","meter":"output_seconds","unit_price":"1","unit_quantity":1}`, locations: []string{"body.model"}},
		{
			name:      "every field",
			method:    http.MethodPost,
			target:    overridesPath,
			body:      `{"provider":"","model":"m","meter":"seconds","conditions":{"quality":"ultra"},"unit_price":"x","unit_quantity":0,"native_unit_price":"1","minimum_charge":"x","billing_increment":"x"}`,
			locations: []string{"body.provider", "body.meter", "body.conditions.quality", "body.unit_price", "body.unit_quantity", "body.native_unit", "body.minimum_charge", "body.billing_increment"},
		},
		{name: "update with a bad price", method: http.MethodPatch, target: path, body: `{"unit_price":"1.2.3"}`, locations: []string{"body.unit_price"}},
		{name: "update setting a native unit without its price", method: http.MethodPatch, target: path, body: `{"native_unit":"credits"}`, locations: []string{"body.native_unit_price"}},
		{name: "update ending before the start", method: http.MethodPatch, target: path, body: `{"effective_to":"` + serviceStart.Add(-time.Hour).Format(time.RFC3339) + `"}`, locations: []string{"body.effective_to"}},
		{name: "quote with an unknown meter", method: http.MethodPost, target: quotePath, body: `{"provider":"acme","model":"m","usage":{"output_second":"8"}}`, locations: []string{"body.usage"}},
		{name: "quote with a bad quantity", method: http.MethodPost, target: quotePath, body: `{"provider":"acme","model":"m","usage":{"output_seconds":"8.1234567"}}`, locations: []string{"body.usage.output_seconds"}},
		{name: "quote with a bad attribute", method: http.MethodPost, target: quotePath, body: `{"provider":"acme","model":"m","attributes":{"resolution":"8k","customer_tier":"gold"},"usage":{"output_seconds":"8"}}`, locations: []string{"body.attributes.resolution"}},
		{name: "quote without a model", method: http.MethodPost, target: quotePath, body: `{"provider":"acme","model":"","usage":{"output_seconds":"8"}}`, locations: []string{"body.model"}},
		{name: "quote with a usage number", method: http.MethodPost, target: quotePath, body: `{"provider":"acme","model":"m","usage":{"<b>secret-value</b>":8}}`, locations: []string{"body.usage"}},
		{name: "quote with an attribute object", method: http.MethodPost, target: quotePath, body: `{"provider":"acme","model":"m","attributes":{"<b>secret-value</b>":{}},"usage":{"output_seconds":"8"}}`, locations: []string{"body.attributes"}},
		{name: "quote with a NUL attribute", method: http.MethodPost, target: quotePath, body: `{"provider":"acme","model":"m","attributes":{"region":"\u0000"},"usage":{"output_seconds":"8"}}`, locations: []string{"body.attributes"}},
		{name: "models search with NUL", method: http.MethodGet, target: modelsPath + "?search=veo%00", locations: []string{"query.search"}},
		{name: "models search with invalid UTF-8", method: http.MethodGet, target: modelsPath + "?search=veo%ff", locations: []string{"query.search"}},
		{name: "models provider with NUL", method: http.MethodGet, target: modelsPath + "?provider=fal_ai%00", locations: []string{"query.provider"}},
		{name: "model attributes provider with NUL", method: http.MethodGet, target: modelAttributesPath + "?provider=fal_ai%00&model=m", locations: []string{"query.provider"}},
		{name: "model attributes model with NUL", method: http.MethodGet, target: modelAttributesPath + "?provider=fal_ai&model=m%00", locations: []string{"query.model"}},
		{name: "models with an unknown meter", method: http.MethodGet, target: modelsPath + "?meter=seconds", locations: []string{"query.meter"}},
		{name: "models with a limit above the maximum", method: http.MethodGet, target: modelsPath + "?limit=101", locations: []string{"query.limit"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, test.method, test.target, httpapi.EnvironmentTest, test.body)

			assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed", test.locations...)
			if strings.Contains(recorder.Body.String(), "secret-value") {
				t.Errorf("problem echoes a request key: %s", recorder.Body.String())
			}
		})
	}
	unchanged := decodeBody(t, harness.memberRequest(t, http.MethodGet, path, httpapi.EnvironmentTest, ""))
	if diff := cmp.Diff(created, unchanged); diff != "" {
		t.Errorf("override after rejected updates mismatch (-created +got):\n%s", diff)
	}
	assertProblem(t, harness.memberRequest(t, http.MethodGet, modelsPath+"?cursor=bad", httpapi.EnvironmentTest, ""), http.StatusUnprocessableEntity, "invalid_cursor")
}

func TestListOverridesRouteHidesEndedOverrides(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	first := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)
	harness.clock.Advance(time.Second)
	second := harness.createOverride(t, httpapi.EnvironmentTest, veoAdjustmentBody)
	harness.clock.Advance(time.Second)
	third := harness.createOverride(t, httpapi.EnvironmentTest, `{"provider":"acme","model":"acme-video-2","meter":"output_seconds","unit_price":"1","unit_quantity":1}`)
	harness.createOverride(t, httpapi.EnvironmentLive, standaloneBody)
	harness.clock.Advance(time.Second)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, overridePath(second["id"]), httpapi.EnvironmentTest, ""), http.StatusNoContent)

	open := decodeBody(t, harness.memberRequest(t, http.MethodGet, overridesPath, httpapi.EnvironmentTest, ""))
	firstPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, overridesPath+"?include_ended=true&limit=2", httpapi.EnvironmentTest, ""))
	nextCursor, isString := firstPage["next_cursor"].(string)
	if !isString {
		t.Fatalf("next_cursor = %v, want a cursor", firstPage["next_cursor"])
	}
	secondPage := decodeBody(t, harness.memberRequest(t, http.MethodGet, overridesPath+"?include_ended=true&limit=2&cursor="+nextCursor, httpapi.EnvironmentTest, ""))
	assertProblem(t, harness.memberRequest(t, http.MethodGet, overridesPath+"?include_ended=true&limit=2&cursor="+nextCursor, httpapi.EnvironmentLive, ""), http.StatusUnprocessableEntity, "invalid_cursor")

	if diff := cmp.Diff([]any{third["id"], first["id"]}, overrideIDs(t, open)); diff != "" {
		t.Errorf("open overrides mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{third["id"], second["id"], first["id"]}, append(overrideIDs(t, firstPage), overrideIDs(t, secondPage)...)); diff != "" {
		t.Errorf("every override mismatch (-want +got):\n%s", diff)
	}
}

func TestOverrideRoutesStayInEnvironment(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	live := harness.createOverride(t, httpapi.EnvironmentLive, standaloneBody)
	tests := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "get an override of the other environment", method: http.MethodGet, target: overridePath(live["id"])},
		{name: "update an override of the other environment", method: http.MethodPatch, target: overridePath(live["id"]), body: `{"unit_price":"1"}`},
		{name: "end an override of the other environment", method: http.MethodDelete, target: overridePath(live["id"])},
		{name: "get an unknown override", method: http.MethodGet, target: overridePath(identifiers.Encode(identifiers.PrefixPricingOverride, identifiers.New()))},
		{name: "get a malformed override id", method: http.MethodGet, target: overridesPath + "/pro_unknown"},
		{name: "end an id of another kind", method: http.MethodDelete, target: overridesPath + "/" + identifiers.Encode(identifiers.PrefixPlan, identifiers.New())},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.memberRequest(t, test.method, test.target, httpapi.EnvironmentTest, test.body)

			assertProblem(t, recorder, http.StatusNotFound, "not_found")
		})
	}
	assertCost(t, harness.quote(t, httpapi.EnvironmentTest, unknownModelBody), nil)
	assertCost(t, harness.quote(t, httpapi.EnvironmentLive, unknownModelBody), "4.000000000")
}

func TestModelAttributesRouteListsValuesWithSources(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	harness.createOverride(t, httpapi.EnvironmentTest,
		`{"provider":"fal_ai","model":"fal-ai/veo3.1/fast","meter":"output_seconds","conditions":{"region":"eu","audio":true},"unit_price":"0.2","unit_quantity":1}`)
	query := "?provider=fal_ai&model=" + url.QueryEscape("fal-ai/veo3.1/fast/image-to-video")

	body := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelAttributesPath+query, httpapi.EnvironmentTest, ""))
	liveBody := decodeBody(t, harness.memberRequest(t, http.MethodGet, modelAttributesPath+query, httpapi.EnvironmentLive, ""))

	if body["provider"] != "fal_ai" || body["model"] != "fal-ai/veo3.1/fast" {
		t.Errorf("provider and model = %v and %v, want fal_ai fal-ai/veo3.1/fast", body["provider"], body["model"])
	}
	attributes := attributesByKey(t, body)
	wantKeys := []string{"audio", "context_tier", "quality", "region", "resolution", "service_tier", "size"}
	if diff := cmp.Diff(wantKeys, attributeKeys(t, body)); diff != "" {
		t.Errorf("attribute keys mismatch (-want +got):\n%s", diff)
	}
	wantAudio := map[string]any{
		"key":     "audio",
		"sources": []any{"vocabulary", "parameter_mappings", "rules", "overrides"},
		"values": []any{
			attributeValue(false, "vocabulary", "parameter_mappings", "rules"),
			attributeValue(true, "vocabulary", "parameter_mappings", "overrides"),
		},
	}
	if diff := cmp.Diff(wantAudio, attributes["audio"]); diff != "" {
		t.Errorf("audio mismatch (-want +got):\n%s", diff)
	}
	wantResolution := map[string]any{
		"key":     "resolution",
		"sources": []any{"vocabulary", "parameter_mappings", "rules"},
		"values": []any{
			attributeValue("480p", "vocabulary"),
			attributeValue("540p", "vocabulary"),
			attributeValue("720p", "vocabulary", "parameter_mappings"),
			attributeValue("768p", "vocabulary"),
			attributeValue("1080p", "vocabulary", "parameter_mappings"),
			attributeValue("2k", "vocabulary"),
			attributeValue("4k", "vocabulary", "parameter_mappings", "rules"),
		},
	}
	if diff := cmp.Diff(wantResolution, attributes["resolution"]); diff != "" {
		t.Errorf("resolution mismatch (-want +got):\n%s", diff)
	}
	wantRegion := map[string]any{"key": "region", "sources": []any{"vocabulary", "overrides"}, "values": []any{attributeValue("eu", "overrides")}}
	if diff := cmp.Diff(wantRegion, attributes["region"]); diff != "" {
		t.Errorf("region mismatch (-want +got):\n%s", diff)
	}
	wantLiveRegion := map[string]any{"key": "region", "sources": []any{"vocabulary"}, "values": []any{}}
	if diff := cmp.Diff(wantLiveRegion, attributesByKey(t, liveBody)["region"]); diff != "" {
		t.Errorf("live region mismatch (-want +got):\n%s", diff)
	}
	assertProblem(t, harness.memberRequest(t, http.MethodGet, modelAttributesPath+"?provider=fal_ai", httpapi.EnvironmentTest, ""), http.StatusUnprocessableEntity, "validation_failed", "query.model")
}

func TestPricingRoutesNeedAdminScope(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	runtimeSecret := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeRuntime)
	adminSecret := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeAdmin)
	routes := []struct {
		method     string
		target     string
		body       string
		wantStatus int
	}{
		{method: http.MethodGet, target: metersPath, wantStatus: http.StatusOK},
		{method: http.MethodGet, target: modelsPath, wantStatus: http.StatusOK},
		{method: http.MethodGet, target: modelAttributesPath + "?provider=acme&model=acme-video-1", wantStatus: http.StatusOK},
		{method: http.MethodGet, target: overridesPath, wantStatus: http.StatusOK},
		{method: http.MethodPost, target: quotePath, body: unknownModelBody, wantStatus: http.StatusOK},
		{method: http.MethodPost, target: overridesPath, body: standaloneBody, wantStatus: http.StatusCreated},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.target, func(t *testing.T) {
			assertProblem(t, harness.bearerRequest(t, route.method, route.target, runtimeSecret, route.body), http.StatusForbidden, "scope_forbidden")

			assertStatus(t, harness.bearerRequest(t, route.method, route.target, adminSecret, route.body), route.wantStatus)
		})
	}
	assertCost(t, decodeBody(t, harness.bearerRequest(t, http.MethodPost, quotePath, adminSecret, unknownModelBody)), "4.000000000")
}

func modelKeys(t *testing.T, page map[string]any) [][2]string {
	t.Helper()
	keys := [][2]string{}
	for _, item := range pageItems(t, page) {
		keys = append(keys, [2]string{item["provider"].(string), item["model"].(string)})
	}
	return keys
}

func overrideIDs(t *testing.T, page map[string]any) []any {
	t.Helper()
	ids := []any{}
	for _, item := range pageItems(t, page) {
		ids = append(ids, item["id"])
	}
	return ids
}

func quoteLines(t *testing.T, quote map[string]any) []map[string]any {
	t.Helper()
	listed, isList := quote["lines"].([]any)
	if !isList {
		t.Fatalf("quote %v has no lines", quote)
	}
	lines := make([]map[string]any, 0, len(listed))
	for _, entry := range listed {
		lines = append(lines, entry.(map[string]any))
	}
	return lines
}

func veoLine(t *testing.T, quote map[string]any) map[string]any {
	t.Helper()
	lines := quoteLines(t, quote)
	if len(lines) != 1 {
		t.Fatalf("quote lines = %v, want one output_seconds line", lines)
	}
	return lines[0]
}

func keyPrice(meter, unitPrice string, unitQuantity float64) map[string]any {
	return map[string]any{"meter": meter, "unit_price": unitPrice, "unit_quantity": unitQuantity}
}

func attributeValue(value any, sources ...any) map[string]any {
	return map[string]any{"value": value, "sources": sources}
}

func attributeKeys(t *testing.T, body map[string]any) []string {
	t.Helper()
	keys := []string{}
	for _, attribute := range body["attributes"].([]any) {
		keys = append(keys, attribute.(map[string]any)["key"].(string))
	}
	return keys
}

func attributesByKey(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	attributes := map[string]any{}
	for _, attribute := range body["attributes"].([]any) {
		attributes[attribute.(map[string]any)["key"].(string)] = attribute
	}
	return attributes
}

func assertOverrideFields(t *testing.T, description string, override map[string]any, want map[string]any) {
	t.Helper()
	got := map[string]any{}
	for field := range want {
		got[field] = override[field]
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s", description, diff)
	}
}
