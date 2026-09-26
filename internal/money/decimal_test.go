package money_test

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/preburn/preburn/internal/money"
)

var malformedDecimals = []string{
	"",
	"-",
	".",
	"01",
	"00.5",
	"-01",
	"1234567890",
	"-1234567890",
	"0.1234567890",
	" 1",
	"1 ",
	"\t1",
	"1\n",
	"1e3",
	"1E3",
	"1.5e2",
	"+1",
	"--1",
	"1.",
	".5",
	"1..5",
	"1.5.",
	"1,000",
	"1_000",
	"0x10",
	"NaN",
	"Inf",
	"１",
}

func TestParsersRejectMalformedDecimals(t *testing.T) {
	parsers := []struct {
		name      string
		parse     func(value string) error
		wantError error
	}{
		{
			name:      "ParseAmount",
			parse:     func(value string) error { _, err := money.ParseAmount(value); return err },
			wantError: money.ErrInvalidAmount,
		},
		{
			name:      "ParseNonNegativeAmount",
			parse:     func(value string) error { _, err := money.ParseNonNegativeAmount(value); return err },
			wantError: money.ErrInvalidAmount,
		},
		{
			name:      "ParseQuantity",
			parse:     func(value string) error { _, err := money.ParseQuantity(value); return err },
			wantError: money.ErrInvalidQuantity,
		},
		{
			name: "ParseRatio",
			parse: func(value string) error {
				_, err := money.ParseRatio(value, math.MinInt64, math.MaxInt64)
				return err
			},
			wantError: money.ErrInvalidRatio,
		},
		{
			name:      "ParseSignalRatio",
			parse:     func(value string) error { _, err := money.ParseSignalRatio(value); return err },
			wantError: money.ErrInvalidRatio,
		},
	}
	for _, parser := range parsers {
		for _, value := range malformedDecimals {
			t.Run(parser.name+"/"+strconv.Quote(value), func(t *testing.T) {
				if err := parser.parse(value); !errors.Is(err, parser.wantError) {
					t.Errorf("%s(%q) error = %v, want %v", parser.name, value, err, parser.wantError)
				}
			})
		}
	}
}
