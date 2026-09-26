import type { DecisionDetailResponse } from "@/client";
import { formatUnitCost } from "@/lib/format";
import { attributeValueLabel, featureLabel } from "@/lib/labels";
import { outcomeStyles } from "@/lib/outcomes";

type DecisionReason = DecisionDetailResponse["reason"];

type EstimateBasis = DecisionDetailResponse["estimate_basis"];

const UNPRICED_TEXT = "Unpriced";

const REASON_LABELS: Record<DecisionReason, string> = {
	no_policy_matched: "No policy matched",
	policy_matched: "Policy matched",
	hard_limit_reached: "Hard limit reached",
	route_chain_exhausted: "No route target was priced",
	uncosted_allowed: "Allowed without a price",
	uncosted_denied: "Denied without a price",
	cap_not_applicable: "Cap does not apply to the model",
};

const ESTIMATE_BASIS_LABELS: Record<EstimateBasis, string> = {
	p95: "p95 of recent usage",
	ceiling: "Usage ceiling",
	request_estimate: "Request estimate",
	none: "Nothing reserved",
};

/**
 * Titles a decision by its outcome and feature, such as "Routed: text to
 * video", for its page header and breadcrumb.
 */
export function decisionTitle(
	decision: Pick<DecisionDetailResponse, "outcome" | "feature">,
): string {
	return `${outcomeStyles[decision.outcome].pastTenseLabel}: ${featureLabel(decision.feature)}`;
}

/** Says why a check decided its outcome, such as "Hard limit reached". */
export function decisionReasonLabel(reason: DecisionReason): string {
	return REASON_LABELS[reason];
}

/** Names the usage whose cost a decision reserved, such as "Usage ceiling". */
export function estimateBasisLabel(basis: EstimateBasis): string {
	return ESTIMATE_BASIS_LABELS[basis];
}

/**
 * Formats the estimated cost of one request, or "Unpriced" when the API
 * sends null because a meter has no price.
 */
export function requestCostLabel(cost: string | null): string {
	return cost === null ? UNPRICED_TEXT : formatUnitCost(cost);
}

/**
 * Lists request attributes or overrides for display, such as "8s, with
 * audio". An empty map gives an empty string.
 */
export function attributesLabel(
	attributes: DecisionDetailResponse["attributes"],
): string {
	return Object.entries(attributes)
		.map(([key, value]) => attributeValueLabel(key, value))
		.join(", ");
}
