package pricing

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/money"
)

const (
	// MeterInputTokens counts prompt tokens.
	MeterInputTokens Meter = "input_tokens"
	// MeterCachedInputTokens counts prompt tokens read from the provider's cache.
	MeterCachedInputTokens Meter = "cached_input_tokens" //nolint:gosec // G101: a meter name, not a credential.
	// MeterCacheWriteInputTokens counts prompt tokens written to the provider's cache.
	MeterCacheWriteInputTokens Meter = "cache_write_input_tokens" //nolint:gosec // G101: a meter name, not a credential.
	// MeterOutputTokens counts generated tokens.
	MeterOutputTokens Meter = "output_tokens"
	// MeterReasoningTokens counts reasoning tokens, priced only where a
	// provider prices them differently from output tokens.
	MeterReasoningTokens Meter = "reasoning_tokens"
	// MeterInputAudioTokens counts audio prompt tokens.
	MeterInputAudioTokens Meter = "input_audio_tokens" //nolint:gosec // G101: a meter name, not a credential.
	// MeterOutputAudioTokens counts generated audio tokens.
	MeterOutputAudioTokens Meter = "output_audio_tokens"
	// MeterInputSeconds counts seconds of input media.
	MeterInputSeconds Meter = "input_seconds"
	// MeterOutputSeconds counts seconds of generated media.
	MeterOutputSeconds Meter = "output_seconds"
	// MeterGPUSeconds counts seconds of GPU time.
	MeterGPUSeconds Meter = "gpu_seconds"
	// MeterCharacters counts characters, such as text sent to speech synthesis.
	MeterCharacters Meter = "characters"
	// MeterImages counts generated images.
	MeterImages Meter = "images"
	// MeterMegapixels counts megapixels of generated images.
	MeterMegapixels Meter = "megapixels"
	// MeterAudioMinutes counts minutes of audio.
	MeterAudioMinutes Meter = "audio_minutes"
	// MeterRequests counts requests.
	MeterRequests Meter = "requests"
	// MeterSearchRequests counts web search requests.
	MeterSearchRequests Meter = "search_requests"
)

const (
	// CostStatusCosted marks a request whose every meter line is priced.
	CostStatusCosted CostStatus = "costed"
	// CostStatusUncosted marks a request with at least one meter line that
	// no rule or override prices.
	CostStatusUncosted CostStatus = "uncosted"
)

// Meter is a usage dimension that rules price, one of the 16 constants.
type Meter string

// CostStatus says whether a rated request has a cost.
type CostStatus string

// RatingRequest is the usage of one provider request to price.
type RatingRequest struct {
	// Provider is the provider name, such as openai.
	Provider string
	// Model is the model name or one of its aliases.
	Model string
	// Attributes describe the request, such as its resolution.
	Attributes Attributes
	// Usage is the quantity of each meter the request used.
	Usage map[Meter]money.Quantity
	// OccurredAt picks the rules and overrides in effect.
	OccurredAt time.Time
}

// RatedLine is the price of one meter of a rated request. Exactly one of
// RuleID and OverrideID is set, or Missing is true and UnitPrice and Cost
// are zero.
type RatedLine struct {
	// Meter is the usage dimension of the line.
	Meter Meter
	// Quantity is the billed quantity, the usage rounded up to the billing
	// increment when one applies.
	Quantity money.Quantity
	// UnitPrice is the USD price applied.
	UnitPrice money.UnitPrice
	// Cost is Quantity at UnitPrice rounded half up to nanos, raised to the
	// minimum charge when one applies and Quantity is above zero.
	Cost money.Amount
	// RuleID is the catalog rule that priced the line.
	RuleID *uuid.UUID
	// OverrideID is the override that priced the line, standalone or
	// adjusting the catalog rule that matched.
	OverrideID *uuid.UUID
	// Missing is true when no rule or override prices the meter.
	Missing bool
}

// RatedRequest is the priced usage of a RatingRequest.
type RatedRequest struct {
	// CostStatus is costed when every line is priced and uncosted otherwise.
	CostStatus CostStatus
	// Cost is the sum of the line costs, nil when the request is uncosted.
	Cost *money.Amount
	// Lines holds one line per meter of the usage, ordered by meter name,
	// except the meters with a zero quantity that nothing prices.
	Lines []RatedLine
}

// KeyPrice is the headline USD price of one meter of a model.
type KeyPrice struct {
	// Meter is the usage dimension priced.
	Meter Meter
	// UnitPrice is the USD price.
	UnitPrice money.UnitPrice
}

// ErrUnknownMeter reports a string that names none of the 16 meters.
var ErrUnknownMeter = errors.New("unknown meter")

// ParseMeter returns the Meter named exactly value. Any other value returns
// ErrUnknownMeter.
func ParseMeter(value string) (Meter, error) {
	meter := Meter(value)
	switch meter {
	case MeterInputTokens, MeterCachedInputTokens, MeterCacheWriteInputTokens, MeterOutputTokens,
		MeterReasoningTokens, MeterInputAudioTokens, MeterOutputAudioTokens, MeterInputSeconds,
		MeterOutputSeconds, MeterGPUSeconds, MeterCharacters, MeterImages, MeterMegapixels,
		MeterAudioMinutes, MeterRequests, MeterSearchRequests:
		return meter, nil
	}
	return "", fmt.Errorf("%w %q", ErrUnknownMeter, value)
}

// Rate prices every meter of request.Usage with the overrides and rules of
// ruleSet in effect at request.OccurredAt, as the package documentation
// describes. A meter nothing prices becomes a Missing line and makes the
// request uncosted, unless its quantity is zero: that meter costs nothing
// under any price, so Rate skips it. It returns money.ErrOverflow when a line cost or the
// total does not fit int64, and money.ErrInvalidIncrement or
// money.ErrInvalidUnitQuantity when the price that applies holds an invalid
// billing increment or unit quantity.
func Rate(request RatingRequest, ruleSet *RuleSet) (RatedRequest, error) {
	meters := ruleSet.lookupModel(request.Provider, request.Model)
	rated := RatedRequest{CostStatus: CostStatusCosted, Lines: make([]RatedLine, 0, len(request.Usage))}
	costs := make([]money.Amount, 0, len(request.Usage))
	for _, meter := range slices.Sorted(maps.Keys(request.Usage)) {
		quantity := request.Usage[meter]
		price, priced := meters.selectPrice(meter, request.Attributes, request.OccurredAt)
		if !priced && quantity == 0 {
			continue
		}
		if !priced {
			rated.CostStatus = CostStatusUncosted
			rated.Lines = append(rated.Lines, RatedLine{Meter: meter, Quantity: quantity, Missing: true})
			continue
		}
		line, err := rateLine(meter, quantity, price)
		if err != nil {
			return RatedRequest{}, fmt.Errorf("rate %s: %w", meter, err)
		}
		rated.Lines = append(rated.Lines, line)
		costs = append(costs, line.Cost)
	}
	if rated.CostStatus == CostStatusUncosted {
		return rated, nil
	}
	cost, err := money.Sum(costs)
	if err != nil {
		return RatedRequest{}, fmt.Errorf("sum line costs: %w", err)
	}
	rated.Cost = &cost
	return rated, nil
}

// KeyPrices returns one headline price per meter that ruleSet holds rules or
// overrides for on the provider model, resolved through the aliases. Each is
// the price Rate would apply at the time at to a request with the model's
// default attributes. Meters whose rules the defaults do not match are left
// out. The result is ordered by meter name.
func KeyPrices(provider, model string, defaults Attributes, ruleSet *RuleSet, at time.Time) []KeyPrice {
	meters := ruleSet.lookupModel(provider, model)
	keyPrices := make([]KeyPrice, 0, len(meters))
	for _, meter := range slices.Sorted(maps.Keys(meters)) {
		price, priced := meters.selectPrice(meter, defaults, at)
		if priced {
			keyPrices = append(keyPrices, KeyPrice{Meter: meter, UnitPrice: price.unitPrice})
		}
	}
	return keyPrices
}

func rateLine(meter Meter, quantity money.Quantity, price selectedPrice) (RatedLine, error) {
	billed := quantity
	if price.billingIncrement != nil {
		rounded, err := money.RoundUpToIncrement(quantity, *price.billingIncrement)
		if err != nil {
			return RatedLine{}, err
		}
		billed = rounded
	}
	cost, err := money.Rate(billed, price.unitPrice)
	if err != nil {
		return RatedLine{}, err
	}
	if billed > 0 && price.minimumCharge != nil && cost < *price.minimumCharge {
		cost = *price.minimumCharge
	}
	return RatedLine{
		Meter:      meter,
		Quantity:   billed,
		UnitPrice:  price.unitPrice,
		Cost:       cost,
		RuleID:     price.ruleID,
		OverrideID: price.overrideID,
	}, nil
}
