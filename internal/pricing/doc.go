// Package pricing prices the usage of AI requests. It holds the pure rating
// engine, the import of the pricing catalog into Postgres, and the service
// and routes that browse the catalog, manage the price overrides of each
// environment, quote requests and list uncosted usage.
//
// A Rule prices one meter of one provider model, such as output_seconds of
// google veo-3, from its effective_from up to its effective_to. Its
// conditions restrict it to requests whose attributes contain every
// condition key with an equal value of the same kind, so a rule with
// resolution 1080p never prices a request without a resolution.
//
// Rate prices each meter of a RatingRequest on its own. It resolves the
// model through the aliases, then picks the standalone override with the
// most condition keys among those in effect at the request time. When none
// applies it picks the catalog rule the same way, preferring the curated
// source over litellm, then the latest effective_from, then the lowest id,
// so the choice never depends on input order. An override is an adjustment
// at a request time when its provider, model, meter and conditions equal a
// catalog rule in effect then, and it replaces that rule's price when the
// rule wins. At any other time it is standalone, so an override of a
// deprecated rule keeps pricing. The billed quantity
// is the usage rounded up to the billing increment, the cost is rounded
// half up once per meter line, and a minimum charge raises a smaller cost
// unless the billed quantity is zero.
// A meter without any applicable rule leaves the request uncosted while its
// other meters keep their prices, unless its quantity is zero, which costs
// nothing under any price.
//
// An override may price in a native unit, such as credits. Its effective
// USD price is the native price times the USD price of one native unit,
// rounded half up to nanos.
//
// ImportCurated and ImportLiteLLM write the catalog rules of the curated
// files and of a LiteLLM price snapshot under the advisory lock
// pricing_import. A rule keeps its window while its price holds. A changed
// price closes the open rule and opens a new one, and a rule the source no
// longer lists is closed and marked deprecated, so past prices stay
// available. A LiteLLM import records the fetch time of its snapshot in
// imported_at and skips a snapshot fetched no later than the newest one
// imported, so preburn migrate never replaces a newer download with the
// vendored snapshot. The pricing_litellm_refresh job downloads the current
// snapshot, imports it and publishes the pricing invalidation when rules
// changed.
//
// Service reads the catalog and the overrides from Postgres. Its
// RuleSetCache keeps the RuleSet of each environment, built from the aliases
// and the rules and overrides open or closed within the last 90 days, for 5
// minutes, and every override change or pricing invalidation clears it. An
// override never names an alias, because creating one stores the model the
// alias names. Overrides are never deleted: ending one sets its
// effective_to, and an ended override no longer changes. A price change to
// an override that has started ends it and opens a successor from now, so
// earlier usage keeps the earlier price.
//
// Creating, changing or ending an override inserts the uncosted_rerate job
// of its environment (UncostedRerateArgs) in the same transaction, at most one
// pending job per environment, and package ledger works it. RateUncosted
// prices an uncosted request at its occurred_at, then each meter still
// missing at the time of the rerate, so a price added later costs the usage
// of the last 90 days at today's price. ListUncosted serves GET
// /api/v1/pricing/uncosted: the uncosted usage of the last 90 days that has
// no correction, one row per provider, model and missing meter with the
// request count and when the latest request ran.
package pricing
