package money_test

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/preburn/preburn/internal/money"
)

func TestParseRatio(t *testing.T) {
	tests := []struct {
		value     string
		minimum   money.BasisPoints
		maximum   money.BasisPoints
		want      money.BasisPoints
		wantError error
	}{
		{value: "0.40", minimum: 0, maximum: 10_000, want: 4000},
		{value: "0.4", minimum: 0, maximum: 10_000, want: 4000},
		{value: "0.0001", minimum: 0, maximum: 10_000, want: 1},
		{value: "0", minimum: 0, maximum: 10_000, want: 0},
		{value: "1", minimum: 0, maximum: 10_000, want: 10_000},
		{value: "-0.25", minimum: -10_000, maximum: 10_000, want: -2500},
		{value: "999999999.9999", minimum: math.MinInt64, maximum: math.MaxInt64, want: 9_999_999_999_999},
		{value: "0.12345", minimum: 0, maximum: 10_000, wantError: money.ErrInvalidRatio},
		{value: "inf", minimum: math.MinInt64, maximum: math.MaxInt64, wantError: money.ErrInvalidRatio},
		{value: "1.0001", minimum: 0, maximum: 10_000, wantError: money.ErrRatioOutOfRange},
		{value: "2", minimum: 0, maximum: 10_000, wantError: money.ErrRatioOutOfRange},
		{value: "-0.0001", minimum: 0, maximum: 10_000, wantError: money.ErrRatioOutOfRange},
	}
	for _, test := range tests {
		t.Run(strconv.Quote(test.value), func(t *testing.T) {
			got, err := money.ParseRatio(test.value, test.minimum, test.maximum)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ParseRatio(%q, %d, %d) error = %v, want %v", test.value, test.minimum, test.maximum, err, test.wantError)
			}
			if got != test.want {
				t.Errorf("ParseRatio(%q, %d, %d) = %d, want %d", test.value, test.minimum, test.maximum, got, test.want)
			}
		})
	}
}

func TestFormatRatio(t *testing.T) {
	tests := []struct {
		ratio money.BasisPoints
		want  string
	}{
		{ratio: 4000, want: "0.4000"},
		{ratio: 0, want: "0.0000"},
		{ratio: 1, want: "0.0001"},
		{ratio: 10_000, want: "1.0000"},
		{ratio: 15_000, want: "1.5000"},
		{ratio: -2500, want: "-0.2500"},
	}
	for _, test := range tests {
		if got := money.FormatRatio(test.ratio); got != test.want {
			t.Errorf("FormatRatio(%d) = %q, want %q", test.ratio, got, test.want)
		}
	}
}

func TestParseSignalRatio(t *testing.T) {
	tests := []struct {
		value     string
		want      float64
		wantError error
	}{
		{value: "0.40", want: 0.4},
		{value: "-0.25", want: -0.25},
		{value: "1.5", want: 1.5},
		{value: "0", want: 0},
		{value: "0.0001", want: 0.0001},
		{value: "inf", want: math.Inf(1)},
		{value: "-inf", want: math.Inf(-1)},
		{value: "0.12345", wantError: money.ErrInvalidRatio},
		{value: "+inf", wantError: money.ErrInvalidRatio},
		{value: "-Inf", wantError: money.ErrInvalidRatio},
		{value: "infinity", wantError: money.ErrInvalidRatio},
		{value: "inf ", wantError: money.ErrInvalidRatio},
	}
	for _, test := range tests {
		t.Run(strconv.Quote(test.value), func(t *testing.T) {
			got, err := money.ParseSignalRatio(test.value)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ParseSignalRatio(%q) error = %v, want %v", test.value, err, test.wantError)
			}
			if got != test.want {
				t.Errorf("ParseSignalRatio(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestFormatSignal(t *testing.T) {
	tests := []struct {
		signal float64
		want   string
	}{
		{signal: 0.4, want: "0.4000"},
		{signal: -0.25, want: "-0.2500"},
		{signal: 0.12346, want: "0.1235"},
		{signal: -1.23456, want: "-1.2346"},
		{signal: 1234.5, want: "1234.5000"},
		{signal: 0, want: "0.0000"},
		{signal: math.Copysign(0, -1), want: "0.0000"},
		{signal: -0.00001, want: "0.0000"},
		{signal: math.Inf(1), want: "inf"},
		{signal: math.Inf(-1), want: "-inf"},
	}
	for _, test := range tests {
		if got := money.FormatSignal(test.signal); got != test.want {
			t.Errorf("FormatSignal(%v) = %q, want %q", test.signal, got, test.want)
		}
	}
}
