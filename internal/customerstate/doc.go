// Package customerstate loads the signals.CustomerState of customers from
// Postgres: the effective plan, the current period and the net revenue
// attributed to it.
//
// A customer's plan is its own plan, else the default plan of its
// environment, and sets the allowance and the hold times. A customer with
// neither has a fixed allowance of 0 and no hold times. The current period
// comes from signals.ResolvePeriod, and the net revenue from the period
// rollup that starts with the current period, 0 when there is none.
//
// Cache keeps loaded states for 30 seconds, and never past the end of their
// period. The customer invalidation removes one customer, and the plan and
// settings invalidations empty the environment.
package customerstate
