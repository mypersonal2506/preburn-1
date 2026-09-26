import type { CreatePlanData, PlanResponse, UpdatePlanData } from "@/client";
import { zCreatePlanBody, zUpdatePlanBody } from "@/client/zod.gen";
import { formatCount, formatMoney, formatPercent } from "@/lib/format";
import { parseRequestBody } from "@/lib/request-body";

/** How a plan sets its customers' AI cost allowance. */
export type PlanMode = PlanResponse["mode"];

/**
 * One reservation hold time of a plan draft: a feature and the seconds a
 * check reserves its cost. Each is null until the member picks it.
 */
export interface HoldTimeRow {
	feature: string | null;
	seconds: number | null;
}

/**
 * A plan as the plan sentence edits it: API values named like the request
 * body, with the hold times as rows. `target_margin` and `allowance` are null
 * until typed, and a draft keeps both while its mode shows one of them.
 */
export interface PlanDraft {
	name: string;
	mode: PlanMode;
	target_margin: string | null;
	allowance: string | null;
	hold_times: HoldTimeRow[];
}

/** The body of a plan create request. */
export type CreatePlanBody = CreatePlanData["body"];

/** The body of a plan update request. */
export type UpdatePlanBody = UpdatePlanData["body"];

type HoldTimes = PlanResponse["hold_times"];

/** The draft of a new plan: a margin target plan with every blank empty. */
export function newPlanDraft(): PlanDraft {
	return {
		name: "",
		mode: "margin_target",
		target_margin: null,
		allowance: null,
		hold_times: [],
	};
}

/** The draft of a stored plan, with a row per hold time. */
export function draftFromPlan(plan: PlanResponse): PlanDraft {
	return {
		name: plan.name,
		mode: plan.mode,
		target_margin: plan.target_margin,
		allowance: plan.allowance,
		hold_times: Object.entries(plan.hold_times).map(([feature, seconds]) => ({
			feature,
			seconds,
		})),
	};
}

/**
 * The create request body of a draft: the trimmed name, the mode with the
 * value it needs, the target margin or the allowance, and the hold times.
 * Throws for a draft missing a value, which planDraftProblems reports
 * first, and `request schema mismatch` for a body the API schema rejects.
 */
export function createPlanBody(draft: PlanDraft): CreatePlanBody {
	const name = draft.name.trim();
	const holdTimes = holdTimesBody(draft.hold_times);
	const body: CreatePlanBody =
		draft.mode === "margin_target"
			? {
					name,
					mode: draft.mode,
					target_margin: requiredValue(draft.target_margin, "target_margin"),
					hold_times: holdTimes,
				}
			: {
					name,
					mode: draft.mode,
					allowance: requiredValue(draft.allowance, "allowance"),
					hold_times: holdTimes,
				};
	return parseRequestBody(zCreatePlanBody, body);
}

/**
 * The update request body that turns plan into draft: only the fields that
 * change. A margin target draft sends its target margin when it changed, and
 * a fixed allowance draft its allowance, so switching to fixed allowance
 * keeps the stored target margin and switching back drops the allowance.
 * Hold times are sent whole when any changed. Throws like createPlanBody.
 */
export function updatePlanBody(
	plan: PlanResponse,
	draft: PlanDraft,
): UpdatePlanBody {
	const body: UpdatePlanBody = {};
	const name = draft.name.trim();
	if (name !== plan.name) {
		body.name = name;
	}
	if (draft.mode !== plan.mode) {
		body.mode = draft.mode;
	}
	if (draft.mode === "margin_target") {
		const targetMargin = requiredValue(draft.target_margin, "target_margin");
		if (targetMargin !== plan.target_margin) {
			body.target_margin = targetMargin;
		}
	} else {
		const allowance = requiredValue(draft.allowance, "allowance");
		if (allowance !== plan.allowance) {
			body.allowance = allowance;
		}
	}
	const holdTimes = holdTimesBody(draft.hold_times);
	if (!sameHoldTimes(holdTimes, plan.hold_times)) {
		body.hold_times = holdTimes;
	}
	return parseRequestBody(zUpdatePlanBody, body);
}

/**
 * Names the rule of a plan, such as "40.0% margin target" or "$2.00 fixed
 * allowance". Throws for a fixed allowance plan without an allowance, which
 * the API never sends.
 */
export function planRule(plan: PlanResponse): string {
	if (plan.mode === "margin_target") {
		return `${formatPercent(plan.target_margin)} margin target`;
	}
	return `${formatMoney(requiredValue(plan.allowance, "allowance"))} fixed allowance`;
}

/**
 * The one-line summary of the hold times in the Advanced settings: "Default
 * hold times" without rows, else the count, such as "2 custom hold times".
 */
export function holdTimesSummary(rows: readonly HoldTimeRow[]): string {
	if (rows.length === 0) {
		return "Default hold times";
	}
	const noun = rows.length === 1 ? "hold time" : "hold times";
	return `${formatCount(rows.length)} custom ${noun}`;
}

function holdTimesBody(rows: readonly HoldTimeRow[]): HoldTimes {
	return Object.fromEntries(
		rows.map(
			(row) =>
				[
					requiredValue(row.feature, "hold_times.feature"),
					requiredValue(row.seconds, "hold_times.seconds"),
				] as const,
		),
	);
}

function sameHoldTimes(left: HoldTimes, right: HoldTimes): boolean {
	const leftFeatures = Object.keys(left);
	return (
		leftFeatures.length === Object.keys(right).length &&
		leftFeatures.every((feature) => left[feature] === right[feature])
	);
}

function requiredValue<Value>(value: Value | null, field: string): Value {
	if (value === null) {
		throw new Error(`plan value missing field=${field}`);
	}
	return value;
}
