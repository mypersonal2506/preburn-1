import type { HoldTimeRow, PlanDraft } from "@/features/plans/plan-draft";
import { toApiProblem } from "@/lib/api-problem";
import {
	type FieldProblems,
	type FormProblem,
	mapProblemToForm,
} from "@/lib/form-problem";

/**
 * The parts of the plan sentence a problem shows at: the name blank, the
 * rule blank with the mode and its value, and the hold times under Advanced.
 */
export type PlanField = "name" | "rule" | "hold_times";

/** A problem with one hold time row and the control of the row it marks. */
export interface HoldTimeProblem {
	part: "feature" | "seconds";
	message: string;
}

/**
 * What keeps a draft from saving: a message per sentence part, and for each
 * hold time row its problem or null.
 */
export interface PlanDraftProblems {
	fields: Partial<Record<PlanField, string>>;
	holdTimes: (HoldTimeProblem | null)[];
}

const NAME_LENGTH_MAXIMUM = 80;
const TARGET_MARGIN_MAXIMUM = 0.9999;
const HOLD_TIME_MINIMUM_SECONDS = 30;
const HOLD_TIME_MAXIMUM_SECONDS = 86_400;
const NAME_TAKEN_CODE = "plan_name_taken";

const NAME_MISSING_MESSAGE = "Enter a name";
const NAME_TOO_LONG_MESSAGE = `Use at most ${NAME_LENGTH_MAXIMUM} characters`;
const NAME_RULE_MESSAGE = `Use 1 to ${NAME_LENGTH_MAXIMUM} characters`;
const NAME_TAKEN_MESSAGE = "Another plan has this name";
const TARGET_MARGIN_MISSING_MESSAGE = "Enter a target margin";
const TARGET_MARGIN_RULE_MESSAGE = "Use 0% to 99.99%";
const ALLOWANCE_MISSING_MESSAGE = "Enter an allowance";
const ALLOWANCE_RULE_MESSAGE = "Enter 0 or more";
const HOLD_TIMES_MESSAGE = "Fix the hold times under Advanced";
const HOLD_TIME_FEATURE_MISSING_MESSAGE = "Pick a feature";
const HOLD_TIME_FEATURE_REPEATED_MESSAGE = "Feature already has a hold time";
const HOLD_TIME_SECONDS_RULE_MESSAGE = "Use 30 seconds to 24 hours";

const PLAN_FIELD_PROBLEMS: FieldProblems<PlanField> = {
	"body.name": { field: "name", message: NAME_RULE_MESSAGE },
	"body.target_margin": { field: "rule", message: TARGET_MARGIN_RULE_MESSAGE },
	"body.allowance": { field: "rule", message: ALLOWANCE_RULE_MESSAGE },
};

/**
 * The problems that keep draft from saving, checked like the API checks a
 * plan: a name of 1 to 80 characters once trimmed, a target margin from 0%
 * to 99.99% in margin target mode or an allowance of 0 or more in fixed
 * allowance mode, and hold time rows each naming their own feature with 30
 * seconds to 24 hours. Any hold time problem also sets the hold_times field.
 */
export function planDraftProblems(draft: PlanDraft): PlanDraftProblems {
	const fields: Partial<Record<PlanField, string>> = {};
	const nameMessage = nameProblem(draft.name);
	if (nameMessage !== null) {
		fields.name = nameMessage;
	}
	const ruleMessage =
		draft.mode === "margin_target"
			? targetMarginProblem(draft.target_margin)
			: allowanceProblem(draft.allowance);
	if (ruleMessage !== null) {
		fields.rule = ruleMessage;
	}
	const holdTimes = draft.hold_times.map((row, index) =>
		holdTimeProblem(row, draft.hold_times.slice(0, index)),
	);
	if (holdTimes.some((problem) => problem !== null)) {
		fields.hold_times = HOLD_TIMES_MESSAGE;
	}
	return { fields, holdTimes };
}

/**
 * Maps the error of a failed plan save onto the sentence: plan_name_taken
 * and the name, target margin and allowance errors at their blanks with plan
 * messages, and everything else to the form list as mapProblemToForm does.
 * A null error maps to no messages.
 */
export function planRequestProblem(error: unknown): FormProblem<PlanField> {
	if (error !== null && toApiProblem(error).code === NAME_TAKEN_CODE) {
		return { fields: { name: [NAME_TAKEN_MESSAGE] } };
	}
	return mapProblemToForm(error, PLAN_FIELD_PROBLEMS);
}

function nameProblem(name: string): string | null {
	const length = Array.from(name.trim()).length;
	if (length === 0) {
		return NAME_MISSING_MESSAGE;
	}
	return length > NAME_LENGTH_MAXIMUM ? NAME_TOO_LONG_MESSAGE : null;
}

function targetMarginProblem(targetMargin: string | null): string | null {
	if (targetMargin === null) {
		return TARGET_MARGIN_MISSING_MESSAGE;
	}
	const ratio = Number(targetMargin);
	return ratio < 0 || ratio > TARGET_MARGIN_MAXIMUM
		? TARGET_MARGIN_RULE_MESSAGE
		: null;
}

function allowanceProblem(allowance: string | null): string | null {
	if (allowance === null) {
		return ALLOWANCE_MISSING_MESSAGE;
	}
	return Number(allowance) < 0 ? ALLOWANCE_RULE_MESSAGE : null;
}

function holdTimeProblem(
	row: HoldTimeRow,
	earlierRows: readonly HoldTimeRow[],
): HoldTimeProblem | null {
	if (row.feature === null) {
		return { part: "feature", message: HOLD_TIME_FEATURE_MISSING_MESSAGE };
	}
	if (earlierRows.some((earlierRow) => earlierRow.feature === row.feature)) {
		return { part: "feature", message: HOLD_TIME_FEATURE_REPEATED_MESSAGE };
	}
	if (
		row.seconds === null ||
		row.seconds < HOLD_TIME_MINIMUM_SECONDS ||
		row.seconds > HOLD_TIME_MAXIMUM_SECONDS
	) {
		return { part: "seconds", message: HOLD_TIME_SECONDS_RULE_MESSAGE };
	}
	return null;
}
