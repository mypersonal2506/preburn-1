// Package decisions holds the decision lifecycle: the check that decides a
// provider request and reserves its cost, the reports that record its usage,
// the releases and expiry that free its reservation, the Redis counters that
// hold the reservations, and the jobs that reconcile and rebuild the counters
// and delete old decisions.
//
// CheckService.Check serves POST /api/v1/check. It resolves the customer,
// loads the customer state, rates the request, reads the period counter,
// computes the signals and resolves the active policies. The outcome allows
// the request, routes it to the first route chain target that can be priced,
// caps it with parameter overrides, or denies it. An allowed, routed or
// capped decision reserves its cost with the reserve script, which denies it
// with reason hard_limit_reached when an allowance or cap limit ceiling would
// be exceeded. A denied decision only counts. Every decision is stored in
// Postgres and appended to the decision stream that feeds the dashboard. A
// Redis step that fails or takes more than 150 ms, or finds the counters not
// ready, answers 503 counters_unavailable, and a Postgres failure answers 503
// database_unavailable. A step that fails because the client closed the
// request answers 499 client_closed_request and logs no error.
//
// ReportService serves POST /api/v1/report, POST /api/v1/reports and POST
// /api/v1/release. A server report rates the usage of a checked decision,
// stores it in the ledger once per decision, marks the decision settled and
// moves its cost from reserved to settled in the counter. A fallback report
// records usage that ran without a decision under the client's idempotency
// key and counts it. A denied decision reserved nothing and answers 409
// decision_not_reportable. A batch holds at most 500 reports, each answered
// on its own. A release frees a reservation before its hold time ends, and
// releasing again changes nothing. Releases, expiries and denies enqueue the
// rollup refresh of their customer period, so decisions that are never
// reported still reach the period rollups. The Preburn-Dropped-Reports header adds
// the reports the SDK dropped to the day's dropped report counter.
//
// Counters keeps the customer period counters and reservations of the Redis
// contract. Checks, reports and releases change them through the reserve,
// settle, release and settle_unreserved Lua scripts, which run atomically and
// use integer arithmetic only. Every one of them refuses to run while the
// counters_ready marker is missing. A check or a ledger entry registers
// itself as a pending change of its counter until both Postgres and Redis
// hold it. A Redis failure after a report or release commits only delays the
// counter, which counters_reconcile repairs.
//
// The reservations_expire job releases every 5 seconds the reservations
// whose hold time passed. The counters_reconcile job repairs every 60 seconds
// the counters that drifted from the decisions and ledger entries in
// Postgres and leaves a counter alone while it has a pending change, so it
// never counts a change twice. CounterBootstrap rebuilds every live counter
// when the counters_ready marker is missing, as after a Redis restart without
// persistence, and a check that finds the marker missing asks it to. The
// decision_retention job deletes daily at 03:00 UTC the decisions older than
// PREBURN_DECISION_RETENTION_DAYS and keeps their ledger entries.
package decisions
