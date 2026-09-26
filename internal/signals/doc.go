// Package signals computes the margin signals of a customer period from the
// customer's state, the period's Redis counter and the rated cost of the
// request being checked.
//
// ResolvePeriod picks the customer's current period: the Stripe subscription
// period, else the latest-starting subscription revenue period containing
// now, else the UTC calendar month. Compute derives every signal from a
// CustomerState and a CounterSnapshot. It never produces NaN: a zero
// allowance or zero revenue has a defined value, and the elapsed fraction is
// floored at MinimumElapsedFraction.
//
// Policies compare the eight signals listed by Names. Money signals are
// money.Amount and compare exactly, ratio signals are float64 and count
// signals are int64. Signals.Value returns one by Name, and reports no value
// for request_estimated_cost when the request has no costed estimate.
//
// The JSON form writes money as amount strings with 9 decimals, ratios with 4
// decimals or as inf and -inf, and counts as integers.
package signals
