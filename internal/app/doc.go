// Package app wires the Preburn processes together. New opens the connections
// a process shares and creates the domain services, Serve runs the HTTP
// server of the api process, Work runs the River workers of the worker
// process, and Migrate applies the schema migrations and imports the
// embedded pricing catalog. RegisterRoutes and RegisterJobs are the one place
// where every domain's routes and jobs are registered.
//
// One authenticator serves every route group. A request with an
// Authorization header of the Bearer scheme is resolved as an API key, and
// any other request as a member session, even one that carries a reverse
// proxy's Basic credentials. While the api process serves, the caches of
// resolved API keys, customers, customer states, active policies and pricing
// rule sets follow the invalidations that every process publishes, and at
// start the api process logs a new setup link while setup is pending.
//
// Both processes make the Redis counters ready at start: the first process
// to take the counters_rebuild lock rebuilds them from Postgres when the
// counters_ready marker is missing, and the other waits for it. /readyz
// fails until the marker exists. The api process makes them ready again
// whenever its invalidation subscription reconnects, and the worker process
// before each counters_reconcile run, so a Redis restart without persistence
// is repaired while both keep running. The api process ends the open
// dashboard decision streams when it shuts down.
//
// The worker process runs every job kind of the first slice, each in a trace
// span named after its kind: the periodic member_cleanup,
// reservations_expire, counters_reconcile, usage_estimates_refresh and
// decision_retention jobs, the rollup_refresh and uncosted_rerate jobs that
// reports and override changes insert, and pricing_litellm_refresh when
// PREBURN_PRICING_LITELLM_REFRESH is true.
package app
