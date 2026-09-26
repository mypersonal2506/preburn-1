import type {
	CreatePolicyData,
	PolicyConditionGroup,
	PolicyResponse,
	UpdatePolicyData,
} from "@/client";
import type { CustomerChoice } from "@/components/pickers/customer-picker";
import {
	conditionGroupMembers,
	isCondition,
	type OverrideValue,
	type PolicyLevel,
	type PolicyLimit,
	type PolicyOutcome,
	type RouteTarget,
} from "@/features/policies/policy-phrases";
import { featureLabel } from "@/lib/labels";

/**
 * The action of a policy draft. A route target's model is empty and an
 * override's value is null until the member picks one.
 */
export interface PolicyDraftAction {
	outcome: PolicyOutcome;
	route_chain: RouteTarget[];
	overrides: Record<string, OverrideValue | null>;
	limit: PolicyLimit | null;
}

/**
 * A policy as the sentence builder edits it: API values named like the
 * request body, so server errors find their blanks. `name` is null while
 * the suggested name stands, `customer` carries the names of the customer a
 * customer policy applies to, and a condition value is empty until typed.
 */
export interface PolicyDraft {
	name: string | null;
	level: PolicyLevel;
	plan_id: string | null;
	customer: CustomerChoice | null;
	feature: string | null;
	when: PolicyConditionGroup;
	action: PolicyDraftAction;
	enforcement: PolicyResponse["enforcement"];
	on_unreachable: PolicyResponse["on_unreachable"];
	on_uncosted: PolicyResponse["on_uncosted"];
	status: PolicyResponse["status"];
}

/**
 * A problem with a draft at a request body location without the `body.`
 * prefix, such as `when.all[0].value`, or null for a problem with the whole
 * request, and its message.
 */
export interface PolicyProblem {
	location: string | null;
	message: string;
}

/** The body of a policy create or preview request. */
export type CreatePolicyBody = CreatePolicyData["body"];

/** The body of a policy update request. */
export type UpdatePolicyBody = UpdatePolicyData["body"];

const OUTCOME_NAMES: Record<PolicyOutcome, string> = {
	allow: "Allow",
	route: "Route",
	cap: "Cap",
	deny: "Deny",
};

/**
 * A new route target whose model the member has not picked yet. Each call
 * returns a new object, because the sentence keys route targets by identity.
 */
export function unpickedRouteTarget(): RouteTarget {
	return { provider: "", model: "" };
}

/**
 * The draft of a stored policy. A customer policy needs customer, the
 * customer it applies to, and throws without it.
 */
export function draftFromPolicy(
	policy: PolicyResponse,
	customer: CustomerChoice | null,
): PolicyDraft {
	if (policy.level === "customer" && customer?.id !== policy.customer_id) {
		throw new Error(`policy customer missing policy_id=${policy.id}`);
	}
	return {
		name: policy.name,
		level: policy.level,
		plan_id: policy.plan_id,
		customer: policy.level === "customer" ? customer : null,
		feature: policy.feature,
		when: policy.when,
		action: {
			outcome: policy.action.outcome,
			route_chain: policy.action.route_chain ?? [],
			overrides: policy.action.overrides ?? {},
			limit: policy.action.limit ?? null,
		},
		enforcement: policy.enforcement,
		on_unreachable: policy.on_unreachable,
		on_uncosted: policy.on_uncosted,
		status: policy.status,
	};
}

/**
 * The create or preview request body of a draft named name. Throws for an
 * override without a value, which draftProblems reports first.
 */
export function createPolicyBody(
	draft: PolicyDraft,
	name: string,
): CreatePolicyBody {
	return { ...updatePolicyBody(draft, name), status: draft.status };
}

/**
 * The update request body of a draft named name: the whole document except
 * the status, which the page menu changes. Throws for an override without a
 * value, which draftProblems reports first.
 */
export function updatePolicyBody(
	draft: PolicyDraft,
	name: string,
): Required<Omit<UpdatePolicyBody, "status">> {
	const overrides = Object.entries(draft.action.overrides);
	return {
		name,
		level: draft.level,
		plan_id: draft.plan_id,
		customer_id: draft.customer?.id ?? null,
		feature: draft.feature,
		when: draft.when,
		action: {
			outcome: draft.action.outcome,
			route_chain:
				draft.action.route_chain.length === 0 ? null : draft.action.route_chain,
			overrides:
				overrides.length === 0
					? null
					: Object.fromEntries(overrides.map(pickedOverride)),
			limit: draft.action.limit,
		},
		enforcement: draft.enforcement,
		on_unreachable: draft.on_unreachable,
		on_uncosted: draft.on_uncosted,
	};
}

/**
 * The blanks of a draft the member has left empty, each at its request body
 * location: an empty name, the plan or customer the level needs, condition
 * values, route models, override values and the limit. Every other rule is
 * the server's.
 */
export function draftProblems(draft: PolicyDraft): PolicyProblem[] {
	const problems: PolicyProblem[] = [];
	if (draft.name !== null && draft.name.trim() === "") {
		problems.push({ location: "name", message: "Enter a name" });
	}
	if (draft.level === "plan" && draft.plan_id === null) {
		problems.push({ location: "plan_id", message: "Choose a plan" });
	}
	if (draft.level === "customer" && draft.customer === null) {
		problems.push({ location: "customer_id", message: "Choose a customer" });
	}
	problems.push(...conditionProblems(draft.when, "when"));
	draft.action.route_chain.forEach((target, index) => {
		if (target.model === "") {
			problems.push({
				location: `action.route_chain[${index}]`,
				message: "Choose a model",
			});
		}
	});
	for (const [key, value] of Object.entries(draft.action.overrides)) {
		if (value === null) {
			problems.push({
				location: `action.overrides.${key}`,
				message: "Choose a value",
			});
		}
	}
	if (draft.action.limit?.value === "") {
		problems.push({ location: "action.limit.value", message: "Enter a limit" });
	}
	return problems;
}

/**
 * Suggests a name from the sentence, such as "Route text to video for
 * Creator customers". planName is the name of the draft's plan, null while
 * it loads or before one is picked.
 */
export function suggestPolicyName(
	draft: PolicyDraft,
	planName: string | null,
): string {
	const requests =
		draft.feature === null ? "requests" : featureLabel(draft.feature);
	return `${OUTCOME_NAMES[draft.action.outcome]} ${requests} for ${suggestedAudience(draft, planName)}`;
}

/**
 * Tells whether two drafts hold the same policy document, whatever the order
 * of their object keys. The status is left out, because the page menu
 * changes it apart from the sentence.
 */
export function sameDocument(left: PolicyDraft, right: PolicyDraft): boolean {
	return (
		canonicalJson({ ...left, status: null }) ===
		canonicalJson({ ...right, status: null })
	);
}

/** The name a customer choice shows: its display name, else its external id. */
export function customerChoiceName(customer: CustomerChoice): string {
	return customer.display_name ?? customer.external_id;
}

function suggestedAudience(
	draft: PolicyDraft,
	planName: string | null,
): string {
	switch (draft.level) {
		case "everyone":
			return "everyone";
		case "plan":
			return planName === null ? "a plan" : `${planName} customers`;
		case "customer":
			return draft.customer === null
				? "a customer"
				: customerChoiceName(draft.customer);
	}
}

function conditionProblems(
	group: PolicyConditionGroup,
	path: string,
): PolicyProblem[] {
	const { match, members } = conditionGroupMembers(group);
	return members.flatMap((member, index) => {
		const memberPath = `${path}.${match}[${index}]`;
		if (!isCondition(member)) {
			return conditionProblems(member, memberPath);
		}
		return member.value === ""
			? [{ location: `${memberPath}.value`, message: "Enter a value" }]
			: [];
	});
}

function canonicalJson(value: unknown): string {
	return JSON.stringify(value, (_key, nested: unknown) =>
		isRecord(nested)
			? Object.fromEntries(
					Object.entries(nested).toSorted(([leftKey], [rightKey]) =>
						leftKey.localeCompare(rightKey),
					),
				)
			: nested,
	);
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function pickedOverride([key, value]: [string, OverrideValue | null]): [
	string,
	OverrideValue,
] {
	if (value === null) {
		throw new Error(`policy override value missing key=${key}`);
	}
	return [key, value];
}
