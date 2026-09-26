package money

import (
	"strconv"
	"strings"
)

const maximumIntegerDigits = 9

func parseSignedDecimal(value string, decimals int) (int64, bool) {
	magnitude, negative := strings.CutPrefix(value, "-")
	scaled, valid := parseUnsignedDecimal(magnitude, decimals)
	if negative {
		return -scaled, valid
	}
	return scaled, valid
}

func parseUnsignedDecimal(value string, decimals int) (int64, bool) {
	whole, fraction, hasFraction := strings.Cut(value, ".")
	leadingZero := len(whole) > 1 && whole[0] == '0'
	if leadingZero || !isDigits(whole, maximumIntegerDigits) || hasFraction && !isDigits(fraction, decimals) {
		return 0, false
	}
	scaled, err := strconv.ParseInt(whole+fraction+strings.Repeat("0", decimals-len(fraction)), 10, 64)
	return scaled, err == nil
}

func isDigits(value string, maximumLength int) bool {
	if value == "" || len(value) > maximumLength {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func formatDecimal(scaled int64, decimals int) string {
	digits, negative := strings.CutPrefix(strconv.FormatInt(scaled, 10), "-")
	if len(digits) <= decimals {
		digits = strings.Repeat("0", decimals+1-len(digits)) + digits
	}
	wholeLength := len(digits) - decimals
	formatted := digits[:wholeLength] + "." + digits[wholeLength:]
	if negative {
		return "-" + formatted
	}
	return formatted
}
