import type { PolicyCondition, PolicyConditionGroup } from "@/client";
import type { OverrideParameter } from "@/features/policies/override-parameters";
import {
	CAP_SETTINGS_BLANK,
	conditionSignalBlank,
	LIMIT_BLANK,
	routeOverrideBlank,
	routeTargetBlank,
	WHEN_BLANK,
} from "@/features/policies/policy-blanks";
import {
	type PolicyDraft,
	type PolicyDraftAction,
	unpickedRouteTarget,
} from "@/features/policies/policy-draft";
import {
	type ConditionGroupMember,
	type ConditionMatch,
	conditionGroupMembers,
	inlineConditions,
	isAlways,
	POLICY_SIGNALS,
} from "@/features/policies/policy-phrases";

/**
 * An optional clause the (+) menu of a policy sentence offers: its label,
 * the draft with the clause added and the blank of the clause to open.
 */
export interface PolicyClause {
	label: string;
	draft: PolicyDraft;
	openedBlank: string;
}

const ROUTE_CHAIN_MAXIMUM = 5;

/** How a new condition compares its signal with its value. */
export const NEW_CONDITION_OPERATOR: PolicyCondition["operator"] = "gt";

/**
 * The optional clauses draft does not have yet. Conditions that read inline
 * or always add a Condition on the first signal, which opens its signal or,
 * as a fourth condition, the condition panel, and a Condition group, which
 * opens the condition panel. A route adds a Fallback model up to 5 models
 * and a Setting from parameters not set yet. A cap adds a Limit, and a
 * Setting once only its limit shows, while its settings blank adds the
 * others.
 */
export function policyClauses(
	draft: PolicyDraft,
	parameters: readonly OverrideParameter[],
): PolicyClause[] {
	return [...conditionClauses(draft), ...outcomeClauses(draft, parameters)];
}

/**
 * Writes a condition group holding members, under `all` or `any` by match.
 */
export function writeConditionGroup(
	match: ConditionMatch,
	members: ConditionGroupMember[],
): PolicyConditionGroup {
	return match === "all" ? { all: members } : { any: members };
}

function conditionClauses(draft: PolicyDraft): PolicyClause[] {
	if (!isAlways(draft.when) && inlineConditions(draft.when) === null) {
		return [];
	}
	const { match, members } = conditionGroupMembers(draft.when);
	const [signal] = POLICY_SIGNALS;
	const withCondition = writeConditionGroup(match, [
		...members,
		{ signal, operator: NEW_CONDITION_OPERATOR, value: "" },
	]);
	return [
		{
			label: "Condition",
			draft: { ...draft, when: withCondition },
			openedBlank:
				inlineConditions(withCondition) === null
					? WHEN_BLANK
					: conditionSignalBlank(members.length),
		},
		{
			label: "Condition group",
			draft: {
				...draft,
				when: writeConditionGroup(match, [...members, { all: [] }]),
			},
			openedBlank: WHEN_BLANK,
		},
	];
}

function outcomeClauses(
	draft: PolicyDraft,
	parameters: readonly OverrideParameter[],
): PolicyClause[] {
	const { action } = draft;
	const unsetParameter = parameters.find(
		(parameter) => !Object.hasOwn(action.overrides, parameter.key),
	);
	const withAction = (
		label: string,
		nextAction: PolicyDraftAction,
		openedBlank: string,
	): PolicyClause => ({
		label,
		draft: { ...draft, action: nextAction },
		openedBlank,
	});
	const clauses: PolicyClause[] = [];
	if (
		action.outcome === "route" &&
		action.route_chain.length < ROUTE_CHAIN_MAXIMUM
	) {
		clauses.push(
			withAction(
				"Fallback model",
				{
					...action,
					route_chain: [...action.route_chain, unpickedRouteTarget()],
				},
				routeTargetBlank(action.route_chain.length),
			),
		);
	}
	if (action.outcome === "route" && unsetParameter !== undefined) {
		clauses.push(
			withAction(
				"Setting",
				{
					...action,
					overrides: { ...action.overrides, [unsetParameter.key]: null },
				},
				routeOverrideBlank(unsetParameter.key),
			),
		);
	}
	if (
		action.outcome === "cap" &&
		action.limit !== null &&
		Object.keys(action.overrides).length === 0 &&
		unsetParameter !== undefined
	) {
		clauses.push(
			withAction(
				"Setting",
				{ ...action, overrides: { [unsetParameter.key]: null } },
				CAP_SETTINGS_BLANK,
			),
		);
	}
	if (action.outcome === "cap" && action.limit === null) {
		clauses.push(
			withAction(
				"Limit",
				{ ...action, limit: { kind: "count", value: "" } },
				LIMIT_BLANK,
			),
		);
	}
	return clauses;
}
