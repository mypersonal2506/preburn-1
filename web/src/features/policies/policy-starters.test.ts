import { describe, expect, test } from "vitest";
import {
	CAP_SETTINGS_BLANK,
	routeTargetBlank,
} from "@/features/policies/policy-blanks";
import {
	createPolicyBody,
	unpickedRouteTarget,
} from "@/features/policies/policy-draft";
import {
	BLANK_POLICY_DRAFT,
	POLICY_STARTERS,
	starterDraft,
} from "@/features/policies/policy-starters";

const DEFAULT_PLAN_ID = "pln_01jbvagescfn78y0938nkrkayd";

describe("starterDraft", () => {
	test("the chips read Stop at allowance, Cheaper model, Cap on pace, Stop losses and Blank", () => {
		expect(POLICY_STARTERS.map((starter) => starter.label)).toEqual([
			"Stop at allowance",
			"Cheaper model",
			"Cap on pace",
			"Stop losses",
			"Blank",
		]);
	});

	test("Stop at allowance denies everyone at no allowance left, enforced hard", () => {
		const { draft, openedBlank } = starterDraft(
			"stop_at_allowance",
			DEFAULT_PLAN_ID,
		);

		expect(createPolicyBody(draft, "Stop")).toEqual({
			name: "Stop",
			level: "everyone",
			plan_id: null,
			customer_id: null,
			feature: null,
			when: {
				all: [
					{
						signal: "allowance_remaining",
						operator: "lte",
						value: "0.000000000",
					},
				],
			},
			action: {
				outcome: "deny",
				route_chain: null,
				overrides: null,
				limit: null,
			},
			enforcement: "hard",
			on_unreachable: "allow",
			on_uncosted: "allow",
			status: "active",
		});
		expect(openedBlank).toBeNull();
	});

	test("Cheaper model routes the default plan above 1.5x pace and opens the model blank", () => {
		const { draft, openedBlank } = starterDraft(
			"cheaper_model",
			DEFAULT_PLAN_ID,
		);

		expect(draft).toEqual({
			...BLANK_POLICY_DRAFT,
			level: "plan",
			plan_id: DEFAULT_PLAN_ID,
			when: { all: [{ signal: "pace", operator: "gt", value: "1.5000" }] },
			action: {
				outcome: "route",
				route_chain: [unpickedRouteTarget()],
				overrides: {},
				limit: null,
			},
		});
		expect(openedBlank).toBe(routeTargetBlank(0));
	});

	test("Cheaper model leaves the plan blank empty without a default plan", () => {
		const { draft } = starterDraft("cheaper_model", null);

		expect(draft.level).toBe("plan");
		expect(draft.plan_id).toBeNull();
	});

	test("Cap on pace caps above 2.0x pace and opens the settings blank", () => {
		const { draft, openedBlank } = starterDraft("cap_on_pace", null);

		expect(draft).toEqual({
			...BLANK_POLICY_DRAFT,
			when: { all: [{ signal: "pace", operator: "gt", value: "2.0000" }] },
			action: {
				outcome: "cap",
				route_chain: [],
				overrides: {},
				limit: null,
			},
		});
		expect(openedBlank).toBe(CAP_SETTINGS_BLANK);
	});

	test("Stop losses denies below 0% projected margin", () => {
		const { draft, openedBlank } = starterDraft("stop_losses", null);

		expect(draft).toEqual({
			...BLANK_POLICY_DRAFT,
			when: {
				all: [{ signal: "projected_margin", operator: "lt", value: "0.0000" }],
			},
			action: { outcome: "deny", route_chain: [], overrides: {}, limit: null },
		});
		expect(openedBlank).toBeNull();
	});

	test("Blank always allows everyone", () => {
		const { draft, openedBlank } = starterDraft("blank", DEFAULT_PLAN_ID);

		expect(createPolicyBody(draft, "Blank")).toEqual({
			name: "Blank",
			level: "everyone",
			plan_id: null,
			customer_id: null,
			feature: null,
			when: { all: [] },
			action: {
				outcome: "allow",
				route_chain: null,
				overrides: null,
				limit: null,
			},
			enforcement: "soft",
			on_unreachable: "allow",
			on_uncosted: "allow",
			status: "active",
		});
		expect(openedBlank).toBeNull();
	});
});
