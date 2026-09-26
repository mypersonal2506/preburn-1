package money

import (
	"fmt"
	"math/big"
)

// UnitPrice is a price of Nanos nano-USD per UnitQuantity whole units. $0.15
// per 1M tokens is {Nanos: 150000000, UnitQuantity: 1000000} and $0.40 per
// second is {Nanos: 400000000, UnitQuantity: 1}.
type UnitPrice struct {
	// Nanos is the price of UnitQuantity whole units in nano-USD.
	Nanos Amount
	// UnitQuantity is the number of whole units that Nanos pays for, 1 or more.
	UnitQuantity int64
}

// ParseUnitPrice parses an API price. price is a non-negative amount in USD in
// the ParseNonNegativeAmount form and unitQuantity is 1 or more. It returns
// ErrInvalidAmount or ErrInvalidUnitQuantity.
func ParseUnitPrice(price string, unitQuantity int64) (UnitPrice, error) {
	nanos, err := ParseNonNegativeAmount(price)
	if err != nil {
		return UnitPrice{}, err
	}
	if unitQuantity < 1 {
		return UnitPrice{}, fmt.Errorf("%w %d", ErrInvalidUnitQuantity, unitQuantity)
	}
	return UnitPrice{Nanos: nanos, UnitQuantity: unitQuantity}, nil
}

// Rate returns the cost of quantity at price: quantity times Nanos divided by
// UnitQuantity times 1,000,000, rounded half up toward positive infinity. The
// arithmetic runs in math/big, so only the result has to fit int64. A result
// outside int64 returns ErrOverflow and a UnitQuantity below 1 returns
// ErrInvalidUnitQuantity.
func Rate(quantity Quantity, price UnitPrice) (Amount, error) {
	if price.UnitQuantity < 1 {
		return 0, fmt.Errorf("%w %d", ErrInvalidUnitQuantity, price.UnitQuantity)
	}
	product := new(big.Int).Mul(big.NewInt(int64(quantity)), big.NewInt(int64(price.Nanos)))
	divisor := new(big.Int).Mul(big.NewInt(price.UnitQuantity), big.NewInt(microsPerUnit))
	cost, remainder := new(big.Int).DivMod(product, divisor, new(big.Int))
	if doubledRemainder := new(big.Int).Lsh(remainder, 1); doubledRemainder.Cmp(divisor) >= 0 {
		cost.Add(cost, big.NewInt(1))
	}
	if !cost.IsInt64() {
		return 0, fmt.Errorf("%w: %s units at %s per %d units", ErrOverflow, FormatQuantity(quantity), FormatAmount(price.Nanos), price.UnitQuantity)
	}
	return Amount(cost.Int64()), nil
}
