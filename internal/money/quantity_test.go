package money_test

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/preburn/preburn/internal/money"
)

func TestParseQuantity(t *testing.T) {
	tests := []struct {
		value     string
		want      money.Quantity
		wantError error
	}{
		{value: "0", want: 0},
		{value: "8", want: 8_000_000},
		{value: "8.5", want: 8_500_000},
		{value: "8.500000", want: 8_500_000},
		{value: "0.000001", want: 1},
		{value: "1234", want: 1_234_000_000},
		{value: "999999999.999999", want: 999_999_999_999_999},
		{value: "0.0000001", wantError: money.ErrInvalidQuantity},
		{value: "8.5000000", wantError: money.ErrInvalidQuantity},
		{value: "-1", wantError: money.ErrInvalidQuantity},
		{value: "-0", wantError: money.ErrInvalidQuantity},
		{value: "-8.5", wantError: money.ErrInvalidQuantity},
	}
	for _, test := range tests {
		t.Run(strconv.Quote(test.value), func(t *testing.T) {
			got, err := money.ParseQuantity(test.value)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ParseQuantity(%q) error = %v, want %v", test.value, err, test.wantError)
			}
			if got != test.want {
				t.Errorf("ParseQuantity(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestFormatQuantity(t *testing.T) {
	tests := []struct {
		quantity money.Quantity
		want     string
	}{
		{quantity: 0, want: "0"},
		{quantity: 1, want: "0.000001"},
		{quantity: 8_000_000, want: "8"},
		{quantity: 8_500_000, want: "8.5"},
		{quantity: 10_000_000, want: "10"},
		{quantity: 1_230_000, want: "1.23"},
		{quantity: 100_500_000, want: "100.5"},
		{quantity: 999_999_999_999_999, want: "999999999.999999"},
		{quantity: -8_500_000, want: "-8.5"},
	}
	for _, test := range tests {
		if got := money.FormatQuantity(test.quantity); got != test.want {
			t.Errorf("FormatQuantity(%d) = %q, want %q", test.quantity, got, test.want)
		}
	}
}

func TestRoundUpToIncrement(t *testing.T) {
	tests := []struct {
		name      string
		quantity  money.Quantity
		increment money.Quantity
		want      money.Quantity
		wantError error
	}{
		{name: "partial second", quantity: 8_200_000, increment: 1_000_000, want: 9_000_000},
		{name: "exact multiple", quantity: 8_000_000, increment: 1_000_000, want: 8_000_000},
		{name: "zero", quantity: 0, increment: 1_000_000, want: 0},
		{name: "one micro-unit", quantity: 1, increment: 1_000_000, want: 1_000_000},
		{name: "increment of one micro-unit", quantity: 8_500_001, increment: 1, want: 8_500_001},
		{name: "thousand tokens", quantity: 1_234_000_000, increment: 1_000_000_000, want: 2_000_000_000},
		{name: "negative rounds toward positive infinity", quantity: -5, increment: 2, want: -4},
		{name: "largest quantity", quantity: math.MaxInt64, increment: 1, want: math.MaxInt64},
		{name: "overflow", quantity: math.MaxInt64 - 1, increment: 1 << 62, wantError: money.ErrOverflow},
		{name: "zero increment", quantity: 5, increment: 0, wantError: money.ErrInvalidIncrement},
		{name: "negative increment", quantity: 5, increment: -1_000_000, wantError: money.ErrInvalidIncrement},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := money.RoundUpToIncrement(test.quantity, test.increment)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("RoundUpToIncrement(%d, %d) error = %v, want %v", test.quantity, test.increment, err, test.wantError)
			}
			if got != test.want {
				t.Errorf("RoundUpToIncrement(%d, %d) = %d, want %d", test.quantity, test.increment, got, test.want)
			}
		})
	}
}
