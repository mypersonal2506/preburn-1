package pricing_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/pricing"
)

var attributeValueComparer = cmp.Comparer(func(first, second pricing.AttributeValue) bool {
	return first == second
})

func TestAttributesJSONPreservesKinds(t *testing.T) {
	encoded := `{"audio":true,"resolution":"1080p","steps":30,"tier":"30"}`
	want := pricing.Attributes{
		"audio":      pricing.BooleanAttribute(true),
		"resolution": pricing.StringAttribute("1080p"),
		"steps":      pricing.IntegerAttribute(30),
		"tier":       pricing.StringAttribute("30"),
	}

	var decoded pricing.Attributes
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", encoded, err)
	}
	if diff := cmp.Diff(want, decoded, attributeValueComparer); diff != "" {
		t.Errorf("decoded attributes mismatch (-want +got):\n%s", diff)
	}

	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	if string(reencoded) != encoded {
		t.Errorf("json.Marshal = %s, want %s", reencoded, encoded)
	}
}

func TestAttributeValueKindsNeverEqual(t *testing.T) {
	tests := []struct {
		name   string
		first  pricing.AttributeValue
		second pricing.AttributeValue
	}{
		{name: "boolean true and string true", first: pricing.BooleanAttribute(true), second: pricing.StringAttribute("true")},
		{name: "integer 1 and string 1", first: pricing.IntegerAttribute(1), second: pricing.StringAttribute("1")},
		{name: "boolean false and integer 0", first: pricing.BooleanAttribute(false), second: pricing.IntegerAttribute(0)},
		{name: "empty string and integer 0", first: pricing.StringAttribute(""), second: pricing.IntegerAttribute(0)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.first == test.second {
				t.Errorf("%+v == %+v, want different values", test.first, test.second)
			}
		})
	}
}

func TestAttributeValueUnmarshalJSONRejectsOtherJSON(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
	}{
		{name: "null", encoded: `{"audio":null}`},
		{name: "fraction", encoded: `{"steps":1.5}`},
		{name: "exponent", encoded: `{"steps":1e3}`},
		{name: "integer above int64", encoded: `{"steps":9223372036854775808}`},
		{name: "object", encoded: `{"size":{"width":1024}}`},
		{name: "array", encoded: `{"size":[1024,1024]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var decoded pricing.Attributes
			err := json.Unmarshal([]byte(test.encoded), &decoded)
			if !errors.Is(err, pricing.ErrInvalidAttributeValue) {
				t.Errorf("json.Unmarshal(%s) error = %v, want %v", test.encoded, err, pricing.ErrInvalidAttributeValue)
			}
		})
	}
}

func TestAttributeValueMarshalJSONRejectsZeroValue(t *testing.T) {
	_, err := json.Marshal(pricing.Attributes{"audio": {}})
	if !errors.Is(err, pricing.ErrInvalidAttributeValue) {
		t.Errorf("json.Marshal of a zero AttributeValue error = %v, want %v", err, pricing.ErrInvalidAttributeValue)
	}
}

func TestValidateAttributes(t *testing.T) {
	tests := []struct {
		name       string
		attributes pricing.Attributes
		want       *pricing.InvalidAttributeError
	}{
		{name: "no attributes", attributes: pricing.Attributes{}},
		{
			name: "every known key with a valid value",
			attributes: pricing.Attributes{
				"resolution":   pricing.StringAttribute("1080p"),
				"audio":        pricing.BooleanAttribute(true),
				"service_tier": pricing.StringAttribute("batch"),
				"region":       pricing.StringAttribute("us-east-1"),
				"quality":      pricing.StringAttribute("hd"),
				"size":         pricing.StringAttribute("1024x1024"),
				"context_tier": pricing.StringAttribute("above_200k"),
			},
		},
		{
			name: "unknown keys of every kind",
			attributes: pricing.Attributes{
				"seed":  pricing.IntegerAttribute(42),
				"style": pricing.StringAttribute("cinematic"),
				"loop":  pricing.BooleanAttribute(false),
			},
		},
		{
			name:       "resolution outside the vocabulary",
			attributes: pricing.Attributes{"resolution": pricing.StringAttribute("8k")},
			want:       &pricing.InvalidAttributeError{Key: "resolution", Requirement: "one of 480p, 540p, 720p, 768p, 1080p, 2k, 4k"},
		},
		{
			name:       "resolution as an integer",
			attributes: pricing.Attributes{"resolution": pricing.IntegerAttribute(1080)},
			want:       &pricing.InvalidAttributeError{Key: "resolution", Requirement: "one of 480p, 540p, 720p, 768p, 1080p, 2k, 4k"},
		},
		{
			name:       "audio as a string",
			attributes: pricing.Attributes{"audio": pricing.StringAttribute("true")},
			want:       &pricing.InvalidAttributeError{Key: "audio", Requirement: "a boolean"},
		},
		{
			name:       "service tier outside the vocabulary",
			attributes: pricing.Attributes{"service_tier": pricing.StringAttribute("premium")},
			want:       &pricing.InvalidAttributeError{Key: "service_tier", Requirement: "one of standard, batch, flex, priority"},
		},
		{
			name:       "region as an integer",
			attributes: pricing.Attributes{"region": pricing.IntegerAttribute(1)},
			want:       &pricing.InvalidAttributeError{Key: "region", Requirement: "a string"},
		},
		{
			name:       "quality outside the vocabulary",
			attributes: pricing.Attributes{"quality": pricing.StringAttribute("ultra")},
			want:       &pricing.InvalidAttributeError{Key: "quality", Requirement: "one of low, medium, high, standard, hd"},
		},
		{
			name:       "size as a boolean",
			attributes: pricing.Attributes{"size": pricing.BooleanAttribute(true)},
			want:       &pricing.InvalidAttributeError{Key: "size", Requirement: "a string"},
		},
		{
			name:       "context tier outside the vocabulary",
			attributes: pricing.Attributes{"context_tier": pricing.StringAttribute("above_100k")},
			want:       &pricing.InvalidAttributeError{Key: "context_tier", Requirement: "one of standard, above_200k, above_272k"},
		},
		{
			name: "first invalid key in alphabetical order",
			attributes: pricing.Attributes{
				"resolution": pricing.StringAttribute("8k"),
				"audio":      pricing.StringAttribute("yes"),
			},
			want: &pricing.InvalidAttributeError{Key: "audio", Requirement: "a boolean"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := pricing.ValidateAttributes(test.attributes)
			if test.want == nil {
				if err != nil {
					t.Fatalf("ValidateAttributes error = %v, want nil", err)
				}
				return
			}
			var invalid *pricing.InvalidAttributeError
			if !errors.As(err, &invalid) {
				t.Fatalf("ValidateAttributes error = %v, want *InvalidAttributeError", err)
			}
			if diff := cmp.Diff(test.want, invalid); diff != "" {
				t.Errorf("InvalidAttributeError mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInvalidAttributeErrorMessage(t *testing.T) {
	err := pricing.ValidateAttributes(pricing.Attributes{"audio": pricing.StringAttribute("true")})
	want := "attribute audio must be a boolean"
	if err == nil || err.Error() != want {
		t.Errorf("ValidateAttributes error = %v, want %q", err, want)
	}
}
