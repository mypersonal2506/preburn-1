import type { DecisionDetailResponse } from "@/client";
import { attributeValueLabel } from "@/lib/labels";
import { outcomeStyles } from "@/lib/outcomes";

/** The parts of a decision its summary sentence reads. */
export type DecisionSummaryFacts = Pick<
	DecisionDetailResponse,
	"outcome" | "reason" | "overrides"
>;

/**
 * The names a summary sentence shows: the matched policy's name, or null
 * when no policy matched, and the display names of the requested model and
 * the model the decision runs.
 */
export interface DecisionSummaryNames {
	policyName: string | null;
	requestedModelName: string;
	servedModelName: string;
}

type DecisionReason = DecisionDetailResponse["reason"];

type OverrideValue = DecisionDetailResponse["overrides"][string];

const REASON_CLAUSES: Record<DecisionReason, string> = {
	no_policy_matched: " because no policy matched",
	policy_matched: "",
	hard_limit_reached: " because the hard limit was reached",
	route_chain_exhausted:
		" because no route target was priced within the limits",
	uncosted_allowed: " without a price",
	uncosted_denied: " because it has no price",
	cap_not_applicable: " because its cap does not apply to the model",
};

/**
 * One sentence saying what a decision did and why, such as "Heavy video
 * users routed Veo 3.1 Fast to Kling 2.5 Turbo Pro, at 5s." The matched
 * policy is the subject, and a decision no policy decided starts with its
 * outcome ("Allowed Veo 3.1 Fast because no policy matched."). A route names
 * its target and its override phrases, a cap its overrides ("capped Veo 3.1
 * Fast to 4s, without audio"), and every reason other than policy_matched
 * adds its clause.
 */
export function decisionSummary(
	decision: DecisionSummaryFacts,
	names: DecisionSummaryNames,
): string {
	const outcomeLabel = outcomeStyles[decision.outcome].pastTenseLabel;
	const subject =
		names.policyName === null
			? outcomeLabel
			: `${names.policyName} ${outcomeLabel.toLowerCase()}`;
	return `${subject} ${names.requestedModelName}${outcomeDetail(decision, names)}${REASON_CLAUSES[decision.reason]}.`;
}

function outcomeDetail(
	decision: DecisionSummaryFacts,
	names: DecisionSummaryNames,
): string {
	const overrides = Object.entries(decision.overrides);
	switch (decision.outcome) {
		case "route":
			return ` to ${names.servedModelName}${overrides.map(routeOverridePhrase).join("")}`;
		case "cap":
			return overrides.length === 0
				? ""
				: ` to ${overrides.map(([key, value]) => attributeValueLabel(key, value)).join(", ")}`;
		case "allow":
		case "deny":
			return "";
	}
}

function routeOverridePhrase([key, value]: [string, OverrideValue]): string {
	const label = attributeValueLabel(key, value);
	return typeof value === "boolean" ? `, ${label}` : `, at ${label}`;
}
