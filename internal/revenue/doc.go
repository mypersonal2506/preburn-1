// Package revenue records the revenue of each customer and serves the revenue
// API.
//
// A revenue entry is an amount of one kind for one customer period. The amount
// is never negative. Subscription and adjustment entries add it to net
// revenue, and stripe_fee, refund and credit_note entries subtract it. A
// period may be empty, as for a one-time line, and lasts at most 400 days.
// Each entry comes from a source: api for POST /api/v1/revenue, stripe for the
// Stripe connector and import for data imports. Its source reference is unique
// per environment, source and kind, so recording an entry again changes
// nothing and returns the first one as a duplicate.
//
// Service.Record creates the customer an entry names when it is missing, and
// refreshes the customer's period rollups in the transaction that inserts the
// entry. After the commit it removes the customer from the customer state
// cache of its own process and publishes the customer invalidation for the
// other processes, so the next check sees the new net revenue.
package revenue
