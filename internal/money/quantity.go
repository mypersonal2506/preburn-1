package money

import (
	"fmt"
	"math"
	"strings"
)

const (
	quantityDecimals = 6
	microsPerUnit    = 1_000_000
)

// Quantity is a usage quantity in micro-units. One token is 1,000,000
// micro-units and 8.5 seconds is 8,500,000.
type Quantity int64

// ParseQuantity parses an API quantity: 1 to 9 integer digits without leading
// zeros and an optional dot followed by 1 to 6 decimals, such as "8.5". Any
// other form, including a sign, returns ErrInvalidQuantity.
func ParseQuantity(value string) (Quantity, error) {
	micros, valid := parseUnsignedDecimal(value, quantityDecimals)
	if !valid {
		return 0, fmt.Errorf("%w %q", ErrInvalidQuantity, value)
	}
	return Quantity(micros), nil
}

// FormatQuantity writes quantity in whole units without trailing zeros or a
// trailing dot, such as "8.5", "8" or "0.000001".
func FormatQuantity(quantity Quantity) string {
	return strings.TrimSuffix(strings.TrimRight(formatDecimal(int64(quantity), quantityDecimals), "0"), ".")
}

// RoundUpToIncrement rounds quantity toward positive infinity to the nearest
// multiple of increment, so 8.2 seconds with a 1 second increment becomes 9
// seconds. It returns ErrInvalidIncrement when increment is below 1 and
// ErrOverflow when the result does not fit int64.
func RoundUpToIncrement(quantity, increment Quantity) (Quantity, error) {
	if increment < 1 {
		return 0, fmt.Errorf("%w %s", ErrInvalidIncrement, FormatQuantity(increment))
	}
	remainder := quantity % increment
	switch {
	case remainder == 0:
		return quantity, nil
	case remainder < 0:
		return quantity - remainder, nil
	}
	gap := increment - remainder
	if quantity > math.MaxInt64-gap {
		return 0, fmt.Errorf("%w: %s rounded up to a multiple of %s", ErrOverflow, FormatQuantity(quantity), FormatQuantity(increment))
	}
	return quantity + gap, nil
}
