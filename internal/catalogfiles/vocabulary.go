package catalogfiles

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Attributes maps attribute keys to values as decoded from a catalog file.
// After Load every key is in the attribute vocabulary and every value is a
// string, or a bool for audio.
type Attributes map[string]any

// VocabularyAttribute is one key of the attribute vocabulary and the values
// it accepts.
type VocabularyAttribute struct {
	// Key is the attribute key, such as resolution.
	Key string
	// Boolean is true for a key whose value is a boolean, such as audio.
	Boolean bool
	// Values lists the strings a string key accepts, in vocabulary order. It
	// is nil for a boolean key and for a string key that accepts any
	// non-empty string, such as region.
	Values []string
}

type attributeRule struct {
	boolean bool
	values  []string
}

var meterVocabulary = []string{
	"input_tokens", "cached_input_tokens", "cache_write_input_tokens", "output_tokens", "reasoning_tokens",
	"input_audio_tokens", "output_audio_tokens", "input_seconds", "output_seconds", "gpu_seconds",
	"characters", "images", "megapixels", "audio_minutes", "requests", "search_requests",
}

var attributeVocabulary = map[string]attributeRule{
	"audio":        {boolean: true},
	"context_tier": {values: []string{"standard", "above_200k", "above_272k"}},
	"quality":      {values: []string{"low", "medium", "high", "standard", "hd"}},
	"region":       {},
	"resolution":   {values: []string{"480p", "540p", "720p", "768p", "1080p", "2k", "4k"}},
	"service_tier": {values: []string{"standard", "batch", "flex", "priority"}},
	"size":         {},
}

// Meters returns the 16 meters of the fixed meter vocabulary, which every
// rule and parameter mapping names. The caller owns the returned slice.
func Meters() []string {
	return slices.Clone(meterVocabulary)
}

// AttributeVocabulary returns every key of the attribute vocabulary ordered
// by key. The caller owns the returned slices.
func AttributeVocabulary() []VocabularyAttribute {
	vocabulary := make([]VocabularyAttribute, 0, len(attributeVocabulary))
	for _, key := range slices.Sorted(maps.Keys(attributeVocabulary)) {
		rule := attributeVocabulary[key]
		vocabulary = append(vocabulary, VocabularyAttribute{Key: key, Boolean: rule.boolean, Values: slices.Clone(rule.values)})
	}
	return vocabulary
}

// Canonical writes the attributes as key=value pairs sorted by key and joined
// by commas, such as "audio=false,resolution=4k", and the empty string for no
// attributes. Two validated attribute sets are equal exactly when their
// canonical forms are equal.
func (attributes Attributes) Canonical() string {
	pairs := make([]string, 0, len(attributes))
	for _, key := range slices.Sorted(maps.Keys(attributes)) {
		pairs = append(pairs, fmt.Sprintf("%s=%v", key, attributes[key]))
	}
	return strings.Join(pairs, ",")
}

func validateMeter(meter string) error {
	if !slices.Contains(meterVocabulary, meter) {
		return fmt.Errorf("%w %q", ErrUnknownMeter, meter)
	}
	return nil
}

func vocabularyRule(key string) (attributeRule, bool) {
	rule, known := attributeVocabulary[key]
	return rule, known
}

func validateAttributes(attributes Attributes) (Attributes, error) {
	validated := make(Attributes, len(attributes))
	for _, key := range slices.Sorted(maps.Keys(attributes)) {
		if err := validateAttribute(key, attributes[key]); err != nil {
			return nil, err
		}
		validated[key] = attributes[key]
	}
	return validated, nil
}

func validateAttribute(key string, value any) error {
	rule, known := vocabularyRule(key)
	if !known {
		return fmt.Errorf("%w: unknown key %q", ErrInvalidAttribute, key)
	}
	if rule.boolean {
		if _, isBoolean := value.(bool); !isBoolean {
			return fmt.Errorf("%w: %s must be a boolean", ErrInvalidAttribute, key)
		}
		return nil
	}
	text, isString := value.(string)
	switch {
	case !isString:
		return fmt.Errorf("%w: %s must be a string", ErrInvalidAttribute, key)
	case text == "":
		return fmt.Errorf("%w: %s must not be empty", ErrInvalidAttribute, key)
	case rule.values != nil && !slices.Contains(rule.values, text):
		return fmt.Errorf("%w: %s %q is not one of %s", ErrInvalidAttribute, key, text, strings.Join(rule.values, ", "))
	}
	return nil
}
