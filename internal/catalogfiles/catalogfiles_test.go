package catalogfiles_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/money"
)

const (
	validVideoPricing = `models:
  - model: video-fast
    display_name: Video Fast
    source_url: https://video.example.com/pricing
    verified_on: 2026-09-26
    rules:
      - meter: output_seconds
        unit_price: "0.15"
        unit_quantity: 1
      - meter: output_seconds
        conditions:
          audio: false
        unit_price: "0.10"
        unit_quantity: 1
      - meter: output_seconds
        conditions:
          resolution: 4k
          audio: false
        unit_price: "0.30"
        unit_quantity: 1
        minimum_charge: "0.60"
        billing_increment: "1"
`
	validSpeechPricing = `models:
  - model: speech-one
    display_name: Speech One
    source_url: https://speech.example.com/pricing
    verified_on: 2026-09-25
    rules:
      - meter: characters
        unit_price: "0.10"
        unit_quantity: 1000
`
	validAliases = `aliases:
  video:
    video-fast/image-to-video: video-fast
  text:
    text-large-latest: text-large
`
	validDisplayNames = `litellm_models:
  text:
    text-large: Text Large
`
	validDefaultAttributes = `curated_models:
  video:
    video-fast:
      audio: true
      resolution: 720p
  speech:
    speech-one: {}
litellm_models:
  text:
    text-large:
      service_tier: standard
`
	validParameterMappings = `curated_models:
  video:
    video-fast:
      duration:
        provider_parameter: duration
        value_type: string
        allowed_values: [4s, 8s]
        effect: sets
        meter: output_seconds
        quantities:
          4s: "4"
          8s: "8"
      clip_seconds:
        provider_parameter: clip_seconds
        value_type: integer
        allowed_values: [5, 10]
        effect: sets
        meter: output_seconds
      resolution:
        provider_parameter: resolution
        value_type: string
        allowed_values: [720p, 4k]
        effect: prices
        meter: output_seconds
      audio:
        provider_parameter: generate_audio
        value_type: boolean
        effect: prices
        meter: output_seconds
  speech:
    speech-one: {}
litellm_models:
  text:
    text-large:
      max_tokens:
        provider_parameter: max_tokens
        value_type: integer
        minimum: 1
        maximum: 64000
        effect: bounds
        meter: output_tokens
`
)

func TestLoadValidCatalog(t *testing.T) {
	minimumCharge := money.Amount(600_000_000)
	billingIncrement := money.Quantity(1_000_000)
	minimumTokens := int64(1)
	maximumTokens := int64(64000)
	want := catalogfiles.Catalog{
		CuratedModels: []catalogfiles.CuratedModel{
			{
				Provider:    "speech",
				Model:       "speech-one",
				DisplayName: "Speech One",
				SourceURL:   "https://speech.example.com/pricing",
				VerifiedOn:  time.Date(2026, time.September, 25, 0, 0, 0, 0, time.UTC),
				Rules: []catalogfiles.Rule{
					{Meter: "characters", Conditions: catalogfiles.Attributes{}, UnitPrice: money.UnitPrice{Nanos: 100_000_000, UnitQuantity: 1000}},
				},
			},
			{
				Provider:    "video",
				Model:       "video-fast",
				DisplayName: "Video Fast",
				SourceURL:   "https://video.example.com/pricing",
				VerifiedOn:  time.Date(2026, time.September, 26, 0, 0, 0, 0, time.UTC),
				Rules: []catalogfiles.Rule{
					{Meter: "output_seconds", Conditions: catalogfiles.Attributes{}, UnitPrice: money.UnitPrice{Nanos: 150_000_000, UnitQuantity: 1}},
					{Meter: "output_seconds", Conditions: catalogfiles.Attributes{"audio": false}, UnitPrice: money.UnitPrice{Nanos: 100_000_000, UnitQuantity: 1}},
					{
						Meter:            "output_seconds",
						Conditions:       catalogfiles.Attributes{"audio": false, "resolution": "4k"},
						UnitPrice:        money.UnitPrice{Nanos: 300_000_000, UnitQuantity: 1},
						MinimumCharge:    &minimumCharge,
						BillingIncrement: &billingIncrement,
					},
				},
			},
		},
		Aliases: []catalogfiles.Alias{
			{Provider: "text", Alias: "text-large-latest", Model: "text-large"},
			{Provider: "video", Alias: "video-fast/image-to-video", Model: "video-fast"},
		},
		DisplayNames: map[catalogfiles.ModelKey]string{
			{Provider: "speech", Model: "speech-one"}: "Speech One",
			{Provider: "video", Model: "video-fast"}:  "Video Fast",
			{Provider: "text", Model: "text-large"}:   "Text Large",
		},
		DefaultAttributes: map[catalogfiles.ModelKey]catalogfiles.Attributes{
			{Provider: "video", Model: "video-fast"}:  {"audio": true, "resolution": "720p"},
			{Provider: "speech", Model: "speech-one"}: {},
			{Provider: "text", Model: "text-large"}:   {"service_tier": "standard"},
		},
		ParameterMappings: map[catalogfiles.ModelKey]map[string]catalogfiles.Parameter{
			{Provider: "video", Model: "video-fast"}: {
				"duration": {
					ProviderParameter: "duration",
					ValueType:         catalogfiles.ValueTypeString,
					AllowedStrings:    []string{"4s", "8s"},
					Effect:            catalogfiles.EffectSets,
					Meter:             "output_seconds",
					Quantities:        map[string]money.Quantity{"4s": 4_000_000, "8s": 8_000_000},
				},
				"clip_seconds": {
					ProviderParameter: "clip_seconds",
					ValueType:         catalogfiles.ValueTypeInteger,
					AllowedIntegers:   []int64{5, 10},
					Effect:            catalogfiles.EffectSets,
					Meter:             "output_seconds",
				},
				"resolution": {
					ProviderParameter: "resolution",
					ValueType:         catalogfiles.ValueTypeString,
					AllowedStrings:    []string{"720p", "4k"},
					Effect:            catalogfiles.EffectPrices,
					Meter:             "output_seconds",
				},
				"audio": {
					ProviderParameter: "generate_audio",
					ValueType:         catalogfiles.ValueTypeBoolean,
					Effect:            catalogfiles.EffectPrices,
					Meter:             "output_seconds",
				},
			},
			{Provider: "speech", Model: "speech-one"}: {},
			{Provider: "text", Model: "text-large"}: {
				"max_tokens": {
					ProviderParameter: "max_tokens",
					ValueType:         catalogfiles.ValueTypeInteger,
					Minimum:           &minimumTokens,
					Maximum:           &maximumTokens,
					Effect:            catalogfiles.EffectBounds,
					Meter:             "output_tokens",
				},
			},
		},
		LiteLLMModels: []catalogfiles.ModelKey{{Provider: "text", Model: "text-large"}},
	}

	got, err := catalogfiles.Load(validFiles())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if difference := cmp.Diff(want, got); difference != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", difference)
	}
}

func TestLoadRejectsInvalidPricing(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantError error
	}{
		{name: "unknown field", content: withRule("        unit_prize: \"0.15\"\n"), wantError: catalogfiles.ErrInvalidDocument},
		{name: "duplicate key", content: strings.Replace(validVideoPricing, "    display_name: Video Fast\n", "    display_name: Video Fast\n    display_name: Video Faster\n", 1), wantError: catalogfiles.ErrInvalidDocument},
		{name: "empty document", content: "", wantError: catalogfiles.ErrInvalidDocument},
		{name: "second document", content: validVideoPricing + "---\nmodels: []\n", wantError: catalogfiles.ErrInvalidDocument},
		{name: "wrong yaml type", content: strings.Replace(validVideoPricing, "unit_quantity: 1\n", "unit_quantity: \"1\"\n", 1), wantError: catalogfiles.ErrInvalidDocument},
		{name: "unknown meter", content: strings.Replace(validVideoPricing, "meter: output_seconds\n        unit_price: \"0.15\"", "meter: output_second\n        unit_price: \"0.15\"", 1), wantError: catalogfiles.ErrUnknownMeter},
		{name: "bad attribute value", content: strings.Replace(validVideoPricing, "resolution: 4k", "resolution: 900p", 1), wantError: catalogfiles.ErrInvalidAttribute},
		{name: "attribute of the wrong type", content: strings.Replace(validVideoPricing, "audio: false\n        unit_price: \"0.10\"", "audio: \"false\"\n        unit_price: \"0.10\"", 1), wantError: catalogfiles.ErrInvalidAttribute},
		{name: "integer attribute value", content: strings.Replace(validVideoPricing, "resolution: 4k", "resolution: 1080", 1), wantError: catalogfiles.ErrInvalidAttribute},
		{name: "unknown attribute key", content: strings.Replace(validVideoPricing, "resolution: 4k", "style: 4k", 1), wantError: catalogfiles.ErrInvalidAttribute},
		{name: "unit price with exponent", content: strings.Replace(validVideoPricing, "unit_price: \"0.15\"", "unit_price: 1.5e-1", 1), wantError: catalogfiles.ErrInvalidPrice},
		{name: "negative unit price", content: strings.Replace(validVideoPricing, "unit_price: \"0.15\"", "unit_price: \"-0.15\"", 1), wantError: catalogfiles.ErrInvalidPrice},
		{name: "missing unit price", content: strings.Replace(validVideoPricing, "        unit_price: \"0.15\"\n", "", 1), wantError: catalogfiles.ErrInvalidPrice},
		{name: "zero unit quantity", content: strings.Replace(validVideoPricing, "unit_quantity: 1\n", "unit_quantity: 0\n", 1), wantError: catalogfiles.ErrInvalidPrice},
		{name: "missing unit quantity", content: strings.Replace(validVideoPricing, "        unit_quantity: 1\n", "", 1), wantError: catalogfiles.ErrInvalidPrice},
		{name: "bad minimum charge", content: strings.Replace(validVideoPricing, "minimum_charge: \"0.60\"", "minimum_charge: \"0.6.0\"", 1), wantError: catalogfiles.ErrInvalidPrice},
		{name: "zero minimum charge", content: strings.Replace(validVideoPricing, "minimum_charge: \"0.60\"", "minimum_charge: \"0\"", 1), wantError: catalogfiles.ErrInvalidPrice},
		{name: "zero billing increment", content: strings.Replace(validVideoPricing, "billing_increment: \"1\"", "billing_increment: \"0\"", 1), wantError: catalogfiles.ErrInvalidPrice},
		{name: "http source url", content: strings.Replace(validVideoPricing, "https://video.example.com", "http://video.example.com", 1), wantError: catalogfiles.ErrInvalidSourceURL},
		{name: "source url without host", content: strings.Replace(validVideoPricing, "https://video.example.com/pricing", "https:///pricing", 1), wantError: catalogfiles.ErrInvalidSourceURL},
		{name: "missing source url", content: strings.Replace(validVideoPricing, "    source_url: https://video.example.com/pricing\n", "", 1), wantError: catalogfiles.ErrInvalidSourceURL},
		{name: "missing verified on", content: strings.Replace(validVideoPricing, "    verified_on: 2026-09-26\n", "", 1), wantError: catalogfiles.ErrInvalidVerifiedOn},
		{name: "verified on is not a date", content: strings.Replace(validVideoPricing, "verified_on: 2026-09-26", "verified_on: 2026-13-01", 1), wantError: catalogfiles.ErrInvalidVerifiedOn},
		{name: "duplicate rule", content: withRule("      - meter: output_seconds\n        conditions:\n          audio: false\n          resolution: 4k\n        unit_price: \"0.31\"\n        unit_quantity: 1\n"), wantError: catalogfiles.ErrDuplicate},
		{name: "duplicate model", content: validVideoPricing + strings.TrimPrefix(validVideoPricing, "models:\n"), wantError: catalogfiles.ErrDuplicate},
		{name: "missing model", content: strings.Replace(validVideoPricing, "  - model: video-fast\n    display_name", "  - display_name", 1), wantError: catalogfiles.ErrMissingField},
		{name: "missing display name", content: strings.Replace(validVideoPricing, "    display_name: Video Fast\n", "", 1), wantError: catalogfiles.ErrMissingField},
		{name: "no rules", content: "models:\n  - model: video-fast\n    display_name: Video Fast\n    source_url: https://video.example.com/pricing\n    verified_on: 2026-09-26\n", wantError: catalogfiles.ErrMissingField},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertLoadError(t, "pricing/video.yaml", test.content, test.wantError)
		})
	}
}

func TestLoadRejectsInvalidAliases(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantError error
	}{
		{name: "unknown field", content: "aliases: {}\nmodels: {}\n", wantError: catalogfiles.ErrInvalidDocument},
		{name: "alias equal to a curated model", content: "aliases:\n  video:\n    video-fast: video-fast\n", wantError: catalogfiles.ErrModelConflict},
		{name: "alias equal to a listed litellm model", content: "aliases:\n  text:\n    text-large: video-fast\n", wantError: catalogfiles.ErrModelConflict},
		{name: "alias to an unknown model", content: "aliases:\n  video:\n    video-slow-latest: video-slow\n", wantError: catalogfiles.ErrUnknownModel},
		{name: "alias to a model of another provider", content: "aliases:\n  speech:\n    video-fast-latest: video-fast\n", wantError: catalogfiles.ErrUnknownModel},
		{name: "empty alias target", content: "aliases:\n  video:\n    video-fast-latest: \"\"\n", wantError: catalogfiles.ErrMissingField},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertLoadError(t, "model_aliases.yaml", test.content, test.wantError)
		})
	}
}

func TestLoadRejectsInvalidDisplayNames(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantError error
	}{
		{name: "curated section", content: "curated_models: {}\nlitellm_models: {}\n", wantError: catalogfiles.ErrInvalidDocument},
		{name: "litellm entry names a curated model", content: "litellm_models:\n  video:\n    video-fast: Video Fast\n", wantError: catalogfiles.ErrModelConflict},
		{name: "empty display name", content: "litellm_models:\n  text:\n    text-large: \"\"\n", wantError: catalogfiles.ErrMissingField},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertLoadError(t, "model_display_names.yaml", test.content, test.wantError)
		})
	}
}

func TestLoadRejectsInvalidDefaultAttributes(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantError error
	}{
		{name: "unknown field", content: "curated_models: {}\ndefaults: {}\n", wantError: catalogfiles.ErrInvalidDocument},
		{name: "curated entry for an unknown model", content: "curated_models:\n  video:\n    video-slow:\n      audio: true\n", wantError: catalogfiles.ErrUnknownModel},
		{name: "litellm entry names a curated model", content: "litellm_models:\n  video:\n    video-fast:\n      audio: true\n", wantError: catalogfiles.ErrModelConflict},
		{name: "bad attribute value", content: "curated_models:\n  video:\n    video-fast:\n      service_tier: premium\n", wantError: catalogfiles.ErrInvalidAttribute},
		{name: "empty free text attribute", content: "curated_models:\n  video:\n    video-fast:\n      region: \"\"\n", wantError: catalogfiles.ErrInvalidAttribute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertLoadError(t, "model_default_attributes.yaml", test.content, test.wantError)
		})
	}
}

func TestLoadRejectsInvalidParameterMappings(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantError error
	}{
		{name: "unknown field", content: withParameter("duration", "provider_parameter: duration\nvalue_type: string\nallowed_values: [4s]\neffect: sets\nmeter: output_seconds\nquantities: {4s: \"4\"}\nsets: output_seconds\n"), wantError: catalogfiles.ErrInvalidDocument},
		{name: "curated entry for an unknown model", content: "curated_models:\n  video:\n    video-slow: {}\n", wantError: catalogfiles.ErrUnknownModel},
		{name: "litellm entry names a curated model", content: "litellm_models:\n  video:\n    video-fast: {}\n", wantError: catalogfiles.ErrModelConflict},
		{name: "unknown meter", content: withParameter("duration", "provider_parameter: duration\nvalue_type: string\nallowed_values: [4s]\neffect: sets\nmeter: output_second\nquantities: {4s: \"4\"}\n"), wantError: catalogfiles.ErrUnknownMeter},
		{name: "missing provider parameter", content: withParameter("duration", "value_type: string\nallowed_values: [4s]\neffect: sets\nmeter: output_seconds\nquantities: {4s: \"4\"}\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "unknown value type", content: withParameter("duration", "provider_parameter: duration\nvalue_type: number\nallowed_values: [4]\neffect: sets\nmeter: output_seconds\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "unknown effect", content: withParameter("duration", "provider_parameter: duration\nvalue_type: integer\nallowed_values: [4]\neffect: caps\nmeter: output_seconds\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "string without allowed values", content: withParameter("duration", "provider_parameter: duration\nvalue_type: string\neffect: sets\nmeter: output_seconds\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "integer allowed value in a string parameter", content: withParameter("duration", "provider_parameter: duration\nvalue_type: string\nallowed_values: [5]\neffect: sets\nmeter: output_seconds\nquantities: {\"5\": \"5\"}\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "duplicate allowed value", content: withParameter("duration", "provider_parameter: duration\nvalue_type: string\nallowed_values: [4s, 4s]\neffect: sets\nmeter: output_seconds\nquantities: {4s: \"4\"}\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "sets without a quantity for every value", content: withParameter("duration", "provider_parameter: duration\nvalue_type: string\nallowed_values: [4s, 8s]\neffect: sets\nmeter: output_seconds\nquantities: {4s: \"4\"}\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "quantity for a value that is not allowed", content: withParameter("duration", "provider_parameter: duration\nvalue_type: string\nallowed_values: [4s]\neffect: sets\nmeter: output_seconds\nquantities: {4s: \"4\", 8s: \"8\"}\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "quantity that does not parse", content: withParameter("duration", "provider_parameter: duration\nvalue_type: string\nallowed_values: [4s]\neffect: sets\nmeter: output_seconds\nquantities: {4s: \"four\"}\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "boolean that sets a meter", content: withParameter("draft", "provider_parameter: draft\nvalue_type: boolean\neffect: sets\nmeter: output_seconds\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "bounds on a string", content: withParameter("length", "provider_parameter: length\nvalue_type: string\nallowed_values: [short]\neffect: bounds\nmeter: output_tokens\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "integer with values and a range", content: withParameter("max_tokens", "provider_parameter: max_tokens\nvalue_type: integer\nallowed_values: [10]\nminimum: 1\nmaximum: 10\neffect: bounds\nmeter: output_tokens\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "integer without values or a range", content: withParameter("max_tokens", "provider_parameter: max_tokens\nvalue_type: integer\neffect: bounds\nmeter: output_tokens\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "range with minimum above maximum", content: withParameter("max_tokens", "provider_parameter: max_tokens\nvalue_type: integer\nminimum: 10\nmaximum: 1\neffect: bounds\nmeter: output_tokens\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "integer below one", content: withParameter("max_tokens", "provider_parameter: max_tokens\nvalue_type: integer\nminimum: 0\nmaximum: 10\neffect: bounds\nmeter: output_tokens\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "prices through an unknown attribute", content: withParameter("style", "provider_parameter: style\nvalue_type: string\nallowed_values: [anime]\neffect: prices\nmeter: output_seconds\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "prices with a value outside the vocabulary", content: withParameter("resolution", "provider_parameter: resolution\nvalue_type: string\nallowed_values: [900p]\neffect: prices\nmeter: output_seconds\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "prices with the wrong value type", content: withParameter("audio", "provider_parameter: generate_audio\nvalue_type: string\nallowed_values: [on]\neffect: prices\nmeter: output_seconds\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "sets through a pricing attribute", content: withParameter("resolution", "provider_parameter: resolution\nvalue_type: string\nallowed_values: [720p]\neffect: sets\nmeter: output_seconds\nquantities: {720p: \"1\"}\n"), wantError: catalogfiles.ErrInvalidParameter},
		{name: "prices with quantities", content: withParameter("resolution", "provider_parameter: resolution\nvalue_type: string\nallowed_values: [720p]\neffect: prices\nmeter: output_seconds\nquantities: {720p: \"1\"}\n"), wantError: catalogfiles.ErrInvalidParameter},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertLoadError(t, "parameter_mappings.yaml", test.content, test.wantError)
		})
	}
}

func TestLoadRequiresEveryFile(t *testing.T) {
	for _, path := range []string{"model_aliases.yaml", "model_display_names.yaml", "model_default_attributes.yaml", "parameter_mappings.yaml"} {
		t.Run(path, func(t *testing.T) {
			files := validFiles()
			delete(files, path)
			if _, err := catalogfiles.Load(files); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("Load() without %s error = %v, want %v", path, err, fs.ErrNotExist)
			}
		})
	}
}

func TestLoadNamesTheFileInErrors(t *testing.T) {
	files := validFiles()
	files["pricing/video.yaml"] = &fstest.MapFile{Data: []byte(strings.Replace(validVideoPricing, "resolution: 4k", "resolution: 900p", 1))}
	_, err := catalogfiles.Load(files)
	want := `pricing/video.yaml: model "video-fast": rule 3: invalid attribute: resolution "900p" is not one of 480p, 540p, 720p, 768p, 1080p, 2k, 4k`
	if err == nil || err.Error() != want {
		t.Fatalf("Load() error = %v, want %s", err, want)
	}
}

func TestAttributesCanonical(t *testing.T) {
	tests := []struct {
		name       string
		attributes catalogfiles.Attributes
		want       string
	}{
		{name: "empty", attributes: catalogfiles.Attributes{}, want: ""},
		{name: "nil", attributes: nil, want: ""},
		{name: "sorted by key", attributes: catalogfiles.Attributes{"resolution": "4k", "audio": false}, want: "audio=false,resolution=4k"},
		{name: "boolean true", attributes: catalogfiles.Attributes{"audio": true}, want: "audio=true"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.attributes.Canonical(); got != test.want {
				t.Errorf("Canonical() = %q, want %q", got, test.want)
			}
		})
	}
}

func validFiles() fstest.MapFS {
	return fstest.MapFS{
		"pricing/video.yaml":            {Data: []byte(validVideoPricing)},
		"pricing/speech.yaml":           {Data: []byte(validSpeechPricing)},
		"model_aliases.yaml":            {Data: []byte(validAliases)},
		"model_display_names.yaml":      {Data: []byte(validDisplayNames)},
		"model_default_attributes.yaml": {Data: []byte(validDefaultAttributes)},
		"parameter_mappings.yaml":       {Data: []byte(validParameterMappings)},
	}
}

func withRule(rule string) string {
	return validVideoPricing + rule
}

func withParameter(name, fields string) string {
	indented := "        " + strings.ReplaceAll(strings.TrimSuffix(fields, "\n"), "\n", "\n        ") + "\n"
	return "curated_models:\n  video:\n    video-fast:\n      " + name + ":\n" + indented
}

func assertLoadError(t *testing.T, path, content string, wantError error) {
	t.Helper()
	files := validFiles()
	files[path] = &fstest.MapFile{Data: []byte(content)}
	_, err := catalogfiles.Load(files)
	if !errors.Is(err, wantError) {
		t.Fatalf("Load() error = %v, want %v", err, wantError)
	}
	if !strings.HasPrefix(err.Error(), path+": ") {
		t.Errorf("Load() error = %q, want it to start with %q", err, path+": ")
	}
}
