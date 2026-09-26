package money

import "errors"

var (
	// ErrInvalidAmount reports a string that is not an API amount.
	ErrInvalidAmount = errors.New("invalid amount")
	// ErrInvalidQuantity reports a string that is not an API quantity.
	ErrInvalidQuantity = errors.New("invalid quantity")
	// ErrInvalidUnitQuantity reports a unit quantity below 1.
	ErrInvalidUnitQuantity = errors.New("invalid unit quantity")
	// ErrInvalidIncrement reports a rounding increment below 1 micro-unit.
	ErrInvalidIncrement = errors.New("invalid increment")
	// ErrInvalidRatio reports a string that is not an API ratio.
	ErrInvalidRatio = errors.New("invalid ratio")
	// ErrRatioOutOfRange reports a well-formed ratio outside the bounds of its field.
	ErrRatioOutOfRange = errors.New("ratio out of range")
	// ErrOverflow reports an arithmetic result that does not fit int64.
	ErrOverflow = errors.New("int64 overflow")
)
