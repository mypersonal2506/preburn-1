import type { Problem, ProblemError } from "@/client";

/** The fields an ApiProblem is built from. */
export interface ApiProblemFields {
	type: string;
	title: string;
	status: number;
	code: string;
	detail: string;
	errors: readonly ProblemError[];
	retryAfterSeconds: number | null;
}

type ProblemBody = Pick<
	Problem,
	"type" | "title" | "status" | "code" | "detail" | "errors"
>;

const PROBLEM_CONTENT_TYPE = "application/problem+json";
const RETRY_AFTER_HEADER = "Retry-After";
const UNEXPECTED_RESPONSE_TYPE = "about:blank";
const UNEXPECTED_RESPONSE_TITLE = "Unexpected response";
const UNEXPECTED_RESPONSE_CODE = "unexpected_response";
const UNEXPECTED_RESPONSE_DETAIL = "unexpected response";
const NO_RESPONSE_STATUS = 0;
const AUTHENTICATION_REQUIRED_CODE = "authentication_required";

/**
 * An error answer of the Preburn API, with the fields of the generated
 * Problem type. `code` is the stable error code of docs/errors.md, `type`
 * links to its entry, `title` is the text of the HTTP status, `errors` lists
 * invalid request fields by location such as `body.email`, and
 * `retryAfterSeconds` holds the Retry-After wait of a rate_limited answer. An
 * answer that is not a problem, or no answer at all, has the code
 * `unexpected_response`, the type `about:blank` and the title "Unexpected
 * response", with status 0 when no response arrived.
 */
export class ApiProblem extends Error {
	readonly type: string;
	readonly title: string;
	readonly status: number;
	readonly code: string;
	readonly detail: string;
	readonly errors: readonly ProblemError[];
	readonly retryAfterSeconds: number | null;

	constructor(fields: ApiProblemFields) {
		super(`api problem status=${fields.status} code=${fields.code}`);
		this.name = "ApiProblem";
		this.type = fields.type;
		this.title = fields.title;
		this.status = fields.status;
		this.code = fields.code;
		this.detail = fields.detail;
		this.errors = fields.errors;
		this.retryAfterSeconds = fields.retryAfterSeconds;
	}
}

/**
 * Reads the body of a response that is not ok into an ApiProblem. A body
 * that is not an application/problem+json problem becomes
 * `unexpected_response` with the response status.
 */
export async function readApiProblem(response: Response): Promise<ApiProblem> {
	const body: unknown =
		response.headers.get("Content-Type") === PROBLEM_CONTENT_TYPE
			? await response.json()
			: null;
	if (!isProblemBody(body)) {
		return unexpectedResponse(response.status);
	}
	const retryAfter = response.headers.get(RETRY_AFTER_HEADER);
	return new ApiProblem({
		type: body.type,
		title: body.title,
		status: body.status,
		code: body.code,
		detail: body.detail,
		errors: body.errors ?? [],
		retryAfterSeconds: retryAfter === null ? null : Number(retryAfter),
	});
}

/**
 * Returns thrown unchanged when it is an ApiProblem, and otherwise an
 * `unexpected_response` ApiProblem with status 0, such as for a network
 * failure. Screens pass every caught API error through it.
 */
export function toApiProblem(thrown: unknown): ApiProblem {
	if (thrown instanceof ApiProblem) {
		return thrown;
	}
	return unexpectedResponse(NO_RESPONSE_STATUS);
}

/**
 * Tells whether thrown is an ApiProblem with the code
 * `authentication_required`, the answer to a request without a live session.
 * `login_failed` is also a 401 and does not count.
 */
export function isAuthenticationRequired(thrown: unknown): boolean {
	return (
		thrown instanceof ApiProblem && thrown.code === AUTHENTICATION_REQUIRED_CODE
	);
}

function unexpectedResponse(status: number): ApiProblem {
	return new ApiProblem({
		type: UNEXPECTED_RESPONSE_TYPE,
		title: UNEXPECTED_RESPONSE_TITLE,
		status,
		code: UNEXPECTED_RESPONSE_CODE,
		detail: UNEXPECTED_RESPONSE_DETAIL,
		errors: [],
		retryAfterSeconds: null,
	});
}

function isProblemBody(body: unknown): body is ProblemBody {
	return (
		typeof body === "object" &&
		body !== null &&
		"type" in body &&
		typeof body.type === "string" &&
		"title" in body &&
		typeof body.title === "string" &&
		"status" in body &&
		typeof body.status === "number" &&
		"code" in body &&
		typeof body.code === "string" &&
		"detail" in body &&
		typeof body.detail === "string" &&
		(!("errors" in body) ||
			(Array.isArray(body.errors) && body.errors.every(isProblemError)))
	);
}

function isProblemError(value: unknown): value is ProblemError {
	return (
		typeof value === "object" &&
		value !== null &&
		"location" in value &&
		typeof value.location === "string" &&
		"message" in value &&
		typeof value.message === "string"
	);
}
