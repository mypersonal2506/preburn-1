package money_test

import (
	"errors"
	"math"
	"testing"

	"github.com/preburn/preburn/internal/money"
)

func TestParseUnitPrice(t *testing.T) {
	tests := []struct {
		name         string
		price        string
		unitQuantity int64
		want         money.UnitPrice
		wantError    error
	}{
		{name: "per million tokens", price: "0.15", unitQuantity: 1_000_000, want: money.UnitPrice{Nanos: 150_000_000, UnitQuantity: 1_000_000}},
		{name: "per second", price: "0.40", unitQuantity: 1, want: money.UnitPrice{Nanos: 400_000_000, UnitQuantity: 1}},
		{name: "free", price: "0", unitQuantity: 1, want: money.UnitPrice{Nanos: 0, UnitQuantity: 1}},
		{name: "negative price", price: "-0.15", unitQuantity: 1, wantError: money.ErrInvalidAmount},
		{name: "exponent price", price: "1e-1", unitQuantity: 1, wantError: money.ErrInvalidAmount},
		{name: "empty price", price: "", unitQuantity: 1, wantError: money.ErrInvalidAmount},
		{name: "zero unit quantity", price: "0.15", unitQuantity: 0, wantError: money.ErrInvalidUnitQuantity},
		{name: "negative unit quantity", price: "0.15", unitQuantity: -1, wantError: money.ErrInvalidUnitQuantity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := money.ParseUnitPrice(test.price, test.unitQuantity)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ParseUnitPrice(%q, %d) error = %v, want %v", test.price, test.unitQuantity, err, test.wantError)
			}
			if got != test.want {
				t.Errorf("ParseUnitPrice(%q, %d) = %+v, want %+v", test.price, test.unitQuantity, got, test.want)
			}
		})
	}
}

func TestRate(t *testing.T) {
	tests := []struct {
		name      string
		quantity  money.Quantity
		price     money.UnitPrice
		want      money.Amount
		wantError error
	}{
		{
			name:     "0.15 per million tokens times 1234 tokens",
			quantity: 1_234_000_000,
			price:    money.UnitPrice{Nanos: 150_000_000, UnitQuantity: 1_000_000},
			want:     185_100,
		},
		{
			name:     "0.40 per second times 8.5 seconds",
			quantity: 8_500_000,
			price:    money.UnitPrice{Nanos: 400_000_000, UnitQuantity: 1},
			want:     3_400_000_000,
		},
		{
			name:     "exact half nano rounds up",
			quantity: 1,
			price:    money.UnitPrice{Nanos: 500_000, UnitQuantity: 1},
			want:     1,
		},
		{
			name:     "exact half nano with odd divisor rounds up",
			quantity: 1,
			price:    money.UnitPrice{Nanos: 1_500_000, UnitQuantity: 3},
			want:     1,
		},
		{
			name:     "just below half nano rounds down",
			quantity: 1,
			price:    money.UnitPrice{Nanos: 499_999, UnitQuantity: 1},
			want:     0,
		},
		{
			name:     "negative exact half rounds toward positive infinity",
			quantity: -1,
			price:    money.UnitPrice{Nanos: 500_000, UnitQuantity: 1},
			want:     0,
		},
		{
			name:     "zero quantity",
			quantity: 0,
			price:    money.UnitPrice{Nanos: 400_000_000, UnitQuantity: 1},
			want:     0,
		},
		{
			name:     "product overflows int64 before division but result fits",
			quantity: 999_999_999_000_000,
			price:    money.UnitPrice{Nanos: 9_000_000_000, UnitQuantity: 1_000_000},
			want:     8_999_999_991_000,
		},
		{
			name:     "result equal to the largest int64",
			quantity: 1_000_000,
			price:    money.UnitPrice{Nanos: math.MaxInt64, UnitQuantity: 1},
			want:     math.MaxInt64,
		},
		{
			name:      "result above the largest int64",
			quantity:  2_000_000,
			price:     money.UnitPrice{Nanos: math.MaxInt64, UnitQuantity: 1},
			wantError: money.ErrOverflow,
		},
		{
			name:      "result far beyond int64",
			quantity:  999_999_999_999_999,
			price:     money.UnitPrice{Nanos: largestParsableNanos, UnitQuantity: 1},
			wantError: money.ErrOverflow,
		},
		{
			name:      "zero unit quantity",
			quantity:  1_000_000,
			price:     money.UnitPrice{Nanos: 400_000_000, UnitQuantity: 0},
			wantError: money.ErrInvalidUnitQuantity,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := money.Rate(test.quantity, test.price)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Rate(%d, %+v) error = %v, want %v", test.quantity, test.price, err, test.wantError)
			}
			if got != test.want {
				t.Errorf("Rate(%d, %+v) = %d, want %d", test.quantity, test.price, got, test.want)
			}
		})
	}
}
