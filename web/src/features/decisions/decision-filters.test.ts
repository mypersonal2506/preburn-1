import { expect, test } from "vitest";
import {
	type DecisionFilters,
	matchesDecisionFilters,
} from "@/features/decisions/decision-filters";
import {
	routedDecision,
	streamedDecision,
} from "@/features/decisions/decisions-test-support";

const routedEvent = streamedDecision(routedDecision);

test.each<DecisionFilters>([
	{},
	{ outcome: "route" },
	{ customer_id: routedDecision.customer_id },
	{ feature: "text_to_video" },
	{ policy_id: "pol_01jbvagescfn78y0938nkrkayd" },
	{
		outcome: "route",
		customer_id: routedDecision.customer_id,
		feature: "text_to_video",
		policy_id: "pol_01jbvagescfn78y0938nkrkayd",
	},
])("a decision matches filters %o it passes", (filters) => {
	expect(matchesDecisionFilters(routedEvent, filters)).toBe(true);
});

test.each<DecisionFilters>([
	{ outcome: "deny" },
	{ customer_id: "cust_01jbvagescfn78y0938nkrkaze" },
	{ feature: "image_generation" },
	{ policy_id: "pol_01jbvagescfn78y0938nkrkaze" },
	{ outcome: "route", feature: "image_generation" },
])("a decision fails filters %o", (filters) => {
	expect(matchesDecisionFilters(routedEvent, filters)).toBe(false);
});

test("a decision no policy decided fails a policy filter", () => {
	expect(
		matchesDecisionFilters(
			{ ...routedEvent, matched_policy_id: null },
			{ policy_id: "pol_01jbvagescfn78y0938nkrkayd" },
		),
	).toBe(false);
});
