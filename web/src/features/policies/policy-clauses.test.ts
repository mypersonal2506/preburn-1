import { describe, expect, test } from "vitest";
import type { OverrideParameter } from "@/features/policies/override-parameters";
import {
	CAP_SETTINGS_BLANK,
	conditionSignalBlank,
	LIMIT_BLANK,
	routeOverrideBlank,
	routeTargetBlank,
	WHEN_BLANK,
} from "@/features/policies/policy-blanks";
import { policyClauses } from "@/features/policies/policy-clauses";
import {
	type PolicyDraft,
	unpickedRouteTarget,
} from "@/features/policies/policy-draft";
import { BLANK_POLICY_DRAFT } from "@/features/policies/policy-starters";

const DURATION: OverrideParameter = {
	key: "duration",
	values: ["4s", "8s"],
	minimum: null,
	maximum: null,
};

const PACE_CONDITION = {
	signal: "pace",
	operator: "gt",
	value: "2.0000",
} as const;

const VEO = { provider: "fal_ai", model: "fal-ai/veo3.1/lite" };

function clauseLabels(draft: PolicyDraft, parameters: OverrideParameter[]) {
	return policyClauses(draft, parameters).map((clause) => clause.label);
}

function clause(
	draft: PolicyDraft,
	parameters: OverrideParameter[],
	label: string,
) {
	const found = policyClauses(draft, parameters).find(
		(candidate) => candidate.label === label,
	);
	if (found === undefined) {
		throw new Error(`clause missing label=${label}`);
	}
	return found;
}

describe("policyClauses", () => {
	test("an always allow draft offers conditions only", () => {
		expect(clauseLabels(BLANK_POLICY_DRAFT, [DURATION])).toEqual([
			"Condition",
			"Condition group",
		]);
	});

	test("a condition added inline opens its signal", () => {
		const added = clause(
			{ ...BLANK_POLICY_DRAFT, when: { any: [PACE_CONDITION] } },
			[],
			"Condition",
		);

		expect(added.draft.when).toEqual({
			any: [
				PACE_CONDITION,
				{ signal: "allowance_remaining", operator: "gt", value: "" },
			],
		});
		expect(added.openedBlank).toBe(conditionSignalBlank(1));
	});

	test("a fourth condition opens the condition panel", () => {
		const draft: PolicyDraft = {
			...BLANK_POLICY_DRAFT,
			when: { all: [PACE_CONDITION, PACE_CONDITION, PACE_CONDITION] },
		};

		const added = clause(draft, [], "Condition");

		expect(added.draft.when.all).toHaveLength(4);
		expect(added.openedBlank).toBe(WHEN_BLANK);
	});

	test("a condition group opens the condition panel", () => {
		const added = clause(BLANK_POLICY_DRAFT, [], "Condition group");

		expect(added.draft.when).toEqual({ all: [{ all: [] }] });
		expect(added.openedBlank).toBe(WHEN_BLANK);
	});

	test("the condition panel offers no condition clauses", () => {
		const draft: PolicyDraft = {
			...BLANK_POLICY_DRAFT,
			when: { all: [{ all: [PACE_CONDITION] }] },
		};

		expect(clauseLabels(draft, [])).toEqual([]);
	});

	test("a route adds fallbacks up to 5 models and settings not set yet", () => {
		const route: PolicyDraft = {
			...BLANK_POLICY_DRAFT,
			when: { all: [PACE_CONDITION] },
			action: {
				outcome: "route",
				route_chain: [VEO],
				overrides: {},
				limit: null,
			},
		};

		const fallback = clause(route, [DURATION], "Fallback model");
		const setting = clause(route, [DURATION], "Setting");

		expect(fallback.draft.action.route_chain).toEqual([
			VEO,
			unpickedRouteTarget(),
		]);
		expect(fallback.openedBlank).toBe(routeTargetBlank(1));
		expect(setting.draft.action.overrides).toEqual({ duration: null });
		expect(setting.openedBlank).toBe(routeOverrideBlank("duration"));
		expect(
			clauseLabels(
				{
					...route,
					action: {
						...route.action,
						route_chain: [VEO, VEO, VEO, VEO, VEO],
						overrides: { duration: "4s" },
					},
				},
				[DURATION],
			),
		).toEqual(["Condition", "Condition group"]);
	});

	test("a cap adds a limit, and a setting once its settings blank is hidden", () => {
		const cap: PolicyDraft = {
			...BLANK_POLICY_DRAFT,
			action: { outcome: "cap", route_chain: [], overrides: {}, limit: null },
		};

		const limit = clause(cap, [DURATION], "Limit");

		expect(clauseLabels(cap, [DURATION])).not.toContain("Setting");
		expect(limit.draft.action.limit).toEqual({ kind: "count", value: "" });
		expect(limit.openedBlank).toBe(LIMIT_BLANK);

		const setting = clause(limit.draft, [DURATION], "Setting");

		expect(setting.draft.action.overrides).toEqual({ duration: null });
		expect(setting.openedBlank).toBe(CAP_SETTINGS_BLANK);
	});
});
