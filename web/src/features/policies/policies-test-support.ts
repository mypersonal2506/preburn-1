import type {
	DecisionResponse,
	PageBodyDecisionResponse,
	PageBodyModelResponse,
	PageBodyPolicyResponse,
	ParameterMappingsResponse,
	PlanResponse,
	PolicyPreviewResult,
	PolicyResponse,
	ProblemError,
} from "@/client";
import type { RouteTarget } from "@/features/policies/policy-phrases";
import {
	type ApiAnswers,
	jsonAnswer,
	signedInAnswers,
} from "@/routes/-render-app";

const MILLISECONDS_PER_HOUR = 3_600_000;

/** Path of the policy list and create endpoint. */
export const POLICIES_PATH = "/api/v1/policies";

/** A plan the plan policy fixtures apply to. */
export const creatorPlan: PlanResponse = {
	id: "pln_01jbvagescfn78y0938nkrkay1",
	name: "Creator",
	mode: "margin_target",
	target_margin: "0.4000",
	allowance: null,
	customer_count: 2,
	hold_times: {},
	status: "active",
	created_at: "2026-09-01T10:00:00Z",
};

/** The cheaper fal video model the route fixtures pick first. */
export const veoLite: RouteTarget = {
	provider: "fal_ai",
	model: "fal-ai/veo3.1/lite",
};

/** The fal video model the route fixtures pick as a fallback. */
export const kling: RouteTarget = {
	provider: "fal_ai",
	model: "fal-ai/kling-video/v2.5-turbo/pro",
};

/** A pricing model page holding veoLite and kling with display names. */
export const videoModelPage: PageBodyModelResponse = {
	items: [
		{
			...veoLite,
			display_name: "Veo 3.1 Lite",
			status: "active",
			key_prices: [],
		},
		{
			...kling,
			display_name: "Kling 2.5 Turbo Pro",
			status: "active",
			key_prices: [],
		},
	],
	next_cursor: null,
};

/** Parameter mappings where veoLite and kling both take 4s or 8s. */
export const videoParameterMappings: ParameterMappingsResponse = {
	models: [veoLite, kling].map((model) => ({
		...model,
		parameters: {
			duration: {
				allowed_values: ["4s", "8s"],
				effect: "sets",
				maximum: null,
				meter: "output_seconds",
				minimum: null,
				provider_parameter: "duration",
				quantities: { "4s": "4", "8s": "8" },
				value_type: "string",
			},
		},
	})),
};

/** An active policy denying every customer above 2.0x pace, version 3. */
export const paceDenyPolicy: PolicyResponse = {
	id: "pol_01jbvagescfn78y0938nkrka01",
	name: "Heavy users",
	level: "everyone",
	plan_id: null,
	customer_id: null,
	feature: null,
	when: { all: [{ signal: "pace", operator: "gt", value: "2.0000" }] },
	action: { outcome: "deny", route_chain: null, overrides: null, limit: null },
	enforcement: "soft",
	on_unreachable: "allow",
	on_uncosted: "allow",
	status: "active",
	version: 3,
	created_at: "2026-09-20T10:00:00Z",
	updated_at: new Date(Date.now() - 2 * MILLISECONDS_PER_HOUR).toISOString(),
};

/** An active policy routing Creator text to video to a cheaper model. */
export const creatorRoutePolicy: PolicyResponse = {
	...paceDenyPolicy,
	id: "pol_01jbvagescfn78y0938nkrka02",
	name: "Cheaper video for Creator",
	level: "plan",
	plan_id: creatorPlan.id,
	feature: "text_to_video",
	action: {
		outcome: "route",
		route_chain: [veoLite],
		overrides: null,
		limit: null,
	},
};

/** A routed decision the pace deny policy's detail lists. */
export const routedDecision: DecisionResponse = {
	id: "dec_01jbvagescfn78y0938nkrka01",
	created_at: new Date(Date.now() - MILLISECONDS_PER_HOUR).toISOString(),
	customer_id: "cust_01jbvagescfn78y0938nkrka01",
	customer_external_id: "acme",
	customer_display_name: "Acme Studio",
	feature: "text_to_video",
	outcome: "deny",
	reason: "policy_matched",
	matched_policy_id: paceDenyPolicy.id,
	provider: "fal_ai",
	model: "fal-ai/veo3.1/fast",
	requested_provider: "fal_ai",
	requested_model: "fal-ai/veo3.1/fast",
	estimated_cost: "0.350000000",
	status: "unreserved",
};

const threeMatches: PolicyPreviewResult = {
	evaluated_customer_count: 40,
	matched_customer_count: 3,
	request_dependent: false,
};

/** Returns a policy list page of items followed by nextCursor. */
export function policyPage(
	items: PolicyResponse[],
	nextCursor: string | null = null,
): PageBodyPolicyResponse {
	return { items, next_cursor: nextCursor };
}

/** Returns a decision list page of items with no next page. */
export function decisionPage(
	items: DecisionResponse[],
): PageBodyDecisionResponse {
	return { items, next_cursor: null };
}

/**
 * The answers a policy editor needs besides its page's own: the signed in
 * shell, the preview, and the active policies of the facts.
 */
export function editorAnswers(activePolicies: PolicyResponse[]): ApiAnswers {
	return {
		...signedInAnswers,
		"POST /api/v1/policies/preview": jsonAnswer(threeMatches),
		"GET /api/v1/policies": jsonAnswer(policyPage(activePolicies)),
	};
}

/** Answers a 422 policy_invalid problem with errors. */
export function policyInvalidAnswer(errors: ProblemError[]): () => Response {
	return () =>
		new Response(
			JSON.stringify({
				type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#policy_invalid",
				title: "Unprocessable Entity",
				status: 422,
				detail: "policy document is invalid",
				code: "policy_invalid",
				errors,
			}),
			{
				status: 422,
				headers: { "Content-Type": "application/problem+json" },
			},
		);
}

/**
 * Answers the requests to one route in order with answers, one each, and
 * fails loud on a request past the last.
 */
export function answersInOrder(answers: (() => Response)[]): () => Response {
	let answered = 0;
	return () => {
		const answer = answers[answered];
		if (answer === undefined) {
			throw new Error(`unexpected extra request count=${answered + 1}`);
		}
		answered += 1;
		return answer();
	};
}

/** Reads the JSON bodies of the requests sent to method and path, in order. */
export async function requestBodies(
	requests: readonly Request[],
	method: string,
	path: string,
): Promise<unknown[]> {
	return Promise.all(
		requests
			.filter(
				(request) =>
					request.method === method && new URL(request.url).pathname === path,
			)
			.map((request) => request.clone().json()),
	);
}
