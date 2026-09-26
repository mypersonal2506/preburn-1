import {
	MutationObserver,
	type QueryClient,
	QueryObserver,
} from "@tanstack/react-query";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { SettingsResponse } from "@/client";
import {
	getCurrentMemberOptions,
	getSettingsOptions,
	getSettingsQueryKey,
	loginMutation,
	updateSettingsMutation,
} from "@/client/@tanstack/react-query.gen";
import { ApiProblem } from "@/lib/api-problem";
import { setEnvironment } from "@/lib/environment-store";
import { createQueryClient } from "@/lib/query-client";

interface PendingResponse {
	request: Request;
	respond: (settings: SettingsResponse) => void;
}

const onAuthenticationRequired = vi.fn(async () => undefined);

function apiProblem(status: number, code: string): ApiProblem {
	return new ApiProblem({
		type: `https://github.com/preburn/preburn/blob/main/docs/errors.md#${code}`,
		title: "Error",
		status,
		code,
		detail: code,
		errors: [],
		retryAfterSeconds: null,
	});
}

function respondWithProblem(status: number, code: string): void {
	vi.stubGlobal(
		"fetch",
		vi.fn(
			async () =>
				new Response(
					JSON.stringify({
						type: `https://github.com/preburn/preburn/blob/main/docs/errors.md#${code}`,
						title: "Error",
						status,
						detail: code,
						code,
					}),
					{
						status,
						headers: { "Content-Type": "application/problem+json" },
					},
				),
		),
	);
}

function holdResponses(): PendingResponse[] {
	const pending: PendingResponse[] = [];
	vi.stubGlobal(
		"fetch",
		vi.fn(
			(request: Request) =>
				new Promise<Response>((resolve) => {
					pending.push({
						request,
						respond: (settings) =>
							resolve(
								new Response(JSON.stringify(settings), {
									headers: { "Content-Type": "application/json" },
								}),
							),
					});
				}),
		),
	);
	return pending;
}

function pendingAt(pending: PendingResponse[], index: number): PendingResponse {
	const response = pending[index];
	if (response === undefined) {
		throw new Error(`pending response missing index=${index}`);
	}
	return response;
}

function installationSettings(installationName: string): SettingsResponse {
	return {
		installation_name: installationName,
		default_plan_id: null,
		stripe_customer_metadata_key: "preburn_customer_id",
	};
}

async function fetchFailingQuery(
	queryClient: QueryClient,
	failures: unknown[],
): Promise<number> {
	let calls = 0;
	const result = queryClient
		.fetchQuery({
			queryKey: ["failing"],
			queryFn: async () => {
				const failure = failures[calls];
				calls += 1;
				if (failure !== undefined) {
					throw failure;
				}
				return "loaded";
			},
		})
		.catch(() => "failed");
	await vi.runAllTimersAsync();
	await result;
	return calls;
}

beforeEach(() => {
	onAuthenticationRequired.mockClear();
});

afterEach(() => {
	setEnvironment("test");
	vi.useRealTimers();
	vi.unstubAllGlobals();
});

test("cached data stays fresh for 30 seconds", () => {
	const queryClient = createQueryClient(onAuthenticationRequired);

	expect(queryClient.getDefaultOptions().queries?.staleTime).toBe(30_000);
});

test.each([
	["a 5xx problem", apiProblem(503, "counters_unavailable")],
	["a network error", new TypeError("Failed to fetch")],
])("a query retries after %s", async (_name, failure) => {
	vi.useFakeTimers();
	const queryClient = createQueryClient(onAuthenticationRequired);

	const calls = await fetchFailingQuery(queryClient, [failure]);

	expect(calls).toBe(2);
});

test("a query retries a lasting 5xx three times", async () => {
	vi.useFakeTimers();
	const queryClient = createQueryClient(onAuthenticationRequired);
	const failure = apiProblem(500, "internal_error");

	const calls = await fetchFailingQuery(queryClient, [
		failure,
		failure,
		failure,
		failure,
		failure,
	]);

	expect(calls).toBe(4);
});

test.each([
	["a 4xx problem", apiProblem(422, "validation_failed")],
	["a 401 problem", apiProblem(401, "authentication_required")],
	["an unexpected error", new Error("request schema mismatch path=email")],
])("a query does not retry after %s", async (_name, failure) => {
	vi.useFakeTimers();
	const queryClient = createQueryClient(onAuthenticationRequired);

	const calls = await fetchFailingQuery(queryClient, [failure]);

	expect(calls).toBe(1);
});

test("a query answered with authentication_required sends the member to login", async () => {
	respondWithProblem(401, "authentication_required");
	const queryClient = createQueryClient(onAuthenticationRequired);

	await expect(
		queryClient.fetchQuery(getCurrentMemberOptions()),
	).rejects.toBeInstanceOf(ApiProblem);

	expect(onAuthenticationRequired).toHaveBeenCalledTimes(1);
});

test("a mutation answered with authentication_required sends the member to login", async () => {
	respondWithProblem(401, "authentication_required");
	const queryClient = createQueryClient(onAuthenticationRequired);
	const mutation = new MutationObserver(queryClient, updateSettingsMutation());

	await expect(
		mutation.mutate({ body: { installation_name: "Acme" } }),
	).rejects.toBeInstanceOf(ApiProblem);

	expect(onAuthenticationRequired).toHaveBeenCalledTimes(1);
});

test("a failed login stays on the login page", async () => {
	respondWithProblem(401, "login_failed");
	const queryClient = createQueryClient(onAuthenticationRequired);
	const mutation = new MutationObserver(queryClient, loginMutation());

	await expect(
		mutation.mutate({
			body: { email: "sam@example.com", password: "wrong password here" },
		}),
	).rejects.toMatchObject({ status: 401, code: "login_failed" });

	expect(onAuthenticationRequired).not.toHaveBeenCalled();
});

test("switching environment drops the cached data of the other environment", () => {
	const queryClient = createQueryClient(onAuthenticationRequired);
	queryClient.setQueryData(["overview"], { environment: "test" });

	setEnvironment("live");

	expect(queryClient.getQueryData(["overview"])).toBeUndefined();
});

test("the redirect to login drops the cached data once it lands", async () => {
	respondWithProblem(401, "authentication_required");
	const { promise: redirected, resolve: finishRedirect } =
		Promise.withResolvers<undefined>();
	const redirectToLogin = vi.fn(() => redirected);
	const queryClient = createQueryClient(redirectToLogin);
	queryClient.setQueryData(["overview"], { environment: "test" });

	await expect(
		queryClient.fetchQuery(getCurrentMemberOptions()),
	).rejects.toBeInstanceOf(ApiProblem);
	expect(redirectToLogin).toHaveBeenCalledTimes(1);
	expect(queryClient.getQueryData(["overview"])).toBeDefined();

	finishRedirect(undefined);

	await vi.waitFor(() => {
		expect(queryClient.getQueryData(["overview"])).toBeUndefined();
	});
});

test("a test response that arrives after a switch to live is never shown", async () => {
	const pending = holdResponses();
	const queryClient = createQueryClient(onAuthenticationRequired);
	const observer = new QueryObserver(queryClient, getSettingsOptions());
	let switched = false;
	const shownAfterSwitch = new Set<string>();
	const unsubscribe = observer.subscribe((result) => {
		if (switched && result.data !== undefined) {
			shownAfterSwitch.add(result.data.installation_name);
		}
	});
	await vi.waitFor(() => {
		expect(pending).toHaveLength(1);
	});
	pendingAt(pending, 0).respond(installationSettings("Test installation"));
	await vi.waitFor(() => {
		expect(queryClient.getQueryData(getSettingsQueryKey())).toBeDefined();
	});
	void observer.refetch();
	await vi.waitFor(() => {
		expect(pending).toHaveLength(2);
	});

	switched = true;
	setEnvironment("live");
	await vi.waitFor(() => {
		expect(pending).toHaveLength(3);
	});
	pendingAt(pending, 1).respond(installationSettings("Late test installation"));
	pendingAt(pending, 2).respond(installationSettings("Live installation"));

	await vi.waitFor(() => {
		expect(observer.getCurrentResult().data?.installation_name).toBe(
			"Live installation",
		);
	});
	expect([...shownAfterSwitch]).toEqual(["Live installation"]);
	expect(
		pending.map(({ request }) => request.headers.get("X-Preburn-Environment")),
	).toEqual(["test", "test", "live"]);
	unsubscribe();
});
