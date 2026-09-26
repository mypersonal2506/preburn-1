import {
	CAP_SETTINGS_BLANK,
	conditionComparisonBlank,
	conditionSignalBlank,
	FEATURE_BLANK,
	LIMIT_BLANK,
	NAME_BLANK,
	OUTCOME_BLANK,
	routeOverrideBlank,
	routeTargetBlank,
	WHEN_BLANK,
	WHO_BLANK,
} from "@/features/policies/policy-blanks";
import type {
	PolicyDraft,
	PolicyProblem,
} from "@/features/policies/policy-draft";
import { inlineConditions } from "@/features/policies/policy-phrases";
import { toApiProblem } from "@/lib/api-problem";
import { bodyFieldProblems, mapProblemToForm } from "@/lib/form-problem";

/**
 * Where the problems of a draft show: the message of each blank that has
 * problems, and the card's error list, which holds every message once.
 */
export interface PolicyProblemView {
	blankMessage: (blank: string) => string | undefined;
	messages: string[];
}

const BODY_LOCATION_PREFIX = "body.";
const BLANK_MESSAGE_SEPARATOR = ". ";
const WHO_LOCATIONS: ReadonlySet<string> = new Set([
	"level",
	"plan_id",
	"customer_id",
]);
const CONDITION_LOCATION_PATTERN =
	/^when\.(all|any)\[(\d+)\](?:\.(signal|operator|value))?$/;
const ROUTE_TARGET_LOCATION_PATTERN = /^action\.route_chain\[(\d+)\]/;
const OVERRIDE_LOCATION_PREFIX = "action.overrides.";

const LOCATION_PHRASES: Readonly<Record<string, string>> = {
	enforcement: "Enforcement",
	on_unreachable: "When Preburn is unreachable",
	on_uncosted: "When a request has no price",
	status: "Status",
};

const CODE_PROBLEMS: Readonly<Record<string, PolicyProblem>> = {
	plan_not_found: { location: "plan_id", message: "Choose an active plan" },
};

/**
 * The problems of a failed policy request: one per field error at its body
 * location without the `body.` prefix, an inactive or unknown plan at the
 * plan, and otherwise one problem with the whole request.
 */
export function serverProblems(error: unknown): PolicyProblem[] {
	const problem = toApiProblem(error);
	const codeProblem = CODE_PROBLEMS[problem.code];
	if (codeProblem !== undefined) {
		return [codeProblem];
	}
	const locations = problem.errors.map(({ location }) =>
		withoutBodyPrefix(location),
	);
	const formProblem = mapProblemToForm(error, bodyFieldProblems(locations));
	return [
		...Object.entries(formProblem.fields).flatMap(([location, messages]) =>
			(messages ?? []).map((message) => ({ location, message })),
		),
		...(formProblem.form ?? []).map((message) => ({ location: null, message })),
	];
}

/**
 * Places the problems of draft: each at the blank that shows its location
 * (the most specific one the sentence renders), and every message in the
 * error list. A problem no blank shows reads "{phrase}: {message}" in the
 * list, such as "When Preburn is unreachable: {message}".
 */
export function policyProblemView(
	problems: readonly PolicyProblem[],
	draft: PolicyDraft,
): PolicyProblemView {
	const blankMessages = new Map<string, string[]>();
	const messages = problems.map(({ location, message }) => {
		if (location === null) {
			return message;
		}
		const blank = problemBlank(location, draft);
		if (blank === null) {
			return `${LOCATION_PHRASES[location] ?? location}: ${message}`;
		}
		blankMessages.set(blank, [...(blankMessages.get(blank) ?? []), message]);
		return message;
	});
	return {
		blankMessage: (blank) => {
			const messagesAtBlank = blankMessages.get(blank);
			return messagesAtBlank === undefined
				? undefined
				: [...new Set(messagesAtBlank)].join(BLANK_MESSAGE_SEPARATOR);
		},
		messages,
	};
}

function problemBlank(location: string, draft: PolicyDraft): string | null {
	if (location === NAME_BLANK) {
		return NAME_BLANK;
	}
	if (WHO_LOCATIONS.has(location)) {
		return WHO_BLANK;
	}
	if (location === FEATURE_BLANK) {
		return FEATURE_BLANK;
	}
	if (isWithin(location, "when")) {
		return conditionBlank(location, draft);
	}
	if (isWithin(location, "action")) {
		return actionBlank(location, draft);
	}
	return null;
}

function conditionBlank(location: string, draft: PolicyDraft): string {
	const inline = inlineConditions(draft.when);
	const match = CONDITION_LOCATION_PATTERN.exec(location);
	if (inline === null || match === null) {
		return WHEN_BLANK;
	}
	const [, groupMatch, position = "", field] = match;
	const index = Number(position);
	if (groupMatch !== inline.match || index >= inline.conditions.length) {
		return WHEN_BLANK;
	}
	return field === "signal"
		? conditionSignalBlank(index)
		: conditionComparisonBlank(index);
}

function actionBlank(location: string, draft: PolicyDraft): string {
	const { action } = draft;
	const routeTarget = ROUTE_TARGET_LOCATION_PATTERN.exec(location);
	if (routeTarget !== null) {
		const index = Number(routeTarget[1]);
		return index < action.route_chain.length
			? routeTargetBlank(index)
			: OUTCOME_BLANK;
	}
	if (isWithin(location, "action.overrides") && action.outcome === "cap") {
		return CAP_SETTINGS_BLANK;
	}
	if (location.startsWith(OVERRIDE_LOCATION_PREFIX)) {
		const key = location.slice(OVERRIDE_LOCATION_PREFIX.length);
		return Object.hasOwn(action.overrides, key)
			? routeOverrideBlank(key)
			: OUTCOME_BLANK;
	}
	if (isWithin(location, "action.limit") && action.limit !== null) {
		return LIMIT_BLANK;
	}
	return OUTCOME_BLANK;
}

function isWithin(location: string, path: string): boolean {
	return (
		location === path ||
		location.startsWith(`${path}.`) ||
		location.startsWith(`${path}[`)
	);
}

function withoutBodyPrefix(location: string): string {
	return location.startsWith(BODY_LOCATION_PREFIX)
		? location.slice(BODY_LOCATION_PREFIX.length)
		: location;
}
