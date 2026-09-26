import type {
	PolicyAction,
	PolicyCondition,
	PolicyConditionGroup,
	PolicyResponse,
} from "@/client";
import {
	formatCount,
	formatMoney,
	formatPace,
	formatPercent,
} from "@/lib/format";
import { attributeValueLabel, conditionOperatorLabel } from "@/lib/labels";

/** A signal a policy condition compares, such as pace. */
export type PolicySignal = PolicyCondition["signal"];

/** What a policy decides when it matches: allow, route, cap or deny. */
export type PolicyOutcome = PolicyAction["outcome"];

/** Who a policy applies to: everyone, the customers of a plan or a customer. */
export type PolicyLevel = PolicyResponse["level"];

/** One provider model of a route chain. */
export type RouteTarget = NonNullable<PolicyAction["route_chain"]>[number];

/** The value of a route or cap override. */
export type OverrideValue = NonNullable<PolicyAction["overrides"]>[string];

/** The per period limit of a cap. */
export type PolicyLimit = NonNullable<PolicyAction["limit"]>;

/** Whether a condition group needs all or any of its members to hold. */
export type ConditionMatch = "all" | "any";

/** The unit a signal's condition values are typed and shown in. */
export type SignalUnit = "amount" | "pace" | "percent" | "count";

/** How a policy is enforced and what it does unreachable or unpriced. */
export type EnforcementSettings = Pick<
	PolicyResponse,
	"enforcement" | "on_unreachable" | "on_uncosted"
>;

/** A condition group of 1 to 3 conditions, which sentences show inline. */
export interface InlineConditions {
	match: ConditionMatch;
	conditions: PolicyCondition[];
}

/** A member of a condition group: a condition or a nested group. */
export type ConditionGroupMember = PolicyCondition | PolicyConditionGroup;

/** The members of a condition group and whether all or any must hold. */
export interface ConditionGroupMembers {
	match: ConditionMatch;
	members: ConditionGroupMember[];
}

interface SignalWords {
	phrase: string;
	unit: SignalUnit;
}

type ValueFormat = (value: string) => string;

/** Every signal in the order the sentence builder offers them. */
export const POLICY_SIGNALS: readonly [PolicySignal, ...PolicySignal[]] = [
	"allowance_remaining",
	"pace",
	"projected_margin",
	"cost_to_date",
	"period_revenue_net",
	"elapsed_fraction",
	"request_estimated_cost",
	"period_decision_count",
];

const INLINE_CONDITIONS_MAXIMUM = 3;

const SIGNAL_WORDS: Record<PolicySignal, SignalWords> = {
	allowance_remaining: { phrase: "allowance left", unit: "amount" },
	pace: { phrase: "pace", unit: "pace" },
	projected_margin: { phrase: "projected margin", unit: "percent" },
	cost_to_date: { phrase: "spend this period", unit: "amount" },
	period_revenue_net: { phrase: "revenue this period", unit: "amount" },
	elapsed_fraction: { phrase: "period elapsed", unit: "percent" },
	request_estimated_cost: { phrase: "request cost", unit: "amount" },
	period_decision_count: { phrase: "requests this period", unit: "count" },
};

const UNIT_FORMATS: Record<SignalUnit, ValueFormat> = {
	amount: formatMoney,
	pace: formatPace,
	percent: formatPercent,
	count: (value) => formatCount(Number(value)),
};

const MATCH_WORDS: Record<ConditionMatch, string> = { all: "and", any: "or" };

const ENFORCEMENT_WORDS: Record<EnforcementSettings["enforcement"], string> = {
	soft: "Soft",
	hard: "Hard",
};

const PARAMETER_KEY_SEPARATOR_PATTERN = /_/g;

/** Phrases a signal as sentences read it, such as "allowance left". */
export function signalPhrase(signal: PolicySignal): string {
	return SIGNAL_WORDS[signal].phrase;
}

/** The unit a signal's condition values are typed and shown in. */
export function signalUnit(signal: PolicySignal): SignalUnit {
	return SIGNAL_WORDS[signal].unit;
}

/**
 * Phrases the comparison of a condition with a value, such as "above 2.0x",
 * "at most $0.00" or "below -50.0%". Throws for a value that is not a
 * decimal.
 */
export function comparisonPhrase(condition: PolicyCondition): string {
	const value = UNIT_FORMATS[signalUnit(condition.signal)](condition.value);
	return `${conditionOperatorLabel(condition.operator)} ${value}`;
}

/** Joins the conditions of a group: "and" for all, "or" for any. */
export function matchWord(match: ConditionMatch): string {
	return MATCH_WORDS[match];
}

/**
 * The members of a condition group. Throws for a group with neither all nor
 * any, which the API never sends.
 */
export function conditionGroupMembers(
	group: PolicyConditionGroup,
): ConditionGroupMembers {
	if (group.any !== undefined) {
		return { match: "any", members: group.any };
	}
	if (group.all !== undefined) {
		return { match: "all", members: group.all };
	}
	throw new Error("condition group invalid members=none");
}

/** Tells whether a group always holds: it needs all of no members. */
export function isAlways(group: PolicyConditionGroup): boolean {
	return group.all !== undefined && group.all.length === 0;
}

/**
 * The conditions of a group that sentences show inline: 1 to 3 conditions
 * and no nested group. Null for any other group.
 */
export function inlineConditions(
	group: PolicyConditionGroup,
): InlineConditions | null {
	const { match, members } = conditionGroupMembers(group);
	const conditions = members.filter(isCondition);
	if (
		conditions.length !== members.length ||
		conditions.length === 0 ||
		conditions.length > INLINE_CONDITIONS_MAXIMUM
	) {
		return null;
	}
	return { match, conditions };
}

/**
 * Phrases a group too deep or long to show inline by its members, such as
 * "all of 5 conditions" or "any of 1 condition".
 */
export function conditionGroupPhrase(group: PolicyConditionGroup): string {
	const { match, members } = conditionGroupMembers(group);
	const noun = members.length === 1 ? "condition" : "conditions";
	return `${match} of ${formatCount(members.length)} ${noun}`;
}

/** Tells whether a condition group member is a condition. */
export function isCondition(
	member: ConditionGroupMember,
): member is PolicyCondition {
	return "signal" in member;
}

/** Names an override parameter for display, such as "image size". */
export function parameterLabel(key: string): string {
	return key.replace(PARAMETER_KEY_SEPARATOR_PATTERN, " ");
}

/**
 * Phrases a route override, such as "at 5s", "at 720p" or "without audio".
 */
export function routeOverridePhrase(key: string, value: OverrideValue): string {
	const label = attributeValueLabel(key, value);
	return typeof value === "boolean" ? label : `at ${label}`;
}

/** Phrases a cap override, such as "4s", "720p" or "no audio". */
export function capOverridePhrase(key: string, value: OverrideValue): string {
	return value === false
		? `no ${parameterLabel(key)}`
		: attributeValueLabel(key, value);
}

/**
 * Summarizes the Advanced settings of a policy in one line, such as "Soft,
 * allow if unreachable or unpriced" or "Hard, allow if unreachable, deny if
 * unpriced".
 */
export function enforcementSummary(settings: EnforcementSettings): string {
	const enforcement = ENFORCEMENT_WORDS[settings.enforcement];
	if (settings.on_unreachable === settings.on_uncosted) {
		return `${enforcement}, ${settings.on_unreachable} if unreachable or unpriced`;
	}
	return `${enforcement}, ${settings.on_unreachable} if unreachable, ${settings.on_uncosted} if unpriced`;
}

/**
 * Phrases a cap limit, such as "20 requests" or "$25.00 of AI cost". Throws
 * for a value that is not a decimal.
 */
export function limitPhrase(limit: PolicyLimit): string {
	if (limit.kind === "amount") {
		return `${formatMoney(limit.value)} of AI cost`;
	}
	const count = Number(limit.value);
	return `${formatCount(count)} ${count === 1 ? "request" : "requests"}`;
}
