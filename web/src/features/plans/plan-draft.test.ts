import { describe, expect, test } from "vitest";
import {
	createPlanBody,
	draftFromPlan,
	holdTimesSummary,
	newPlanDraft,
	type PlanDraft,
	planRule,
	updatePlanBody,
} from "@/features/plans/plan-draft";
import { creatorPlan, freePlan } from "@/features/plans/plans-test-support";

function marginDraft(overrides: Partial<PlanDraft> = {}): PlanDraft {
	return {
		...newPlanDraft(),
		name: "Creator",
		target_margin: "0.4000",
		...overrides,
	};
}

describe("draftFromPlan", () => {
	test("keeps the stored target margin of a fixed allowance plan", () => {
		expect(draftFromPlan(freePlan)).toEqual({
			name: "Free",
			mode: "fixed_allowance",
			target_margin: "0.4000",
			allowance: "2.000000000",
			hold_times: [],
		});
	});

	test("turns hold times into rows", () => {
		expect(draftFromPlan(creatorPlan).hold_times).toEqual([
			{ feature: "text_to_video", seconds: 600 },
		]);
	});
});

describe("createPlanBody", () => {
	test("a margin target plan sends its target margin and no allowance", () => {
		expect(
			createPlanBody(
				marginDraft({ name: "  Creator ", allowance: "1.000000000" }),
			),
		).toEqual({
			name: "Creator",
			mode: "margin_target",
			target_margin: "0.4000",
			hold_times: {},
		});
	});

	test("a fixed allowance plan sends its allowance and no target margin", () => {
		expect(
			createPlanBody(
				marginDraft({
					name: "Free",
					mode: "fixed_allowance",
					allowance: "2.000000000",
				}),
			),
		).toEqual({
			name: "Free",
			mode: "fixed_allowance",
			allowance: "2.000000000",
			hold_times: {},
		});
	});

	test("hold time rows become the hold times map", () => {
		expect(
			createPlanBody(
				marginDraft({
					hold_times: [
						{ feature: "text_to_video", seconds: 600 },
						{ feature: "chat", seconds: 30 },
					],
				}),
			).hold_times,
		).toEqual({ text_to_video: 600, chat: 30 });
	});

	test("throws for a draft missing the value its mode needs", () => {
		expect(() => createPlanBody(marginDraft({ target_margin: null }))).toThrow(
			"plan value missing field=target_margin",
		);
	});
});

describe("updatePlanBody", () => {
	test("an unchanged draft sends nothing", () => {
		expect(updatePlanBody(creatorPlan, draftFromPlan(creatorPlan))).toEqual({});
	});

	test("switching to fixed allowance sends the allowance and keeps the target margin", () => {
		expect(
			updatePlanBody(creatorPlan, {
				...draftFromPlan(creatorPlan),
				mode: "fixed_allowance",
				allowance: "2.000000000",
			}),
		).toEqual({ mode: "fixed_allowance", allowance: "2.000000000" });
	});

	test("switching back to margin target sends the mode alone", () => {
		expect(
			updatePlanBody(freePlan, {
				...draftFromPlan(freePlan),
				mode: "margin_target",
			}),
		).toEqual({ mode: "margin_target" });
	});

	test("a changed name, target margin and hold times are sent", () => {
		expect(
			updatePlanBody(creatorPlan, {
				...draftFromPlan(creatorPlan),
				name: " Creator plus ",
				target_margin: "0.5000",
				hold_times: [],
			}),
		).toEqual({
			name: "Creator plus",
			target_margin: "0.5000",
			hold_times: {},
		});
	});

	test("a changed allowance is sent", () => {
		expect(
			updatePlanBody(freePlan, {
				...draftFromPlan(freePlan),
				allowance: "3.000000000",
			}),
		).toEqual({ allowance: "3.000000000" });
	});
});

describe("planRule", () => {
	test("names the target margin or the allowance", () => {
		expect(planRule(creatorPlan)).toBe("40.0% margin target");
		expect(planRule(freePlan)).toBe("$2.00 fixed allowance");
	});
});

describe("holdTimesSummary", () => {
	test("counts the custom hold times", () => {
		expect(holdTimesSummary([])).toBe("Default hold times");
		expect(holdTimesSummary([{ feature: "chat", seconds: 30 }])).toBe(
			"1 custom hold time",
		);
		expect(
			holdTimesSummary([
				{ feature: "chat", seconds: 30 },
				{ feature: null, seconds: null },
			]),
		).toBe("2 custom hold times");
	});
});
