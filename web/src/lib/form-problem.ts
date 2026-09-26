import { type ApiProblem, toApiProblem } from "@/lib/api-problem";

/**
 * The form field a server error at one location shows at, and the message it
 * shows there. Without a message the field shows the server's message.
 */
export interface FieldProblem<FieldName extends string = string> {
	field: FieldName;
	message?: string;
}

/** The field problems of a form by error location, such as `body.email`. */
export type FieldProblems<FieldName extends string = string> = Readonly<
	Record<string, FieldProblem<FieldName>>
>;

/** Form messages by API problem code, each built from the problem. */
export type CodeMessages = Readonly<
	Record<string, (problem: ApiProblem) => string>
>;

/**
 * The messages a form shows for a failed request, in the shape TanStack
 * Form's `setErrorMap` takes under `onSubmit`: every message of each form
 * field, and the messages for the form's error list, absent when there are
 * none.
 */
export interface FormProblem<FieldName extends string = string> {
	fields: Partial<Record<FieldName, string[]>>;
	form?: string[];
}

const UNEXPECTED_PROBLEM_MESSAGE = "Something went wrong. Try again.";
const BODY_LOCATION_PREFIX = "body.";
const SECONDS_PER_MINUTE = 60;
const LOCALE = "en-US";

const secondsFormat = new Intl.NumberFormat(LOCALE, {
	style: "unit",
	unit: "second",
	unitDisplay: "long",
});
const minutesFormat = new Intl.NumberFormat(LOCALE, {
	style: "unit",
	unit: "minute",
	unitDisplay: "long",
});

/**
 * Maps the error of a failed form request onto the form, and a null error
 * (no failed request) onto no messages. Each field error whose location
 * fieldProblems lists shows at its field. Every other field error goes to
 * the form list as "location: message", without the `body.` prefix. The
 * form list also gets the codeMessages message for the problem's code, and
 * "Something went wrong. Try again." when the problem has neither field
 * errors nor a code message, as does an error that is not an API problem.
 *
 * Render-time forms read the result from the mutation error. Forms whose
 * fields are named like the request body pass bodyFieldProblems and hand
 * the result to TanStack Form's `setErrorMap` under `onSubmit`, so a
 * field's error clears when the field changes.
 */
export function mapProblemToForm<FieldName extends string>(
	error: unknown,
	fieldProblems: FieldProblems<FieldName>,
	codeMessages: CodeMessages = {},
): FormProblem<FieldName> {
	if (error === null) {
		return { fields: {} };
	}
	const problem = toApiProblem(error);
	const fields: Partial<Record<FieldName, string[]>> = {};
	const formMessages: string[] = [];
	const codeMessage = codeMessages[problem.code];
	if (codeMessage !== undefined) {
		formMessages.push(codeMessage(problem));
	}
	for (const { location, message } of problem.errors) {
		const fieldProblem = fieldProblems[location];
		if (fieldProblem === undefined) {
			formMessages.push(`${withoutBodyPrefix(location)}: ${message}`);
		} else {
			fields[fieldProblem.field] = [
				...(fields[fieldProblem.field] ?? []),
				fieldProblem.message ?? message,
			];
		}
	}
	if (formMessages.length === 0 && problem.errors.length === 0) {
		formMessages.push(UNEXPECTED_PROBLEM_MESSAGE);
	}
	return formMessages.length === 0
		? { fields }
		: { fields, form: formMessages };
}

/**
 * The field problems of a form whose fields are named like the request
 * body, such as `when.all[0].value`: an error at `body.<name>` shows its
 * server message at the field of that name. Pass
 * `Object.keys(form.fieldInfo)` as fieldNames.
 */
export function bodyFieldProblems(
	fieldNames: readonly string[],
): FieldProblems {
	return Object.fromEntries(
		fieldNames.map((field) => [`${BODY_LOCATION_PREFIX}${field}`, { field }]),
	);
}

/**
 * The message for a rate_limited problem with its Retry-After wait, in
 * seconds under a minute and in minutes rounded up otherwise, such as "Too
 * many attempts. Try again in 15 minutes." Throws when the problem has no
 * Retry-After wait, which the API always sends with rate_limited.
 */
export function rateLimitMessage(problem: ApiProblem): string {
	const waitSeconds = problem.retryAfterSeconds;
	if (waitSeconds === null) {
		throw new Error(`retry after missing code=${problem.code}`);
	}
	const wait =
		waitSeconds < SECONDS_PER_MINUTE
			? secondsFormat.format(waitSeconds)
			: minutesFormat.format(Math.ceil(waitSeconds / SECONDS_PER_MINUTE));
	return `Too many attempts. Try again in ${wait}.`;
}

function withoutBodyPrefix(location: string): string {
	return location.startsWith(BODY_LOCATION_PREFIX)
		? location.slice(BODY_LOCATION_PREFIX.length)
		: location;
}
