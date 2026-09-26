package pricing

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// NullableChange is a change to one optional field of an override for
// Service.UpdateOverride, decoded from a JSON field that may hold null. The
// zero NullableChange, an absent field, keeps the stored value. Replace is
// true when the field is present, and a nil Value then clears the stored
// value.
type NullableChange[Value any] struct {
	// Replace tells whether the change replaces the stored value.
	Replace bool
	// Value is the new value, nil to clear it.
	Value *Value
}

var meterVocabulary = []Meter{
	MeterInputTokens, MeterCachedInputTokens, MeterCacheWriteInputTokens, MeterOutputTokens, MeterReasoningTokens,
	MeterInputAudioTokens, MeterOutputAudioTokens, MeterInputSeconds, MeterOutputSeconds, MeterGPUSeconds,
	MeterCharacters, MeterImages, MeterMegapixels, MeterAudioMinutes, MeterRequests, MeterSearchRequests,
}

// Schema documents an attribute value as a JSON string, boolean or integer,
// so request validation rejects any other JSON before UnmarshalJSON runs.
func (AttributeValue) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{OneOf: []*huma.Schema{
		{Type: huma.TypeString},
		{Type: huma.TypeBoolean},
		{Type: huma.TypeInteger, Format: "int64"},
	}}
}

// Schema documents a meter as a string holding one of the 16 meter names.
func (Meter) Schema(huma.Registry) *huma.Schema {
	names := make([]any, 0, len(meterVocabulary))
	for _, meter := range meterVocabulary {
		names = append(names, string(meter))
	}
	return &huma.Schema{Type: huma.TypeString, Enum: names}
}

// UnmarshalJSON records that the request holds the field, and its value
// unless the value is null.
func (change *NullableChange[Value]) UnmarshalJSON(data []byte) error {
	change.Replace = true
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	return json.Unmarshal(data, &change.Value)
}

// Schema documents the field as its value's schema or null.
func (NullableChange[Value]) Schema(registry huma.Registry) *huma.Schema {
	schema := huma.SchemaFromType(registry, reflect.TypeFor[Value]())
	schema.Nullable = true
	return schema
}
