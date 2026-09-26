import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type RenderResult, render } from "@testing-library/react";
import type { ReactElement } from "react";
import { vi } from "vitest";
import type { Problem } from "@/client";

/**
 * Stubs fetch so every API request gets the response answer builds for its
 * URL, and returns the list of requested URLs, which grows as requests go
 * out. Call `vi.unstubAllGlobals()` after each test.
 */
export function stubApi(answer: (url: URL) => Response): URL[] {
	const requestedUrls: URL[] = [];
	vi.stubGlobal(
		"fetch",
		vi.fn(async (request: Request) => {
			const url = new URL(request.url);
			requestedUrls.push(url);
			return answer(url);
		}),
	);
	return requestedUrls;
}

/** Returns a 200 response with body as JSON. */
export function jsonResponse(body: unknown): Response {
	return new Response(JSON.stringify(body), {
		headers: { "Content-Type": "application/json" },
	});
}

/** Returns an application/problem+json response carrying problem. */
export function problemResponse(problem: Problem): Response {
	return new Response(JSON.stringify(problem), {
		status: problem.status,
		headers: { "Content-Type": "application/problem+json" },
	});
}

/**
 * Renders ui inside a QueryClientProvider whose queries never retry. The
 * provider stays around ui on rerender.
 */
export function renderWithQueries(ui: ReactElement): RenderResult {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(ui, {
		wrapper: ({ children }) => (
			<QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
		),
	});
}
