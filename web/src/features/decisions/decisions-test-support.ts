import type {
	CustomerDetailResponse,
	DecisionDetailResponse,
	DecisionResponse,
	ModelResponse,
	PageBodyCustomerMarginResponse,
	PageBodyDecisionResponse,
	PageBodyKnownFeatureResponse,
	PageBodyModelResponse,
	PolicyResponse,
	SignalsResponse,
} from "@/client";
import type { DecisionEvent } from "@/lib/use-event-stream";
import {
	type ApiAnswers,
	jsonAnswer,
	signedInAnswers,
} from "@/routes/-render-app";

const veoFastModel: ModelResponse = {
	provider: "fal_ai",
	model: "fal-ai/veo3.1/fast",
	display_name: "Veo 3.1 Fast",
	key_prices: [
		{ meter: "output_seconds", unit_price: "0.150000000", unit_quantity: 1 },
	],
	status: "active",
};

const klingTurboModel: ModelResponse = {
	provider: "fal_ai",
	model: "fal-ai/kling-video/v2.5-turbo/pro/text-to-video",
	display_name: "Kling 2.5 Turbo Pro",
	key_prices: [
		{ meter: "output_seconds", unit_price: "0.070000000", unit_quantity: 1 },
	],
	status: "active",
};

/** The policy that routed and denied the fixture decisions. */
export const heavyVideoPolicy: PolicyResponse = {
	id: "pol_01jbvagescfn78y0938nkrkayd",
	name: "Heavy video users",
	level: "everyone",
	plan_id: null,
	customer_id: null,
	feature: "text_to_video",
	enforcement: "soft",
	on_uncosted: "allow",
	on_unreachable: "allow",
	status: "active",
	version: 3,
	when: {
		all: [{ signal: "pace", operator: "gt", value: "2.0000" }],
	},
	action: {
		outcome: "route",
		route_chain: [
			{ provider: klingTurboModel.provider, model: klingTurboModel.model },
		],
		overrides: null,
		limit: null,
	},
	created_at: "2026-09-20T09:00:00Z",
	updated_at: "2026-09-24T09:00:00Z",
};

/** A decision that routed Veo 3.1 Fast to Kling 2.5 Turbo Pro. */
export const routedDecision: DecisionResponse = {
	id: "dec_01jbvagescfn78y0938nkrka01",
	created_at: "2026-09-26T10:15:30Z",
	customer_id: "cust_01jbvagescfn78y0938nkrkayd",
	customer_external_id: "customer-42",
	customer_display_name: "Acme Studio",
	feature: "text_to_video",
	requested_provider: veoFastModel.provider,
	requested_model: veoFastModel.model,
	provider: klingTurboModel.provider,
	model: klingTurboModel.model,
	outcome: "route",
	reason: "policy_matched",
	estimated_cost: "0.560000000",
	matched_policy_id: heavyVideoPolicy.id,
	status: "settled",
};

/** A decision that denied Veo 3.1 Fast for another customer. */
export const deniedDecision: DecisionResponse = {
	id: "dec_01jbvagescfn78y0938nkrka02",
	created_at: "2026-09-26T10:14:00Z",
	customer_id: "cust_01jbvagescfn78y0938nkrkaze",
	customer_external_id: "customer-7",
	customer_display_name: "Cedar Games",
	feature: "image_generation",
	requested_provider: veoFastModel.provider,
	requested_model: veoFastModel.model,
	provider: veoFastModel.provider,
	model: veoFastModel.model,
	outcome: "deny",
	reason: "hard_limit_reached",
	estimated_cost: "0.000160000",
	matched_policy_id: null,
	status: "unreserved",
};

const halfwaySignals: SignalsResponse = {
	period_revenue_net: "30.000000000",
	cost_to_date: "12.000000000",
	reserved: "0.560000000",
	cost_allowance: "10.000000000",
	allowance_remaining: "-2.560000000",
	elapsed_fraction: "0.5000",
	pace: "2.4000",
	projected_margin: "0.2000",
	period_decision_count: 120,
	request_estimated_cost: "1.200000000",
	features: {},
};

/** The detail of routedDecision with one reported ledger entry. */
export const routedDecisionDetail: DecisionDetailResponse = {
	id: routedDecision.id,
	created_at: routedDecision.created_at,
	customer_id: routedDecision.customer_id,
	customer_external_id: routedDecision.customer_external_id,
	customer_display_name: routedDecision.customer_display_name,
	customer_user_external_id: "user-9",
	feature: routedDecision.feature,
	requested_provider: routedDecision.requested_provider,
	requested_model: routedDecision.requested_model,
	provider: routedDecision.provider,
	model: routedDecision.model,
	attributes: { duration: 8, audio: true },
	overrides: { duration: 5 },
	outcome: routedDecision.outcome,
	reason: routedDecision.reason,
	requested_estimated_cost: "1.200000000",
	estimated_cost: routedDecision.estimated_cost,
	reserved_amount: "0.560000000",
	estimate_basis: "request_estimate",
	matched_policy_id: heavyVideoPolicy.id,
	matched_policy_version: 3,
	expires_at: "2026-09-26T10:25:30Z",
	settled_at: "2026-09-26T10:16:10Z",
	period_start: "2026-09-01T00:00:00Z",
	period_end: "2026-10-01T00:00:00Z",
	status: routedDecision.status,
	signals: halfwaySignals,
	ledger_entries: [
		{
			id: "led_01jbvagescfn78y0938nkrka01",
			correction_of: null,
			provider: klingTurboModel.provider,
			model: klingTurboModel.model,
			usage: { output_seconds: "5" },
			cost: "0.350000000",
			cost_status: "costed",
			occurred_at: "2026-09-26T10:16:05Z",
			created_at: "2026-09-26T10:16:10Z",
		},
	],
};

/** The dashboard detail of the customer of routedDecision. */
export const acmeCustomer: CustomerDetailResponse = {
	id: routedDecision.customer_id,
	external_id: routedDecision.customer_external_id,
	display_name: routedDecision.customer_display_name,
	status: "active",
	plan_id: null,
	plan_name: null,
	target_margin: null,
	period_start: "2026-09-01T00:00:00Z",
	period_end: "2026-10-01T00:00:00Z",
	signals: halfwaySignals,
	history: [],
	usage: [],
	recent_decisions: [],
};

const pricingModels: PageBodyModelResponse = {
	items: [klingTurboModel, veoFastModel],
	next_cursor: null,
};

const knownFeatures: PageBodyKnownFeatureResponse = {
	items: [
		{ feature: "text_to_video", sources: ["decisions"] },
		{ feature: "image_generation", sources: ["decisions"] },
	],
	next_cursor: null,
};

const noCustomers: PageBodyCustomerMarginResponse = {
	items: [],
	next_cursor: null,
};

/**
 * Answers of a signed-in dashboard for the decision screens: the pricing
 * catalog, the known features and an empty customer list. Tests add the
 * decisions list, a decision or a policy.
 */
export const decisionScreenAnswers: ApiAnswers = {
	...signedInAnswers,
	"GET /api/v1/pricing/models": jsonAnswer(pricingModels),
	"GET /api/v1/dashboard/features": jsonAnswer(knownFeatures),
	"GET /api/v1/dashboard/customers": jsonAnswer(noCustomers),
};

/** Removes the fields the decision stream does not carry from a decision. */
export function streamedDecision(decision: DecisionResponse): DecisionEvent {
	return {
		id: decision.id,
		created_at: decision.created_at,
		customer_id: decision.customer_id,
		customer_external_id: decision.customer_external_id,
		customer_display_name: decision.customer_display_name,
		feature: decision.feature,
		requested_model: decision.requested_model,
		model: decision.model,
		outcome: decision.outcome,
		reason: decision.reason,
		estimated_cost: decision.estimated_cost,
		matched_policy_id: decision.matched_policy_id,
	};
}

/** A last page of the decisions list holding decisions. */
export function decisionsPage(
	decisions: DecisionResponse[],
): PageBodyDecisionResponse {
	return { items: decisions, next_cursor: null };
}
