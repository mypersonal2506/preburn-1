// Package ledger keeps the usage record of each customer and what Preburn
// derives from it: per-period rollups, usage estimates and the corrections
// that price uncosted usage.
//
// A ledger entry records the rated usage of one request in one customer
// period. InsertEntry stores it once per environment and idempotency key:
// decision:<decision UUID> for the usage of a decision, the client's UUID for
// usage that ran without one. Entries store usage as a JSON object that maps
// each meter to its quantity in micro-units, a JSON integer, and the cost
// breakdown as one line per priced or missing meter. The cost is null while
// a meter has no price.
//
// A period rollup sums one customer billing period: net revenue, ledger cost,
// the count of uncosted ledger entries and the count of decisions by outcome.
// Billing periods are the non-empty periods of subscription revenue entries
// and of ledger entries, and rollups are keyed by their start. A revenue entry
// belongs to the latest-starting billing period that contains its period
// start, so prorations and zero-length lines fold into the period around them.
// Ledger cost and decisions belong to the rollup whose start equals their
// period start. RefreshRollups recomputes the rollups around one moment inside
// the transaction of the write that changed them. Rollup refresh jobs
// (RollupRefreshArgs) do the same after usage reports, at most one pending job
// per customer period.
//
// The hourly usage estimates refresh job (UsageEstimatesRefreshArgs) replaces
// the usage estimates of each environment with the 95th percentile quantity
// of every feature, provider, model and meter that has at least 50 samples in
// the last 30 days of ledger entries, correction entries excluded.
//
// The uncosted_rerate job (UncostedRerateWorker), which an override change
// inserts, re-rates the uncosted entries of the last 90 days that have no
// correction. Each entry that becomes costed gets one correction entry with
// correction_of set and the idempotency key correction:<entry UUID>, the
// rollups of its period are refreshed, its cost is added to the counter of a
// period that has not ended, and its customer's cached state is invalidated.
package ledger
