import {
	type LinkOptions,
	linkOptions,
	type RegisteredRouter,
} from "@tanstack/react-router";
import type { AttentionResponse } from "@/client";
import { formatCount, formatPace } from "@/lib/format";

/** A link from the attention banner to the list behind its message. */
export interface AttentionAction {
	label: string;
	link: LinkOptions<RegisteredRouter, string, string>;
}

/** The attention banner's message: a title, a muted line and an action. */
export interface AttentionMessage {
	title: string;
	description: string;
	action: AttentionAction | null;
}

const ATTENTION_PACE_THRESHOLD = "2.0000";

/**
 * Picks the strongest message of the overview's attention counts, in the
 * order of spec section 22.5: plans below target, customers above
 * ATTENTION_PACE_THRESHOLD pace, reports the SDK dropped, then requests
 * without a price. Customers above pace link to the customer list sorted by
 * pace, highest first. Returns null when every count is zero.
 */
export function strongestAttention(
	attention: AttentionResponse,
): AttentionMessage | null {
	if (attention.plans_below_target > 0) {
		return {
			title: `${countOf(attention.plans_below_target, "plan", "plans")} below target`,
			description: "Margin is below the plan target.",
			action: { label: "View plans", link: linkOptions({ to: "/plans" }) },
		};
	}
	if (attention.customers_above_pace > 0) {
		return {
			title: `${countOf(attention.customers_above_pace, "customer", "customers")} above ${formatPace(ATTENTION_PACE_THRESHOLD)} pace`,
			description: "Cost is running ahead of the allowance.",
			action: {
				label: "View customers",
				link: linkOptions({
					to: "/customers",
					search: { sort: "pace", direction: "descending" },
				}),
			},
		};
	}
	if (attention.dropped_reports > 0) {
		return {
			title: `${countOf(attention.dropped_reports, "report", "reports")} dropped by the SDK`,
			description: "In the last 7 days.",
			action: null,
		};
	}
	if (attention.uncosted_requests > 0) {
		return {
			title: `${countOf(attention.uncosted_requests, "request", "requests")} without a price`,
			description: "They count as zero cost until priced.",
			action: null,
		};
	}
	return null;
}

function countOf(count: number, singular: string, plural: string): string {
	return `${formatCount(count)} ${count === 1 ? singular : plural}`;
}
