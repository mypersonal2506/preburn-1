package signals

import "time"

// Period is a customer period, from Start inclusive to End exclusive. End is
// after Start.
type Period struct {
	// Start is the first instant of the period.
	Start time.Time
	// End is the first instant after the period.
	End time.Time
}

// ResolvePeriod returns the current period of a customer at now, the first
// that applies of subscriptionPeriod, subscriptionRevenuePeriod and the UTC
// calendar month containing now. subscriptionPeriod is the current period of
// the customer's active Stripe subscription, nil until the Stripe connector
// exists. subscriptionRevenuePeriod is the period of the latest-starting
// subscription revenue entry whose period contains now, or nil when there is
// none.
func ResolvePeriod(now time.Time, subscriptionPeriod *Period, subscriptionRevenuePeriod *Period) Period {
	if subscriptionPeriod != nil {
		return *subscriptionPeriod
	}
	if subscriptionRevenuePeriod != nil {
		return *subscriptionRevenuePeriod
	}
	year, month, _ := now.UTC().Date()
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	return Period{Start: start, End: start.AddDate(0, 1, 0)}
}

// Contains reports whether moment is at or after Start and before End.
func (period Period) Contains(moment time.Time) bool {
	return !moment.Before(period.Start) && moment.Before(period.End)
}
