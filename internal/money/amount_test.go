package money_test

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"testing"

	"github.com/preburn/preburn/internal/money"
)

const (
	largestParsableNanos = 999_999_999_999_999_999
	specAmountPattern    = `^-?(0|[1-9][0-9]{0,8})(\.[0-9]{1,9})?$`
)

func TestParseAmount(t *testing.T) {
	tests := []struct {
		value string
		want  money.Amount
	}{
		{value: "0", want: 0},
		{value: "-0", want: 0},
		{value: "0.0", want: 0},
		{value: "12.5", want: 12_500_000_000},
		{value: "-12.5", want: -12_500_000_000},
		{value: "0.000000001", want: 1},
		{value: "-0.000000001", want: -1},
		{value: "1.000000000", want: 1_000_000_000},
		{value: "100", want: 100_000_000_000},
		{value: "999999999.999999999", want: largestParsableNanos},
		{value: "-999999999.999999999", want: -largestParsableNanos},
	}
	for _, test := range tests {
		t.Run(strconv.Quote(test.value), func(t *testing.T) {
			got, err := money.ParseAmount(test.value)
			if err != nil {
				t.Fatalf("ParseAmount(%q): %v", test.value, err)
			}
			if got != test.want {
				t.Errorf("ParseAmount(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestParseNonNegativeAmount(t *testing.T) {
	tests := []struct {
		value     string
		want      money.Amount
		wantError error
	}{
		{value: "0", want: 0},
		{value: "12.5", want: 12_500_000_000},
		{value: "0.000000001", want: 1},
		{value: "999999999.999999999", want: largestParsableNanos},
		{value: "-1", wantError: money.ErrInvalidAmount},
		{value: "-0", wantError: money.ErrInvalidAmount},
		{value: "-0.5", wantError: money.ErrInvalidAmount},
	}
	for _, test := range tests {
		t.Run(strconv.Quote(test.value), func(t *testing.T) {
			got, err := money.ParseNonNegativeAmount(test.value)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ParseNonNegativeAmount(%q) error = %v, want %v", test.value, err, test.wantError)
			}
			if got != test.want {
				t.Errorf("ParseNonNegativeAmount(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		amount money.Amount
		want   string
	}{
		{amount: 0, want: "0.000000000"},
		{amount: 1, want: "0.000000001"},
		{amount: -1, want: "-0.000000001"},
		{amount: 5_000_000_000, want: "5.000000000"},
		{amount: -5_000_000_000, want: "-5.000000000"},
		{amount: 12_500_000_000, want: "12.500000000"},
		{amount: 999_999_999, want: "0.999999999"},
		{amount: largestParsableNanos, want: "999999999.999999999"},
		{amount: math.MaxInt64, want: "9223372036.854775807"},
		{amount: math.MinInt64, want: "-9223372036.854775808"},
	}
	for _, test := range tests {
		if got := money.FormatAmount(test.amount); got != test.want {
			t.Errorf("FormatAmount(%d) = %q, want %q", test.amount, got, test.want)
		}
	}
}

func TestAmountFromCents(t *testing.T) {
	tests := []struct {
		cents     int64
		want      money.Amount
		wantError error
	}{
		{cents: 0, want: 0},
		{cents: 1250, want: 12_500_000_000},
		{cents: -1, want: -10_000_000},
		{cents: 922_337_203_685, want: 9_223_372_036_850_000_000},
		{cents: -922_337_203_685, want: -9_223_372_036_850_000_000},
		{cents: 922_337_203_686, wantError: money.ErrOverflow},
		{cents: -922_337_203_686, wantError: money.ErrOverflow},
		{cents: math.MaxInt64, wantError: money.ErrOverflow},
		{cents: math.MinInt64, wantError: money.ErrOverflow},
	}
	for _, test := range tests {
		got, err := money.AmountFromCents(test.cents)
		if !errors.Is(err, test.wantError) {
			t.Errorf("AmountFromCents(%d) error = %v, want %v", test.cents, err, test.wantError)
		}
		if got != test.want {
			t.Errorf("AmountFromCents(%d) = %d, want %d", test.cents, got, test.want)
		}
	}
}

func TestAmountAdd(t *testing.T) {
	tests := []struct {
		amount    money.Amount
		other     money.Amount
		want      money.Amount
		wantError error
	}{
		{amount: 1, other: 2, want: 3},
		{amount: 5, other: -8, want: -3},
		{amount: math.MaxInt64, other: -1, want: math.MaxInt64 - 1},
		{amount: math.MinInt64, other: math.MaxInt64, want: -1},
		{amount: math.MaxInt64, other: 1, wantError: money.ErrOverflow},
		{amount: math.MinInt64, other: -1, wantError: money.ErrOverflow},
		{amount: math.MaxInt64, other: math.MaxInt64, wantError: money.ErrOverflow},
	}
	for _, test := range tests {
		got, err := test.amount.Add(test.other)
		if !errors.Is(err, test.wantError) {
			t.Errorf("%d.Add(%d) error = %v, want %v", test.amount, test.other, err, test.wantError)
		}
		if got != test.want {
			t.Errorf("%d.Add(%d) = %d, want %d", test.amount, test.other, got, test.want)
		}
	}
}

func TestAmountSubtract(t *testing.T) {
	tests := []struct {
		amount    money.Amount
		other     money.Amount
		want      money.Amount
		wantError error
	}{
		{amount: 5, other: 3, want: 2},
		{amount: 3, other: 5, want: -2},
		{amount: -1, other: math.MaxInt64, want: math.MinInt64},
		{amount: 0, other: math.MaxInt64, want: -math.MaxInt64},
		{amount: math.MinInt64, other: 1, wantError: money.ErrOverflow},
		{amount: math.MaxInt64, other: -1, wantError: money.ErrOverflow},
		{amount: 0, other: math.MinInt64, wantError: money.ErrOverflow},
	}
	for _, test := range tests {
		got, err := test.amount.Subtract(test.other)
		if !errors.Is(err, test.wantError) {
			t.Errorf("%d.Subtract(%d) error = %v, want %v", test.amount, test.other, err, test.wantError)
		}
		if got != test.want {
			t.Errorf("%d.Subtract(%d) = %d, want %d", test.amount, test.other, got, test.want)
		}
	}
}

func TestSum(t *testing.T) {
	tests := []struct {
		name      string
		amounts   []money.Amount
		want      money.Amount
		wantError error
	}{
		{name: "empty", amounts: nil, want: 0},
		{name: "meter lines", amounts: []money.Amount{185_100, 3_400_000_000, 1}, want: 3_400_185_101},
		{name: "mixed signs", amounts: []money.Amount{math.MaxInt64, -1, 1}, want: math.MaxInt64},
		{name: "overflow", amounts: []money.Amount{math.MaxInt64, 1}, wantError: money.ErrOverflow},
		{name: "underflow", amounts: []money.Amount{math.MinInt64, -1}, wantError: money.ErrOverflow},
		{name: "running total overflow", amounts: []money.Amount{math.MaxInt64, 1, -1}, wantError: money.ErrOverflow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := money.Sum(test.amounts)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Sum(%v) error = %v, want %v", test.amounts, err, test.wantError)
			}
			if got != test.want {
				t.Errorf("Sum(%v) = %d, want %d", test.amounts, got, test.want)
			}
		})
	}
}

func FuzzAmountRoundTrip(f *testing.F) {
	for _, seed := range []int64{
		0,
		1,
		-1,
		12_500_000_000,
		largestParsableNanos,
		-largestParsableNanos,
		largestParsableNanos + 1,
		-largestParsableNanos - 1,
		math.MaxInt64,
		math.MinInt64,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, nanos int64) {
		formatted := money.FormatAmount(money.Amount(nanos))
		parsed, err := money.ParseAmount(formatted)
		if nanos > largestParsableNanos || nanos < -largestParsableNanos {
			if !errors.Is(err, money.ErrInvalidAmount) {
				t.Fatalf("ParseAmount(%q) error = %v, want ErrInvalidAmount", formatted, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("ParseAmount(%q): %v", formatted, err)
		}
		if parsed != money.Amount(nanos) {
			t.Fatalf("ParseAmount(%q) = %d, want %d", formatted, parsed, nanos)
		}
	})
}

func FuzzParseAmountMatchesSpecPattern(f *testing.F) {
	pattern := regexp.MustCompile(specAmountPattern)
	for _, seed := range malformedDecimals {
		f.Add(seed)
	}
	for _, seed := range []string{"0", "-0", "12.5", "-0.000000001", "999999999.999999999"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		amount, err := money.ParseAmount(value)
		if accepted := err == nil; accepted != pattern.MatchString(value) {
			t.Fatalf("ParseAmount(%q) accepted = %t, spec pattern match = %t", value, accepted, !accepted)
		}
		if err != nil {
			if !errors.Is(err, money.ErrInvalidAmount) {
				t.Fatalf("ParseAmount(%q) error = %v, want ErrInvalidAmount", value, err)
			}
			return
		}
		dollars, valid := new(big.Rat).SetString(value)
		if !valid {
			t.Fatalf("big.Rat rejected %q", value)
		}
		want := dollars.Mul(dollars, big.NewRat(1_000_000_000, 1))
		if !want.IsInt() || !want.Num().IsInt64() || want.Num().Int64() != int64(amount) {
			t.Fatalf("ParseAmount(%q) = %d, want %s", value, amount, want.RatString())
		}
	})
}
