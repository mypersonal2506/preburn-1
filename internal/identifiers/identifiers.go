package identifiers

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Prefix names the entity type of an exposed identifier, such as cust in
// cust_01jbvagescfn78y0938nkrkayd.
type Prefix string

const (
	// PrefixMember identifies a member.
	PrefixMember Prefix = "mem"
	// PrefixMemberLink identifies a member link.
	PrefixMemberLink Prefix = "mlk"
	// PrefixAPIKey identifies an API key row, which is separate from the key secret.
	PrefixAPIKey Prefix = "key"
	// PrefixCustomer identifies a customer.
	PrefixCustomer Prefix = "cust"
	// PrefixCustomerUser identifies a customer user.
	PrefixCustomerUser Prefix = "cuser"
	// PrefixPlan identifies a plan.
	PrefixPlan Prefix = "pln"
	// PrefixPricingRule identifies a pricing rule.
	PrefixPricingRule Prefix = "prc"
	// PrefixPricingOverride identifies a pricing override.
	PrefixPricingOverride Prefix = "pro"
	// PrefixPolicy identifies a policy.
	PrefixPolicy Prefix = "pol"
	// PrefixDecision identifies a decision.
	PrefixDecision Prefix = "dec"
	// PrefixLedgerEntry identifies a ledger entry.
	PrefixLedgerEntry Prefix = "led"
	// PrefixRevenueEntry identifies a revenue entry.
	PrefixRevenueEntry Prefix = "rev"
	// PrefixAlert identifies an alert.
	PrefixAlert Prefix = "alr"
	// PrefixWebhookEndpoint identifies a webhook endpoint.
	PrefixWebhookEndpoint Prefix = "whe"
	// PrefixWebhookDelivery identifies a webhook delivery.
	PrefixWebhookDelivery Prefix = "whd"
	// PrefixWebhookEvent identifies a webhook event.
	PrefixWebhookEvent Prefix = "evt"
	// PrefixSimulation identifies a simulation.
	PrefixSimulation Prefix = "sim"
	// PrefixImport identifies an import.
	PrefixImport Prefix = "imp"
)

const (
	alphabet          = "0123456789abcdefghjkmnpqrstvwxyz"
	separator         = "_"
	bodyLength        = 26
	bitsPerCharacter  = 5
	bitsPerWord       = 64
	characterMask     = 1<<bitsPerCharacter - 1
	maximumFirstDigit = 7
)

// ErrInvalidIdentifier reports an exposed identifier that Decode rejects: a
// wrong prefix, a body that is not 26 characters of the lowercase Crockford
// base32 alphabet, or a body whose value does not fit in 128 bits.
var ErrInvalidIdentifier = errors.New("invalid identifier")

// New returns a new UUID version 7. Values from one process increase strictly
// in creation order, even within the same millisecond. The random bits come
// from crypto/rand, which stops the process instead of returning an error, so
// New never fails.
func New() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// Encode returns the exposed form of value: prefix, an underscore and the 26
// character lowercase Crockford base32 form of the 128-bit value. Encoded
// values of one prefix sort in the same order as their UUIDs.
func Encode(prefix Prefix, value uuid.UUID) string {
	high := binary.BigEndian.Uint64(value[:8])
	low := binary.BigEndian.Uint64(value[8:])
	var body [bodyLength]byte
	for position := bodyLength - 1; position >= 0; position-- {
		body[position] = alphabet[low&characterMask]
		low = low>>bitsPerCharacter | high<<(bitsPerWord-bitsPerCharacter)
		high >>= bitsPerCharacter
	}
	return string(prefix) + separator + string(body[:])
}

// Decode returns the UUID whose exposed form under prefix is encoded. It
// accepts only the exact form Encode produces, so every UUID has one exposed
// identifier. Any other input returns an error wrapping ErrInvalidIdentifier.
func Decode(prefix Prefix, encoded string) (uuid.UUID, error) {
	body, found := strings.CutPrefix(encoded, string(prefix)+separator)
	if !found {
		return uuid.Nil, fmt.Errorf("%w: prefix want=%s%s", ErrInvalidIdentifier, prefix, separator)
	}
	if len(body) != bodyLength {
		return uuid.Nil, fmt.Errorf("%w: body length=%d want=%d", ErrInvalidIdentifier, len(body), bodyLength)
	}
	if strings.IndexByte(alphabet, body[0]) > maximumFirstDigit {
		return uuid.Nil, fmt.Errorf("%w: value above 128 bits", ErrInvalidIdentifier)
	}
	var high, low uint64
	for position := range bodyLength {
		digit := strings.IndexByte(alphabet, body[position])
		if digit < 0 {
			return uuid.Nil, fmt.Errorf("%w: character outside alphabet position=%d", ErrInvalidIdentifier, position)
		}
		high = high<<bitsPerCharacter | low>>(bitsPerWord-bitsPerCharacter)
		low = low<<bitsPerCharacter | uint64(digit)
	}
	var value uuid.UUID
	binary.BigEndian.PutUint64(value[:8], high)
	binary.BigEndian.PutUint64(value[8:], low)
	return value, nil
}
