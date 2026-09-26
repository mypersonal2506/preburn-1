import { afterEach, beforeEach, expect, test, vi } from "vitest";
import {
	completeSetup,
	getSetupStatus,
	login,
	revokeApiKey,
	updateSettings,
	upsertCustomer,
} from "@/client";
import { ApiProblem } from "@/lib/api-problem";
import { setEnvironment } from "@/lib/environment-store";

const csrfToken = crypto.randomUUID();

const sentRequests: Request[] = [];

function respondWith(response: () => Response): void {
	vi.stubGlobal(
		"fetch",
		vi.fn(async (request: Request) => {
			sentRequests.push(request);
			return response();
		}),
	);
}

function jsonResponse(body: unknown): Response {
	return new Response(JSON.stringify(body), {
		headers: { "Content-Type": "application/json" },
	});
}

function lastRequest(): Request {
	const request = sentRequests.at(-1);
	if (request === undefined) {
		throw new Error("no request sent");
	}
	return request;
}

function writeCookie(cookie: string): void {
	// biome-ignore lint/suspicious/noDocumentCookie: jsdom has no Cookie Store API.
	document.cookie = cookie;
}

beforeEach(() => {
	sentRequests.length = 0;
	respondWith(() => jsonResponse({ setup_required: false }));
});

afterEach(() => {
	writeCookie("preburn_csrf=; path=/; max-age=0");
	setEnvironment("test");
	vi.unstubAllGlobals();
});

test("requests go to the same origin path with same-origin credentials", async () => {
	await getSetupStatus({ throwOnError: true });

	const request = lastRequest();
	expect(new URL(request.url).pathname).toBe("/api/v1/setup/status");
	expect(request.credentials).toBe("same-origin");
});

test("a GET carries the environment header and no CSRF header", async () => {
	writeCookie(`preburn_csrf=${csrfToken}; path=/`);

	await getSetupStatus({ throwOnError: true });

	const request = lastRequest();
	expect(request.method).toBe("GET");
	expect(request.headers.get("X-Preburn-Environment")).toBe("test");
	expect(request.headers.has("X-CSRF-Token")).toBe(false);
});

test.each([
	[
		"POST",
		() =>
			login({
				body: { email: "sam@example.com", password: "correct horse battery" },
				throwOnError: true,
			}),
	],
	[
		"PATCH",
		() =>
			updateSettings({
				body: { installation_name: "Acme" },
				throwOnError: true,
			}),
	],
	[
		"PUT",
		() =>
			upsertCustomer({
				path: { external_id: "customer-42" },
				body: {},
				throwOnError: true,
			}),
	],
	[
		"DELETE",
		() =>
			revokeApiKey({
				path: { api_key_id: "key_01jbvagescfn78y0938nkrkayd" },
				throwOnError: true,
			}),
	],
])(
	"a %s carries the CSRF cookie as a header and the environment header",
	async (method, send) => {
		writeCookie(`preburn_csrf=${csrfToken}; path=/`);

		await send();

		const request = lastRequest();
		expect(request.method).toBe(method);
		expect(request.headers.get("X-CSRF-Token")).toBe(csrfToken);
		expect(request.headers.get("X-Preburn-Environment")).toBe("test");
	},
);

test("a POST without the CSRF cookie carries no CSRF header", async () => {
	await login({
		body: { email: "sam@example.com", password: "correct horse battery" },
		throwOnError: true,
	});

	expect(lastRequest().headers.has("X-CSRF-Token")).toBe(false);
});

test("the environment header follows the environment store", async () => {
	setEnvironment("live");

	await getSetupStatus({ throwOnError: true });

	expect(lastRequest().headers.get("X-Preburn-Environment")).toBe("live");
});

test("a problem response rejects with an ApiProblem holding its field errors", async () => {
	respondWith(
		() =>
			new Response(
				JSON.stringify({
					type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#validation_failed",
					title: "Unprocessable Entity",
					status: 422,
					detail: "validation failed",
					code: "validation_failed",
					errors: [
						{
							location: "body.password",
							message: "expected 12 to 256 characters",
						},
					],
				}),
				{
					status: 422,
					headers: { "Content-Type": "application/problem+json" },
				},
			),
	);

	const rejection = completeSetup({
		body: {
			email: "sam@example.com",
			display_name: "Sam",
			password: "short",
			token: "setup-token",
		},
		throwOnError: true,
	});

	await expect(rejection).rejects.toBeInstanceOf(ApiProblem);
	await expect(rejection).rejects.toMatchObject({
		status: 422,
		code: "validation_failed",
		errors: [
			{ location: "body.password", message: "expected 12 to 256 characters" },
		],
	});
});
