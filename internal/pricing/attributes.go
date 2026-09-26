package pricing

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
)

const (
	attributeKindString attributeKind = iota + 1
	attributeKindBoolean
	attributeKindInteger
)

type attributeKind uint8

// AttributeValue is one attribute of a request or one condition of a rule:
// a string, a boolean or an integer. Values of different kinds are never
// equal, so the boolean true does not match the string "true". Values
// compare with ==. The zero AttributeValue holds nothing and fails JSON
// encoding.
type AttributeValue struct {
	kind    attributeKind
	text    string
	boolean bool
	integer int64
}

// Attributes maps attribute keys, such as resolution or audio, to their
// values. It describes a request and, as a rule's conditions, the requests
// the rule prices.
type Attributes map[string]AttributeValue

// InvalidAttributeError reports a known attribute key whose value has the
// wrong kind or lies outside the key's vocabulary.
type InvalidAttributeError struct {
	// Key is the attribute key, such as resolution.
	Key string
	// Requirement states what the key accepts, such as "a boolean" or
	// "one of low, medium, high, standard, hd".
	Requirement string
}

type attributeRequirement struct {
	key    string
	kind   attributeKind
	values []string
}

// ErrInvalidAttributeValue reports JSON that is not a string, a boolean or
// an integer within int64, and the JSON encoding of a zero AttributeValue.
var ErrInvalidAttributeValue = errors.New("attribute value must be a string, a boolean or an integer")

var attributeVocabulary = []attributeRequirement{
	{key: "audio", kind: attributeKindBoolean},
	{key: "context_tier", kind: attributeKindString, values: []string{"standard", "above_200k", "above_272k"}},
	{key: "quality", kind: attributeKindString, values: []string{"low", "medium", "high", "standard", "hd"}},
	{key: "region", kind: attributeKindString},
	{key: "resolution", kind: attributeKindString, values: []string{"480p", "540p", "720p", "768p", "1080p", "2k", "4k"}},
	{key: "service_tier", kind: attributeKindString, values: []string{"standard", "batch", "flex", "priority"}},
	{key: "size", kind: attributeKindString},
}

// StringAttribute returns an AttributeValue holding the string value.
func StringAttribute(value string) AttributeValue {
	return AttributeValue{kind: attributeKindString, text: value}
}

// BooleanAttribute returns an AttributeValue holding the boolean value.
func BooleanAttribute(value bool) AttributeValue {
	return AttributeValue{kind: attributeKindBoolean, boolean: value}
}

// IntegerAttribute returns an AttributeValue holding the integer value.
func IntegerAttribute(value int64) AttributeValue {
	return AttributeValue{kind: attributeKindInteger, integer: value}
}

// MarshalJSON writes attributeValue as a JSON string, boolean or integer
// number. A zero AttributeValue returns ErrInvalidAttributeValue.
func (attributeValue AttributeValue) MarshalJSON() ([]byte, error) {
	switch attributeValue.kind {
	case attributeKindString:
		return json.Marshal(attributeValue.text)
	case attributeKindBoolean:
		return strconv.AppendBool(nil, attributeValue.boolean), nil
	case attributeKindInteger:
		return strconv.AppendInt(nil, attributeValue.integer, 10), nil
	}
	return nil, ErrInvalidAttributeValue
}

// UnmarshalJSON reads a JSON string, boolean or integer number into
// attributeValue. Any other JSON, such as null, 1.5, 1e3 or a number beyond
// int64, returns ErrInvalidAttributeValue.
func (attributeValue *AttributeValue) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	switch value := decoded.(type) {
	case string:
		*attributeValue = StringAttribute(value)
	case bool:
		*attributeValue = BooleanAttribute(value)
	case json.Number:
		integer, err := strconv.ParseInt(value.String(), 10, 64)
		if err != nil {
			return ErrInvalidAttributeValue
		}
		*attributeValue = IntegerAttribute(integer)
	default:
		return ErrInvalidAttributeValue
	}
	return nil
}

// Error states the key and what it accepts, such as "attribute audio must be
// a boolean".
func (attributeError *InvalidAttributeError) Error() string {
	return "attribute " + attributeError.Key + " must be " + attributeError.Requirement
}

// ValidateAttributes checks attributes against the vocabulary of known keys:
// audio is a boolean, region and size are any string, and context_tier,
// quality, resolution and service_tier are one of their listed strings.
// Unknown keys may hold any value. It returns an *InvalidAttributeError for
// the first known key, in alphabetical order, whose value breaks its rule.
func ValidateAttributes(attributes Attributes) error {
	for _, requirement := range attributeVocabulary {
		value, present := attributes[requirement.key]
		if !present {
			continue
		}
		if value.kind != requirement.kind || (len(requirement.values) > 0 && !slices.Contains(requirement.values, value.text)) {
			return &InvalidAttributeError{Key: requirement.key, Requirement: requirement.describe()}
		}
	}
	return nil
}

func (attributes Attributes) contain(conditions Attributes) bool {
	for key, condition := range conditions {
		value, present := attributes[key]
		if !present || value != condition {
			return false
		}
	}
	return true
}

func (requirement attributeRequirement) describe() string {
	if requirement.kind == attributeKindBoolean {
		return "a boolean"
	}
	if len(requirement.values) == 0 {
		return "a string"
	}
	return "one of " + strings.Join(requirement.values, ", ")
}
