import { describe, expect, test } from "vitest";
import {
	CAP_SETTINGS_BLANK,
	conditionComparisonBlank,
	conditionSignalBlank,
	FEATURE_BLANK,
	LIMIT_BLANK,
	NAME_BLANK,
	OUTCOME_BLANK,
	routeOverrideBlank,
	routeTargetBlank,
	WHEN_BLANK,
	WHO_BLANK,
} from "@/features/policies/policy-blanks";
import type { PolicyDraft } from "@/features/policies/policy-draft";
import {
	policyProblemView,
	serverProblems,
} from "@/features/policies/policy-problems";
import { ApiProblem } from "@/lib/api-problem";

const MESSAGE = "is invalid";

const routeDraft: PolicyDraft = {
	name: null,
	level: "everyone",
	plan_id: null,
	customer: null,
	feature: null,
	when: {
		all: [
			{ signal: "pace", operator: "gt", value: "2.0000" },
			{ signal: "cost_to_date", operator: "gt", value: "5.000000000" },
		],
	},
	action: {
		outcome: "route",
		route_chain: [
			{ provider: "fal_ai", model: "fal-ai/veo3.1/lite" },
			{ provider: "fal_ai", model: "fal-ai/kling-video/v2.5-turbo/pro" },
		],
		overrides: { duration: "5" },
		limit: null,
	},
	enforcement: "soft",
	on_unreachable: "allow",
	on_uncosted: "allow",
	status: "active",
};

const capDraft: PolicyDraft = {
	...routeDraft,
	when: {
		all: [
			{ signal: "pace", operator: "gt", value: "2.0000" },
			{ all: [{ signal: "pace", operator: "gt", value: "3.0000" }] },
		],
	},
	action: {
		outcome: "cap",
		route_chain: [],
		overrides: { audio: false },
		limit: { kind: "count", value: "20" },
	},
};

function problemAnswer(
	status: number,
	code: string,
	errors: { location: string; message: string }[],
): ApiProblem {
	return new ApiProblem({
		type: `https://github.com/preburn/preburn/blob/main/docs/errors.md#${code}`,
		title: "Error",
		status,
		code,
		detail: code,
		errors,
		retryAfterSeconds: null,
	});
}

describe("policyProblemView", () => {
	test.each<[string, PolicyDraft, string, string]>([
		["the name", routeDraft, "name", NAME_BLANK],
		["the level", routeDraft, "level", WHO_BLANK],
		["the plan", routeDraft, "plan_id", WHO_BLANK],
		["the customer", routeDraft, "customer_id", WHO_BLANK],
		["the feature", routeDraft, "feature", FEATURE_BLANK],
		["the group", routeDraft, "when", WHEN_BLANK],
		["the group members", routeDraft, "when.all", WHEN_BLANK],
		[
			"an inline condition's signal",
			routeDraft,
			"when.all[1].signal",
			conditionSignalBlank(1),
		],
		[
			"an inline condition's operator",
			routeDraft,
			"when.all[0].operator",
			conditionComparisonBlank(0),
		],
		[
			"an inline condition's value",
			routeDraft,
			"when.all[0].value",
			conditionComparisonBlank(0),
		],
		[
			"a whole inline condition",
			routeDraft,
			"when.all[1]",
			conditionComparisonBlank(1),
		],
		[
			"a condition the draft no longer has",
			routeDraft,
			"when.all[2]",
			WHEN_BLANK,
		],
		["a condition of the other match", routeDraft, "when.any[0]", WHEN_BLANK],
		["a condition in the panel", capDraft, "when.all[0].value", WHEN_BLANK],
		["the action", routeDraft, "action", OUTCOME_BLANK],
		["the outcome", routeDraft, "action.outcome", OUTCOME_BLANK],
		["the route chain", routeDraft, "action.route_chain", OUTCOME_BLANK],
		[
			"a route target",
			routeDraft,
			"action.route_chain[1].model",
			routeTargetBlank(1),
		],
		[
			"a route override",
			routeDraft,
			"action.overrides.duration",
			routeOverrideBlank("duration"),
		],
		[
			"a route override the draft does not have",
			routeDraft,
			"action.overrides.audio",
			OUTCOME_BLANK,
		],
		["a cap override", capDraft, "action.overrides.audio", CAP_SETTINGS_BLANK],
		["the cap overrides", capDraft, "action.overrides", CAP_SETTINGS_BLANK],
		["the limit value", capDraft, "action.limit.value", LIMIT_BLANK],
		[
			"a limit the draft does not have",
			routeDraft,
			"action.limit",
			OUTCOME_BLANK,
		],
	])("a problem at %s shows at its blank", (_case, draft, location, blank) => {
		const view = policyProblemView([{ location, message: MESSAGE }], draft);

		expect(view.blankMessage(blank)).toBe(MESSAGE);
		expect(view.messages).toEqual([MESSAGE]);
	});

	test.each([
		["enforcement", "Enforcement: is invalid"],
		["on_unreachable", "When Preburn is unreachable: is invalid"],
		["on_uncosted", "When a request has no price: is invalid"],
		["status", "Status: is invalid"],
		["path.policy_id", "path.policy_id: is invalid"],
	])("a problem at %s reads %s in the list only", (location, listed) => {
		const view = policyProblemView(
			[{ location, message: MESSAGE }],
			routeDraft,
		);

		expect(view.messages).toEqual([listed]);
		expect(view.blankMessage(OUTCOME_BLANK)).toBeUndefined();
	});

	test("a problem with the whole request goes to the list as it is", () => {
		const view = policyProblemView(
			[{ location: null, message: "Something went wrong. Try again." }],
			routeDraft,
		);

		expect(view.messages).toEqual(["Something went wrong. Try again."]);
	});

	test("a blank with several problems shows each message once", () => {
		const view = policyProblemView(
			[
				{ location: "action.limit.kind", message: "Choose a kind" },
				{ location: "action.limit.value", message: "Enter a limit" },
				{ location: "action.limit", message: "Enter a limit" },
			],
			capDraft,
		);

		expect(view.blankMessage(LIMIT_BLANK)).toBe("Choose a kind. Enter a limit");
	});
});

describe("serverProblems", () => {
	test("each field error becomes a problem at its body location", () => {
		expect(
			serverProblems(
				problemAnswer(422, "policy_invalid", [
					{ location: "body.when.all[0].value", message: "must be a ratio" },
					{
						location: "body.action.overrides.duration",
						message: "not allowed",
					},
				]),
			),
		).toEqual([
			{ location: "when.all[0].value", message: "must be a ratio" },
			{ location: "action.overrides.duration", message: "not allowed" },
		]);
	});

	test("an inactive plan shows at the plan", () => {
		expect(serverProblems(problemAnswer(422, "plan_not_found", []))).toEqual([
			{ location: "plan_id", message: "Choose an active plan" },
		]);
	});

	test("a problem without field errors asks to try again", () => {
		expect(serverProblems(problemAnswer(500, "internal_error", []))).toEqual([
			{ location: null, message: "Something went wrong. Try again." },
		]);
	});
});
