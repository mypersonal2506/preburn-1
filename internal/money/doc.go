// Package money holds the fixed-point types for money amounts, usage
// quantities, unit prices and ratios, their API string forms, and arithmetic
// that returns ErrOverflow instead of wrapping.
//
// An Amount is int64 nano-USD, so 1 USD is 1,000,000,000. A Quantity is int64
// micro-units, so 1 token is 1,000,000 and 8.5 seconds is 8,500,000. A stored
// ratio is BasisPoints, so 0.40 is 4000. Computed signals stay float64 and
// serialize with 4 decimals.
package money
