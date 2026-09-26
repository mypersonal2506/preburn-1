// Package plans holds the pricing plans of each environment and their
// reservation hold times.
//
// A plan sets the AI cost allowance of its customers per period in one of two
// modes. In margin_target mode the allowance is the period's net revenue times
// one minus the target margin. In fixed_allowance mode it is a fixed amount,
// and the stored target margin is kept for a later switch back. A customer
// without a plan gets the default plan of its environment. Hold times set,
// per feature, how long a check reserves cost before the reservation expires.
//
// Plans are archived, never deleted. The default plan of an environment is
// never archived. Every change publishes the plan invalidation.
package plans
