import type { CreateClientConfig } from "@/client/client.gen";
import { readApiProblem } from "@/lib/api-problem";
import { getEnvironment } from "@/lib/environment-store";

const CSRF_COOKIE_NAME = "preburn_csrf";
const CSRF_HEADER = "X-CSRF-Token";
const ENVIRONMENT_HEADER = "X-Preburn-Environment";
const CSRF_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

/**
 * Runtime configuration of the generated API client. Requests go to the
 * page's own origin with its cookies, carry `X-Preburn-Environment` from the
 * environment store and, on POST, PUT, PATCH and DELETE, `X-CSRF-Token` from
 * the `preburn_csrf` cookie. A response that is not ok rejects with an
 * ApiProblem.
 */
export const createClientConfig: CreateClientConfig = (config) => ({
	...config,
	baseUrl: "",
	credentials: "same-origin",
	fetch: fetchWithSession,
});

async function fetchWithSession(
	input: RequestInfo | URL,
	init?: RequestInit,
): Promise<Response> {
	const response = await fetch(addSessionHeaders(new Request(input, init)));
	if (!response.ok) {
		throw await readApiProblem(response);
	}
	return response;
}

function addSessionHeaders(request: Request): Request {
	request.headers.set(ENVIRONMENT_HEADER, getEnvironment());
	const csrfToken = readCookie(CSRF_COOKIE_NAME);
	if (CSRF_METHODS.has(request.method) && csrfToken !== undefined) {
		request.headers.set(CSRF_HEADER, csrfToken);
	}
	return request;
}

function readCookie(name: string): string | undefined {
	const prefix = `${name}=`;
	return document.cookie
		.split("; ")
		.find((cookie) => cookie.startsWith(prefix))
		?.slice(prefix.length);
}
