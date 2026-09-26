// Package customers holds the customers of each environment and their
// users.
//
// A customer is known by its external id, the id the operator's own system
// gives it: 1 to 128 letters, digits or the characters . _ : @ -. External
// ids are unique within an environment and match with letter case. The
// runtime route PUT /api/v1/customers/{external_id} creates a customer or
// replaces its display name, plan and metadata. A customer without a plan
// follows the default plan of its environment. Checks create the customers
// and customer users they name through Ensure and EnsureUser, which are safe
// to call concurrently for the same external id.
//
// The admin routes GET /api/v1/customers and GET
// /api/v1/customers/{external_id} return customers with the signals of their
// current period, computed from the customer state of package customerstate
// and the Redis period counter that a CounterReader reads, with nothing
// requested. The list pages newest first, keeps the customers whose external
// id starts with a search text in any letter case, and loads the states of a
// page in one batch.
//
// Cache keeps customers resolved by external id, and the customer users it
// ensured, in process memory for 30 seconds, so a check that names a known
// customer and user writes nothing to Postgres. A lookup that overlaps a
// clear of that cache does not cache its result. Upsert clears the cache in
// the upserting process and publishes the customer invalidation, which clears
// it in every other process that subscribes Cache.Invalidate.
package customers
