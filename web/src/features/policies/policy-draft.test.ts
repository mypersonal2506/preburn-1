import { describe, expect, test } from "vitest";
import type { PolicyResponse } from "@/client";
import {
	createPolicyBody,
	draftFromPolicy,
	draftProblems,
	type PolicyDraft,
	sameDocument,
	suggestPolicyName,
	unpickedRouteTarget,
	updatePolicyBody,
} from "@/features/policies/policy-draft";

const CUSTOMER = {
	id: "cust_01jbvagescfn78y0938nkrkayd",
	external_id: "acme",
	display_name: "Acme",
};

const routePolicy: PolicyResponse = {
	id: "pol_01jbvagescfn78y0938nkrkayd",
	name: "Heavy video users",
	level: "plan",
	plan_id: "pln_01jbvagescfn78y0938nkrkayd",
	customer_id: null,
	feature: "text_to_video",
	when: { all: [{ signal: "pace", operator: "gt", value: "2.0000" }] },
	action: {
		outcome: "route",
		route_chain: [
			{ provider: "fal_ai", model: "fal-ai/kling-video/v2.5-turbo/pro" },
			{ provider: "fal_ai", model: "fal-ai/veo3.1/lite" },
		],
		overrides: { audio: false, duration: "4s" },
		limit: null,
	},
	enforcement: "soft",
	on_unreachable: "allow",
	on_uncosted: "deny",
	status: "disabled",
	version: 3,
	created_at: "2026-09-20T10:00:00Z",
	updated_at: "2026-09-25T10:00:00Z",
};

const denyDraft: PolicyDraft = {
	name: null,
	level: "everyone",
	plan_id: null,
	customer: null,
	feature: null,
	when: { all: [] },
	action: { outcome: "deny", route_chain: [], overrides: {}, limit: null },
	enforcement: "hard",
	on_unreachable: "deny",
	on_uncosted: "allow",
	status: "active",
};

describe("request bodies", () => {
	test("a stored policy's draft builds its document again", () => {
		const draft = draftFromPolicy(routePolicy, null);

		expect(createPolicyBody(draft, routePolicy.name)).toEqual({
			name: "Heavy video users",
			level: "plan",
			plan_id: "pln_01jbvagescfn78y0938nkrkayd",
			customer_id: null,
			feature: "text_to_video",
			when: { all: [{ signal: "pace", operator: "gt", value: "2.0000" }] },
			action: {
				outcome: "route",
				route_chain: [
					{ provider: "fal_ai", model: "fal-ai/kling-video/v2.5-turbo/pro" },
					{ provider: "fal_ai", model: "fal-ai/veo3.1/lite" },
				],
				overrides: { audio: false, duration: "4s" },
				limit: null,
			},
			enforcement: "soft",
			on_unreachable: "allow",
			on_uncosted: "deny",
			status: "disabled",
		});
	});

	test("an update leaves the status to the page menu", () => {
		const body = updatePolicyBody(
			draftFromPolicy(routePolicy, null),
			"Renamed",
		);

		expect(body).not.toHaveProperty("status");
		expect(body.name).toBe("Renamed");
	});

	test("an action without a chain, overrides or a limit sends nulls", () => {
		expect(createPolicyBody(denyDraft, "Deny").action).toEqual({
			outcome: "deny",
			route_chain: null,
			overrides: null,
			limit: null,
		});
	});

	test("a customer policy sends its customer id", () => {
		const draft: PolicyDraft = {
			...denyDraft,
			level: "customer",
			customer: CUSTOMER,
		};

		expect(createPolicyBody(draft, "Acme").customer_id).toBe(CUSTOMER.id);
	});

	test("a customer policy's draft needs its customer", () => {
		const customerPolicy: PolicyResponse = {
			...routePolicy,
			level: "customer",
			plan_id: null,
			customer_id: CUSTOMER.id,
		};

		expect(() => draftFromPolicy(customerPolicy, null)).toThrow(
			"policy customer missing",
		);
		expect(draftFromPolicy(customerPolicy, CUSTOMER).customer).toEqual(
			CUSTOMER,
		);
	});
});

describe("draftProblems", () => {
	test("a complete draft has none", () => {
		expect(draftProblems(denyDraft)).toEqual([]);
	});

	test("lists every empty blank at its body location", () => {
		const draft: PolicyDraft = {
			...denyDraft,
			name: " ",
			level: "plan",
			when: {
				any: [
					{ signal: "pace", operator: "gt", value: "" },
					{
						all: [
							{ signal: "pace", operator: "gt", value: "1.0000" },
							{ signal: "cost_to_date", operator: "lt", value: "" },
						],
					},
				],
			},
			action: {
				outcome: "route",
				route_chain: [
					{ provider: "fal_ai", model: "fal-ai/veo3.1/lite" },
					unpickedRouteTarget(),
				],
				overrides: { duration: null },
				limit: null,
			},
		};

		expect(draftProblems(draft)).toEqual([
			{ location: "name", message: "Enter a name" },
			{ location: "plan_id", message: "Choose a plan" },
			{ location: "when.any[0].value", message: "Enter a value" },
			{ location: "when.any[1].all[1].value", message: "Enter a value" },
			{ location: "action.route_chain[1]", message: "Choose a model" },
			{ location: "action.overrides.duration", message: "Choose a value" },
		]);
	});

	test("a customer policy without a customer and a cap without a limit value", () => {
		const draft: PolicyDraft = {
			...denyDraft,
			level: "customer",
			action: {
				outcome: "cap",
				route_chain: [],
				overrides: {},
				limit: { kind: "count", value: "" },
			},
		};

		expect(draftProblems(draft)).toEqual([
			{ location: "customer_id", message: "Choose a customer" },
			{ location: "action.limit.value", message: "Enter a limit" },
		]);
	});
});

describe("sameDocument", () => {
	const draft = draftFromPolicy(routePolicy, null);

	test("ignores the order of object keys and the status", () => {
		const reordered: PolicyDraft = {
			...draft,
			status: "active",
			when: { all: [{ value: "2.0000", operator: "gt", signal: "pace" }] },
			action: { ...draft.action, overrides: { duration: "4s", audio: false } },
		};

		expect(sameDocument(draft, reordered)).toBe(true);
	});

	test("tells a changed value apart", () => {
		const changed: PolicyDraft = {
			...draft,
			when: { all: [{ signal: "pace", operator: "gt", value: "3.0000" }] },
		};

		expect(sameDocument(draft, changed)).toBe(false);
	});
});

describe("suggestPolicyName", () => {
	test.each<[string, PolicyDraft, string | null, string]>([
		["everyone and any feature", denyDraft, null, "Deny requests for everyone"],
		[
			"a plan and a feature",
			{
				...draftFromPolicy(routePolicy, null),
				name: null,
			},
			"Creator",
			"Route text to video for Creator customers",
		],
		[
			"a plan still loading",
			{ ...denyDraft, level: "plan", plan_id: routePolicy.plan_id },
			null,
			"Deny requests for a plan",
		],
		[
			"a customer",
			{ ...denyDraft, level: "customer", customer: CUSTOMER },
			null,
			"Deny requests for Acme",
		],
	])("%s", (_case, draft, planName, name) => {
		expect(suggestPolicyName(draft, planName)).toBe(name);
	});
});
