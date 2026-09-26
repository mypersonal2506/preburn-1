import type * as z from "zod";
import { zListDashboardDecisionsQuery } from "@/client/zod.gen";
import type { DecisionEvent } from "@/lib/use-event-stream";

/**
 * The filters of the decisions list as URL search params, named like the
 * list endpoint's query parameters: outcome, customer_id, feature and
 * policy_id, each optional.
 */
export const decisionFiltersSchema = zListDashboardDecisionsQuery.pick({
	outcome: true,
	customer_id: true,
	feature: true,
	policy_id: true,
});

/** The filters of the decisions list. An unset filter keeps every decision. */
export type DecisionFilters = z.infer<typeof decisionFiltersSchema>;

/**
 * Tells whether the decisions list with filters keeps decision: its outcome,
 * customer, feature and matched policy equal every filter that is set.
 */
export function matchesDecisionFilters(
	decision: DecisionEvent,
	filters: DecisionFilters,
): boolean {
	return (
		(filters.outcome === undefined || decision.outcome === filters.outcome) &&
		(filters.customer_id === undefined ||
			decision.customer_id === filters.customer_id) &&
		(filters.feature === undefined || decision.feature === filters.feature) &&
		(filters.policy_id === undefined ||
			decision.matched_policy_id === filters.policy_id)
	);
}
