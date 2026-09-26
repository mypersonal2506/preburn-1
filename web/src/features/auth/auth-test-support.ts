import type { ProblemError } from "@/client";

/** Extra parts of a problem answer: field errors and the Retry-After wait. */
export interface ProblemDetails {
	errors?: ProblemError[];
	retryAfterSeconds?: number;
}

/**
 * Answers a problem with status and code, plus its field errors and a
 * Retry-After header when details carry them.
 */
export function detailedProblemAnswer(
	status: number,
	code: string,
	details: ProblemDetails,
): () => Response {
	const headers = new Headers({ "Content-Type": "application/problem+json" });
	if (details.retryAfterSeconds !== undefined) {
		headers.set("Retry-After", String(details.retryAfterSeconds));
	}
	return () =>
		new Response(
			JSON.stringify({
				type: `https://github.com/preburn/preburn/blob/main/docs/errors.md#${code}`,
				title: "Error",
				status,
				detail: code,
				code,
				errors: details.errors,
			}),
			{ status, headers },
		);
}

/** Lists the sent requests as routes, such as `POST /api/v1/setup`. */
export function sentRoutes(requests: readonly Request[]): string[] {
	return requests.map(requestRoute);
}

/**
 * Returns the JSON body of the one request sent to route, such as
 * `POST /api/v1/setup`. Throws unless exactly one was sent.
 */
export async function sentBody(
	requests: readonly Request[],
	route: string,
): Promise<unknown> {
	const matching = requests.filter(
		(request) => requestRoute(request) === route,
	);
	const [request] = matching;
	if (request === undefined || matching.length > 1) {
		throw new Error(
			`sent requests unexpected route=${route} count=${matching.length}`,
		);
	}
	return request.clone().json();
}

function requestRoute(request: Request): string {
	return `${request.method} ${new URL(request.url).pathname}`;
}
