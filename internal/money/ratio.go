package money

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	ratioDecimals          = 4
	basisPointsPerUnit     = 10_000
	positiveInfinitySignal = "inf"
	negativeInfinitySignal = "-inf"
)

// BasisPoints is a stored ratio in ten-thousandths, so 0.40 is 4000.
type BasisPoints int64

// ParseRatio parses an API ratio: an optional minus sign, 1 to 9 integer
// digits without leading zeros, and an optional dot followed by 1 to 4
// decimals, such as "0.40". Any other form returns ErrInvalidRatio. A ratio
// below minimum or above maximum, both inclusive, returns ErrRatioOutOfRange.
func ParseRatio(value string, minimum, maximum BasisPoints) (BasisPoints, error) {
	scaled, valid := parseSignedDecimal(value, ratioDecimals)
	if !valid {
		return 0, fmt.Errorf("%w %q", ErrInvalidRatio, value)
	}
	ratio := BasisPoints(scaled)
	if ratio < minimum || ratio > maximum {
		return 0, fmt.Errorf("%w: %s is outside %s to %s", ErrRatioOutOfRange, value, FormatRatio(minimum), FormatRatio(maximum))
	}
	return ratio, nil
}

// FormatRatio writes ratio with exactly 4 decimals, such as "0.4000".
func FormatRatio(ratio BasisPoints) string {
	return formatDecimal(int64(ratio), ratioDecimals)
}

// ParseSignalRatio parses a ratio to compare with computed signals. It
// accepts the ParseRatio form plus "inf" and "-inf", and returns
// ErrInvalidRatio for anything else. The caller checks the range of its field.
func ParseSignalRatio(value string) (float64, error) {
	switch value {
	case positiveInfinitySignal:
		return math.Inf(1), nil
	case negativeInfinitySignal:
		return math.Inf(-1), nil
	}
	scaled, valid := parseSignedDecimal(value, ratioDecimals)
	if !valid {
		return 0, fmt.Errorf("%w %q", ErrInvalidRatio, value)
	}
	return float64(scaled) / basisPointsPerUnit, nil
}

// FormatSignal writes a computed signal rounded to exactly 4 decimals, such as
// "0.4012". Positive and negative infinity write as "inf" and "-inf", and a
// value that rounds to zero writes as "0.0000" without a minus sign. signal
// must not be NaN, which has no API form.
func FormatSignal(signal float64) string {
	switch {
	case math.IsInf(signal, 1):
		return positiveInfinitySignal
	case math.IsInf(signal, -1):
		return negativeInfinitySignal
	}
	formatted := strconv.FormatFloat(signal, 'f', ratioDecimals, 64)
	if strings.Trim(formatted, "-0.") == "" {
		return strings.TrimPrefix(formatted, "-")
	}
	return formatted
}
