package identifiers_test

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/identifiers"
)

const (
	randomValueCount   = 1000
	sequenceValueCount = 10000
	knownBody          = "01jbvagescfn78y0938nkrkayd"
)

func TestNewReturnsVersion7(t *testing.T) {
	value := identifiers.New()

	if value.Version() != 7 {
		t.Errorf("version = %d, want 7", value.Version())
	}
	if value.Variant() != uuid.RFC4122 {
		t.Errorf("variant = %v, want %v", value.Variant(), uuid.RFC4122)
	}
}

func TestEncodeAndDecodeKnownValues(t *testing.T) {
	tests := []struct {
		name    string
		prefix  identifiers.Prefix
		value   uuid.UUID
		encoded string
	}{
		{
			name:    "zero value",
			prefix:  identifiers.PrefixCustomer,
			value:   uuid.Nil,
			encoded: "cust_00000000000000000000000000",
		},
		{
			name:    "maximum value",
			prefix:  identifiers.PrefixDecision,
			value:   uuid.Max,
			encoded: "dec_7zzzzzzzzzzzzzzzzzzzzzzzzz",
		},
		{
			name:    "ulid specification example",
			prefix:  identifiers.PrefixPolicy,
			value:   uuid.MustParse("01563e3a-b5d3-d676-4c61-efb99302bd5b"),
			encoded: "pol_01arz3ndektsv4rrffq69g5fav",
		},
		{
			name:    "version 7 value",
			prefix:  identifiers.PrefixCustomerUser,
			value:   uuid.MustParse("0192f6a8-3b2c-7d4e-8f01-23456789abcd"),
			encoded: "cuser_" + knownBody,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := identifiers.Encode(test.prefix, test.value); got != test.encoded {
				t.Errorf("Encode = %q, want %q", got, test.encoded)
			}
			got, err := identifiers.Decode(test.prefix, test.encoded)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != test.value {
				t.Errorf("Decode = %v, want %v", got, test.value)
			}
		})
	}
}

func TestEncodeAndDecodeRoundTripRandomValues(t *testing.T) {
	for range randomValueCount {
		var value uuid.UUID
		rand.Read(value[:])
		encoded := identifiers.Encode(identifiers.PrefixLedgerEntry, value)
		decoded, err := identifiers.Decode(identifiers.PrefixLedgerEntry, encoded)
		if err != nil {
			t.Fatalf("Decode(%q): %v", encoded, err)
		}
		if decoded != value {
			t.Fatalf("Decode(%q) = %v, want %v", encoded, decoded, value)
		}
	}
}

func TestEncodedValuesSortInCreationOrder(t *testing.T) {
	previous := identifiers.Encode(identifiers.PrefixDecision, identifiers.New())
	for range sequenceValueCount {
		current := identifiers.Encode(identifiers.PrefixDecision, identifiers.New())
		if current <= previous {
			t.Fatalf("encoded %q after %q, want strictly increasing", current, previous)
		}
		previous = current
	}
}

func TestDecodeRejectsInvalidIdentifiers(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
		message string
	}{
		{
			name:    "empty",
			encoded: "",
			message: "invalid identifier: prefix want=cust_",
		},
		{
			name:    "body without prefix",
			encoded: knownBody,
			message: "invalid identifier: prefix want=cust_",
		},
		{
			name:    "prefix of another entity",
			encoded: "pln_" + knownBody,
			message: "invalid identifier: prefix want=cust_",
		},
		{
			name:    "prefix that extends the wanted prefix",
			encoded: "cuser_" + knownBody,
			message: "invalid identifier: prefix want=cust_",
		},
		{
			name:    "prefix without separator",
			encoded: "cust" + knownBody,
			message: "invalid identifier: prefix want=cust_",
		},
		{
			name:    "uppercase prefix",
			encoded: "CUST_" + knownBody,
			message: "invalid identifier: prefix want=cust_",
		},
		{
			name:    "empty body",
			encoded: "cust_",
			message: "invalid identifier: body length=0 want=26",
		},
		{
			name:    "body one character short",
			encoded: "cust_" + knownBody[1:],
			message: "invalid identifier: body length=25 want=26",
		},
		{
			name:    "body one character long",
			encoded: "cust_" + knownBody + "0",
			message: "invalid identifier: body length=27 want=26",
		},
		{
			name:    "letter i",
			encoded: "cust_" + knownBodyWithCharacter(10, 'i'),
			message: "invalid identifier: character outside alphabet position=10",
		},
		{
			name:    "letter l",
			encoded: "cust_" + knownBodyWithCharacter(11, 'l'),
			message: "invalid identifier: character outside alphabet position=11",
		},
		{
			name:    "letter o",
			encoded: "cust_" + knownBodyWithCharacter(12, 'o'),
			message: "invalid identifier: character outside alphabet position=12",
		},
		{
			name:    "letter u",
			encoded: "cust_" + knownBodyWithCharacter(25, 'u'),
			message: "invalid identifier: character outside alphabet position=25",
		},
		{
			name:    "uppercase body",
			encoded: "cust_" + strings.ToUpper(knownBody),
			message: "invalid identifier: character outside alphabet position=2",
		},
		{
			name:    "separator inside body",
			encoded: "cust_" + knownBodyWithCharacter(0, '_'),
			message: "invalid identifier: character outside alphabet position=0",
		},
		{
			name:    "byte outside ascii",
			encoded: "cust_" + knownBodyWithCharacter(5, 0xff),
			message: "invalid identifier: character outside alphabet position=5",
		},
		{
			name:    "first character 8",
			encoded: "cust_" + knownBodyWithCharacter(0, '8'),
			message: "invalid identifier: value above 128 bits",
		},
		{
			name:    "first character z",
			encoded: "cust_zzzzzzzzzzzzzzzzzzzzzzzzzz",
			message: "invalid identifier: value above 128 bits",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := identifiers.Decode(identifiers.PrefixCustomer, test.encoded)
			if !errors.Is(err, identifiers.ErrInvalidIdentifier) {
				t.Fatalf("Decode(%q) = %v, %v, want ErrInvalidIdentifier", test.encoded, value, err)
			}
			if err.Error() != test.message {
				t.Errorf("error = %q, want %q", err.Error(), test.message)
			}
			if value != uuid.Nil {
				t.Errorf("value = %v, want the nil UUID", value)
			}
		})
	}
}

func FuzzEncodeDecodeRoundTrip(f *testing.F) {
	f.Add(uint64(0), uint64(0))
	f.Add(uint64(math.MaxUint64), uint64(math.MaxUint64))
	f.Add(uint64(0x0192f6a83b2c7d4e), uint64(0x8f0123456789abcd))
	f.Fuzz(func(t *testing.T, high uint64, low uint64) {
		var value uuid.UUID
		binary.BigEndian.PutUint64(value[:8], high)
		binary.BigEndian.PutUint64(value[8:], low)

		encoded := identifiers.Encode(identifiers.PrefixWebhookEvent, value)
		decoded, err := identifiers.Decode(identifiers.PrefixWebhookEvent, encoded)
		if err != nil {
			t.Fatalf("Decode(%q): %v", encoded, err)
		}
		if decoded != value {
			t.Fatalf("Decode(%q) = %v, want %v", encoded, decoded, value)
		}
	})
}

func FuzzDecodeAcceptsOnlyCanonicalIdentifiers(f *testing.F) {
	f.Add("cust_" + knownBody)
	f.Add("cust_" + strings.ToUpper(knownBody))
	f.Add("cust_7zzzzzzzzzzzzzzzzzzzzzzzzz")
	f.Add("cust_80000000000000000000000000")
	f.Add("cuser_" + knownBody)
	f.Add("")
	f.Fuzz(func(t *testing.T, encoded string) {
		value, err := identifiers.Decode(identifiers.PrefixCustomer, encoded)
		if err != nil {
			if !errors.Is(err, identifiers.ErrInvalidIdentifier) {
				t.Fatalf("Decode(%q) error = %v, want ErrInvalidIdentifier", encoded, err)
			}
			return
		}
		if canonical := identifiers.Encode(identifiers.PrefixCustomer, value); canonical != encoded {
			t.Fatalf("Decode(%q) accepted a form that encodes as %q", encoded, canonical)
		}
	})
}

func knownBodyWithCharacter(position int, character byte) string {
	body := []byte(knownBody)
	body[position] = character
	return string(body)
}
