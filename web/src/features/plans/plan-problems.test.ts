import { describe, expect, test } from "vitest";
import type { ProblemError } from "@/client";
import { newPlanDraft, type PlanDraft } from "@/features/plans/plan-draft";
import {
	planDraftProblems,
	planRequestProblem,
} from "@/features/plans/plan-problems";
import { ApiProblem } from "@/lib/api-problem";

function validDraft(overrides: Partial<PlanDraft> = {}): PlanDraft {
	return {
		...newPlanDraft(),
		name: "Creator",
		target_margin: "0.4000",
		...overrides,
	};
}

function problem(
	status: number,
	code: string,
	errors: ProblemError[] = [],
): ApiProblem {
	return new ApiProblem({
		type: `https://github.com/preburn/preburn/blob/main/docs/errors.md#${code}`,
		title: "Error",
		status,
		code,
		detail: code.replaceAll("_", " "),
		errors,
		retryAfterSeconds: null,
	});
}

describe("planDraftProblems", () => {
	test("a complete draft has no problems", () => {
		expect(
			planDraftProblems(
				validDraft({ hold_times: [{ feature: "chat", seconds: 30 }] }),
			),
		).toEqual({ fields: {}, holdTimes: [null] });
	});

	test("the name needs 1 to 80 characters", () => {
		expect(planDraftProblems(validDraft({ name: "  " })).fields).toEqual({
			name: "Enter a name",
		});
		expect(
			planDraftProblems(validDraft({ name: "p".repeat(81) })).fields,
		).toEqual({ name: "Use at most 80 characters" });
		expect(
			planDraftProblems(validDraft({ name: "p".repeat(80) })).fields,
		).toEqual({});
	});

	test("margin target mode needs a target margin from 0% to 99.99%", () => {
		expect(
			planDraftProblems(validDraft({ target_margin: null })).fields,
		).toEqual({ rule: "Enter a target margin" });
		expect(
			planDraftProblems(validDraft({ target_margin: "1.0000" })).fields,
		).toEqual({ rule: "Use 0% to 99.99%" });
		expect(
			planDraftProblems(validDraft({ target_margin: "-0.0100" })).fields,
		).toEqual({ rule: "Use 0% to 99.99%" });
		expect(
			planDraftProblems(validDraft({ target_margin: "0.9999" })).fields,
		).toEqual({});
	});

	test("fixed allowance mode needs an allowance of 0 or more", () => {
		const fixedDraft = validDraft({ mode: "fixed_allowance" });

		expect(planDraftProblems(fixedDraft).fields).toEqual({
			rule: "Enter an allowance",
		});
		expect(
			planDraftProblems({ ...fixedDraft, allowance: "-1.000000000" }).fields,
		).toEqual({ rule: "Enter 0 or more" });
		expect(
			planDraftProblems({ ...fixedDraft, allowance: "0.000000000" }).fields,
		).toEqual({});
	});

	test("hold times need 30 seconds to 24 hours", () => {
		const problems = planDraftProblems(
			validDraft({
				hold_times: [
					{ feature: "chat", seconds: 29 },
					{ feature: "text_to_video", seconds: 30 },
					{ feature: "image", seconds: 86_400 },
					{ feature: "speech", seconds: 86_401 },
				],
			}),
		);

		expect(problems.fields).toEqual({
			hold_times: "Fix the hold times under Advanced",
		});
		expect(problems.holdTimes).toEqual([
			{ part: "seconds", message: "Use 30 seconds to 24 hours" },
			null,
			null,
			{ part: "seconds", message: "Use 30 seconds to 24 hours" },
		]);
	});

	test("each hold time needs its own feature and a duration", () => {
		expect(
			planDraftProblems(
				validDraft({
					hold_times: [
						{ feature: null, seconds: 60 },
						{ feature: "chat", seconds: null },
						{ feature: "chat", seconds: 60 },
					],
				}),
			).holdTimes,
		).toEqual([
			{ part: "feature", message: "Pick a feature" },
			{ part: "seconds", message: "Use 30 seconds to 24 hours" },
			{ part: "feature", message: "Feature already has a hold time" },
		]);
	});
});

describe("planRequestProblem", () => {
	test("no error maps to no messages", () => {
		expect(planRequestProblem(null)).toEqual({ fields: {} });
	});

	test("plan_name_taken shows at the name", () => {
		expect(planRequestProblem(problem(409, "plan_name_taken"))).toEqual({
			fields: { name: ["Another plan has this name"] },
		});
	});

	test("name, target margin and allowance errors show at their blanks", () => {
		expect(
			planRequestProblem(
				problem(422, "validation_failed", [
					{
						location: "body.name",
						message: "expected 1 to 80 characters without control characters",
					},
					{
						location: "body.target_margin",
						message: "expected a ratio from 0 to 0.9999",
					},
					{
						location: "body.allowance",
						message: "expected a non-negative USD amount",
					},
				]),
			),
		).toEqual({
			fields: {
				name: ["Use 1 to 80 characters"],
				rule: ["Use 0% to 99.99%", "Enter 0 or more"],
			},
		});
	});

	test("other errors go to the form list", () => {
		expect(
			planRequestProblem(
				problem(422, "validation_failed", [
					{
						location: "body.hold_times.chat",
						message: "expected 30 to 86400 seconds",
					},
				]),
			),
		).toEqual({
			fields: {},
			form: ["hold_times.chat: expected 30 to 86400 seconds"],
		});
		expect(planRequestProblem(problem(500, "internal_error"))).toEqual({
			fields: {},
			form: ["Something went wrong. Try again."],
		});
	});
});
