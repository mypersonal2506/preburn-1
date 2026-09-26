import { expect, test } from "vitest";
import type { AttentionResponse } from "@/client";
import {
	type AttentionMessage,
	strongestAttention,
} from "@/features/overview/overview-attention";

const NO_ATTENTION: AttentionResponse = {
	plans_below_target: 0,
	customers_above_pace: 0,
	dropped_reports: 0,
	uncosted_requests: 0,
};

test.each<[string, AttentionResponse, AttentionMessage | null]>([
	["nothing needs attention", NO_ATTENTION, null],
	[
		"plans below target win over every other count",
		{
			plans_below_target: 1,
			customers_above_pace: 3,
			dropped_reports: 12,
			uncosted_requests: 5,
		},
		{
			title: "1 plan below target",
			description: "Margin is below the plan target.",
			action: { label: "View plans", link: { to: "/plans" } },
		},
	],
	[
		"customers above pace come next",
		{
			...NO_ATTENTION,
			customers_above_pace: 3,
			dropped_reports: 12,
			uncosted_requests: 5,
		},
		{
			title: "3 customers above 2.0x pace",
			description: "Cost is running ahead of the allowance.",
			action: {
				label: "View customers",
				link: {
					to: "/customers",
					search: { sort: "pace", direction: "descending" },
				},
			},
		},
	],
	[
		"dropped reports come before uncosted requests",
		{ ...NO_ATTENTION, dropped_reports: 1, uncosted_requests: 5 },
		{
			title: "1 report dropped by the SDK",
			description: "In the last 7 days.",
			action: null,
		},
	],
	[
		"uncosted requests come last",
		{ ...NO_ATTENTION, uncosted_requests: 1200 },
		{
			title: "1,200 requests without a price",
			description: "They count as zero cost until priced.",
			action: null,
		},
	],
])("%s", (_case, attention, expected) => {
	expect(strongestAttention(attention)).toEqual(expected);
});
