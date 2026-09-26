import type {
	PolicyAction,
	PolicyConditionGroup,
	PolicyResponse,
} from "@/client";
import {
	capOverridePhrase,
	comparisonPhrase,
	conditionGroupPhrase,
	inlineConditions,
	isAlways,
	limitPhrase,
	matchWord,
	type RouteTarget,
	routeOverridePhrase,
	signalPhrase,
} from "@/features/policies/policy-phrases";
import { featureLabel } from "@/lib/labels";

/**
 * The display names a policy summary reads: a plan's name by plan id, a
 * customer's name by customer id and a model's display name.
 */
export interface PolicyNames {
	plan: (planId: string) => string;
	customer: (customerId: string) => string;
	model: (target: RouteTarget) => string;
}

/** The fields of a policy that its summary sentence describes. */
export type PolicySummaryDocument = Pick<
	PolicyResponse,
	"level" | "plan_id" | "customer_id" | "feature" | "when" | "action"
>;

/**
 * Turns a policy document into its one-line sentence, the grammar of the
 * sentence builder: "For {who} using {feature}, {when} {then}.", such as
 * "For Creator customers using text to video, when pace is above 2.0x,
 * route to Kling 2.5 Turbo Pro, or Veo 3.1 Lite if that has no price, at
 * 5s." Up to three conditions read inline, deeper or longer groups as
 * "when all of 5 conditions match". Throws for a document the API rejects,
 * such as a plan policy without a plan.
 */
export function policySummary(
	document: PolicySummaryDocument,
	names: PolicyNames,
): string {
	const feature =
		document.feature === null ? "any feature" : featureLabel(document.feature);
	return `For ${whoPhrase(document, names)} using ${feature}, ${whenPhrase(document.when)} ${thenPhrase(document.action, names)}.`;
}

/**
 * Phrases who a policy applies to: "all customers", "Creator customers" for
 * a plan, or the customer's name. Throws when the plan or customer the
 * level needs is missing.
 */
export function whoPhrase(
	document: Pick<PolicySummaryDocument, "level" | "plan_id" | "customer_id">,
	names: Pick<PolicyNames, "plan" | "customer">,
): string {
	switch (document.level) {
		case "everyone":
			return "all customers";
		case "plan":
			if (document.plan_id === null) {
				throw new Error("policy plan missing level=plan");
			}
			return `${names.plan(document.plan_id)} customers`;
		case "customer":
			if (document.customer_id === null) {
				throw new Error("policy customer missing level=customer");
			}
			return names.customer(document.customer_id);
	}
}

function whenPhrase(group: PolicyConditionGroup): string {
	if (isAlways(group)) {
		return "always";
	}
	const inline = inlineConditions(group);
	if (inline === null) {
		return `when ${conditionGroupPhrase(group)} match,`;
	}
	const conditions = inline.conditions.map(
		(condition) =>
			`${signalPhrase(condition.signal)} is ${comparisonPhrase(condition)}`,
	);
	return `when ${conditions.join(` ${matchWord(inline.match)} `)},`;
}

function thenPhrase(action: PolicyAction, names: PolicyNames): string {
	switch (action.outcome) {
		case "allow":
			return "allow the request";
		case "deny":
			return "deny the request";
		case "route":
			return routePhrase(action, names);
		case "cap":
			return capPhrase(action);
	}
}

function routePhrase(action: PolicyAction, names: PolicyNames): string {
	const [target, ...fallbacks] = action.route_chain ?? [];
	if (target === undefined) {
		throw new Error("policy route chain missing outcome=route");
	}
	return [
		`route to ${names.model(target)}`,
		...fallbacks.map(
			(fallback) => `or ${names.model(fallback)} if that has no price`,
		),
		...Object.entries(action.overrides ?? {}).map(([key, value]) =>
			routeOverridePhrase(key, value),
		),
	].join(", ");
}

function capPhrase(action: PolicyAction): string {
	const settings = Object.entries(action.overrides ?? {}).map(([key, value]) =>
		capOverridePhrase(key, value),
	);
	const settingsPart =
		settings.length === 0 ? "" : ` to ${settings.join(", ")}`;
	const limitPart =
		action.limit === null || action.limit === undefined
			? ""
			: ` and allow at most ${limitPhrase(action.limit)} this period`;
	return settingsPart === "" && limitPart === ""
		? "cap the request"
		: `cap${settingsPart}${limitPart}`;
}
