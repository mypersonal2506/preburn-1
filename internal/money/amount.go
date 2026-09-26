package money

import (
	"fmt"
	"math"
)

const (
	amountDecimals = 9
	nanosPerCent   = 10_000_000
)

// Amount is a money amount in nano-USD. One USD is 1,000,000,000 nanos.
type Amount int64

// ParseAmount parses an API amount: an optional minus sign, 1 to 9 integer
// digits without leading zeros, and an optional dot followed by 1 to 9
// decimals, such as "12.5" or "-0.000000001". Any other form, including a
// plus sign, an exponent or surrounding whitespace, returns ErrInvalidAmount.
func ParseAmount(value string) (Amount, error) {
	nanos, valid := parseSignedDecimal(value, amountDecimals)
	if !valid {
		return 0, fmt.Errorf("%w %q", ErrInvalidAmount, value)
	}
	return Amount(nanos), nil
}

// ParseNonNegativeAmount parses an API amount like ParseAmount, except that a
// minus sign returns ErrInvalidAmount.
func ParseNonNegativeAmount(value string) (Amount, error) {
	nanos, valid := parseUnsignedDecimal(value, amountDecimals)
	if !valid {
		return 0, fmt.Errorf("%w %q", ErrInvalidAmount, value)
	}
	return Amount(nanos), nil
}

// FormatAmount writes amount in USD with exactly 9 decimals, such as
// "12.500000000" or "-0.000000001". It formats every int64, including amounts
// above the 9 integer digits that ParseAmount accepts.
func FormatAmount(amount Amount) string {
	return formatDecimal(int64(amount), amountDecimals)
}

// AmountFromCents converts whole USD cents to an Amount. It returns
// ErrOverflow when the result does not fit int64.
func AmountFromCents(cents int64) (Amount, error) {
	if cents > math.MaxInt64/nanosPerCent || cents < math.MinInt64/nanosPerCent {
		return 0, fmt.Errorf("%w: %d cents", ErrOverflow, cents)
	}
	return Amount(cents * nanosPerCent), nil
}

// Add returns amount plus other. It returns ErrOverflow when the sum does not
// fit int64.
func (amount Amount) Add(other Amount) (Amount, error) {
	sum := amount + other
	if (other > 0 && sum < amount) || (other < 0 && sum > amount) {
		return 0, fmt.Errorf("%w: %s plus %s", ErrOverflow, FormatAmount(amount), FormatAmount(other))
	}
	return sum, nil
}

// Subtract returns amount minus other. It returns ErrOverflow when the
// difference does not fit int64.
func (amount Amount) Subtract(other Amount) (Amount, error) {
	difference := amount - other
	if (other > 0 && difference > amount) || (other < 0 && difference < amount) {
		return 0, fmt.Errorf("%w: %s minus %s", ErrOverflow, FormatAmount(amount), FormatAmount(other))
	}
	return difference, nil
}

// Sum adds amounts in order and returns 0 for none. It returns ErrOverflow as
// soon as the running total leaves the int64 range, even when later amounts
// would bring it back.
func Sum(amounts []Amount) (Amount, error) {
	total := Amount(0)
	for _, amount := range amounts {
		next, err := total.Add(amount)
		if err != nil {
			return 0, err
		}
		total = next
	}
	return total, nil
}
