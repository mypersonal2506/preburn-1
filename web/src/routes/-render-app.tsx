import { QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { vi } from "vitest";
import type { MemberResponse, SettingsResponse } from "@/client";
import { RouteError } from "@/components/route-error";
import { RouteNotFound } from "@/components/route-not-found";
import { RoutePending } from "@/components/route-pending";
import { type Environment, getEnvironment } from "@/lib/environment-store";
import { createQueryClient } from "@/lib/query-client";
import { routeTree } from "@/routeTree.gen";

/** Fake API answers keyed by method and path, such as `GET /api/v1/settings`. */
export type ApiAnswers = Record<string, () => Response | Promise<Response>>;

/**
 * An answer the fake API holds back until the test releases it with the
 * answer to send, for a request that must stay pending while the test acts.
 */
export interface HeldAnswer {
	answer: () => Promise<Response>;
	release: (answer: () => Response) => void;
}

/** The member the fake API signs in. */
export const signedInMember: MemberResponse = {
	id: "mem_01jbvagescfn78y0938nkrkayd",
	email: "sam@example.com",
	display_name: "Sam Rivera",
	has_password: true,
	status: "active",
	last_login_at: "2026-09-26T08:00:00Z",
	created_at: "2026-09-01T00:00:00Z",
};

/** The installation settings the fake API answers with. */
export const installationSettings: SettingsResponse = {
	installation_name: "Acme AI",
	default_plan_id: null,
	stripe_customer_metadata_key: "preburn_customer_id",
};

/** Answers of a completed installation with signedInMember signed in. */
export const signedInAnswers: ApiAnswers = {
	"GET /api/v1/setup/status": jsonAnswer({ setup_required: false }),
	"GET /api/v1/auth/me": jsonAnswer(signedInMember),
	"GET /api/v1/settings": jsonAnswer(installationSettings),
	"POST /api/v1/auth/logout": () => new Response(null, { status: 204 }),
};

/** Answers a JSON body with status 200. */
export function jsonAnswer(body: unknown): () => Response {
	return () =>
		new Response(JSON.stringify(body), {
			headers: { "Content-Type": "application/json" },
		});
}

/** Answers a problem with status and code. */
export function problemAnswer(status: number, code: string): () => Response {
	return () =>
		new Response(
			JSON.stringify({
				type: `https://github.com/preburn/preburn/blob/main/docs/errors.md#${code}`,
				title: "Error",
				status,
				detail: code,
				code,
			}),
			{ status, headers: { "Content-Type": "application/problem+json" } },
		);
}

/** Holds the answer to one request until the test releases it. */
export function holdAnswer(): HeldAnswer {
	const { promise, resolve } = Promise.withResolvers<Response>();
	return {
		answer: () => promise,
		release: (answer) => resolve(answer()),
	};
}

/**
 * Answers with the answer of the environment the dashboard works in when
 * the request is sent, such as a record that exists in test only.
 */
export function answerByEnvironment(
	answers: Record<Environment, () => Response>,
): () => Response {
	return () => answers[getEnvironment()]();
}

/**
 * Replaces fetch with a fake API that answers from answers and fails loud on
 * any other request. Returns the list every sent request is appended to.
 */
export function stubApi(answers: ApiAnswers): Request[] {
	const requests: Request[] = [];
	vi.stubGlobal(
		"fetch",
		vi.fn(async (request: Request) => {
			requests.push(request);
			const route = `${request.method} ${new URL(request.url).pathname}`;
			const answer = answers[route];
			if (answer === undefined) {
				throw new Error(`unexpected request route=${route}`);
			}
			return answer();
		}),
	);
	return requests;
}

/**
 * Renders the dashboard at path with the router and query client wired as in
 * main.tsx.
 */
export function renderApp(path: string) {
	const queryClient = createQueryClient(() =>
		router.navigate({ to: "/login" }),
	);
	const router = createRouter({
		routeTree,
		context: { queryClient },
		history: createMemoryHistory({ initialEntries: [path] }),
		defaultPendingComponent: RoutePending,
		defaultErrorComponent: RouteError,
		defaultNotFoundComponent: RouteNotFound,
	});
	render(
		<QueryClientProvider client={queryClient}>
			<RouterProvider router={router} />
		</QueryClientProvider>,
	);
	return { router, queryClient };
}
