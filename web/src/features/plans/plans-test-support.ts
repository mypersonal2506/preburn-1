import type {
	PageBodyKnownFeatureResponse,
	PageBodyPlanResponse,
	PlanResponse,
	PolicyResponse,
} from "@/client";

/** A margin target plan keeping 40% with one custom hold time. */
export const creatorPlan: PlanResponse = {
	id: "pln_01jbvagescfn78y0938nkrkayg",
	name: "Creator",
	mode: "margin_target",
	target_margin: "0.4000",
	allowance: null,
	hold_times: { text_to_video: 600 },
	status: "active",
	customer_count: 12,
	created_at: "2026-09-01T00:00:00Z",
};

/** A fixed allowance plan of $2.00 that keeps a stored 40% target margin. */
export const freePlan: PlanResponse = {
	id: "pln_01jbvagescfn78y0938nkrkayh",
	name: "Free",
	mode: "fixed_allowance",
	target_margin: "0.4000",
	allowance: "2.000000000",
	hold_times: {},
	status: "active",
	customer_count: 1204,
	created_at: "2026-09-02T00:00:00Z",
};

/** An archived margin target plan. */
export const legacyPlan: PlanResponse = {
	id: "pln_01jbvagescfn78y0938nkrkayj",
	name: "Legacy",
	mode: "margin_target",
	target_margin: "0.2500",
	allowance: null,
	hold_times: {},
	status: "archived",
	customer_count: 0,
	created_at: "2026-08-01T00:00:00Z",
};

/** An active policy denying Creator customers above 2.0x pace. */
export const creatorDenyPolicy: PolicyResponse = {
	id: "pol_01jbvagescfn78y0938nkrka11",
	name: "Creator deny",
	level: "plan",
	plan_id: creatorPlan.id,
	customer_id: null,
	feature: null,
	when: { all: [{ signal: "pace", operator: "gt", value: "2.0000" }] },
	action: { outcome: "deny", route_chain: null, overrides: null, limit: null },
	enforcement: "soft",
	on_unreachable: "allow",
	on_uncosted: "allow",
	status: "active",
	version: 1,
	created_at: "2026-09-20T10:00:00Z",
	updated_at: "2026-09-20T10:00:00Z",
};

/** An archived policy of creatorPlan. */
export const archivedPlanPolicy: PolicyResponse = {
	...creatorDenyPolicy,
	id: "pol_01jbvagescfn78y0938nkrka15",
	name: "Old deny",
	status: "archived",
};

/** The features the environment knows, for the hold time pickers. */
export const knownFeatures: PageBodyKnownFeatureResponse = {
	items: [
		{ feature: "chat", sources: ["decisions"] },
		{ feature: "text_to_video", sources: ["decisions", "plan_hold_times"] },
	],
	next_cursor: null,
};

/** One page of plans holding plans, the last page unless nextCursor is set. */
export function planPage(
	plans: PlanResponse[],
	nextCursor: string | null = null,
): PageBodyPlanResponse {
	return { items: plans, next_cursor: nextCursor };
}

/** The route of a request for plan, such as `PATCH /api/v1/plans/{plan_id}`. */
export function planRoute(method: "GET" | "PATCH", plan: PlanResponse): string {
	return `${method} /api/v1/plans/${plan.id}`;
}
