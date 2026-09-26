import { QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { MemberResponse } from "@/client";
import { setEnvironment } from "@/lib/environment-store";
import {
	FakeEventSource,
	latestEventSource,
} from "@/lib/event-source-test-support";
import { createQueryClient } from "@/lib/query-client";
import {
	type DecisionEvent,
	PAUSED_DECISIONS_MAXIMUM,
	useEventStream,
} from "@/lib/use-event-stream";

function decisionEvent(id: string): DecisionEvent {
	return {
		id,
		created_at: "2026-09-26T10:15:30.123456789Z",
		customer_id: "cust_01jbvagescfn78y0938nkrkayd",
		customer_external_id: "customer-42",
		customer_display_name: "Acme",
		feature: "text_to_video",
		requested_model: "veo-3.1",
		model: "veo-3.1-fast",
		outcome: "route",
		reason: "policy_matched",
		estimated_cost: "0.400000000",
		matched_policy_id: "pol_01jbvagescfn78y0938nkrkayd",
	};
}

function streamQuery(source: FakeEventSource): URLSearchParams {
	return new URL(source.url, window.location.origin).searchParams;
}

function renderEventStream(
	keepsDecision?: (decision: DecisionEvent) => boolean,
) {
	const received: DecisionEvent[] = [];
	const redirectToLogin = vi.fn(async () => undefined);
	const queryClient = createQueryClient(redirectToLogin);
	const rendered = renderHook(
		() =>
			useEventStream((decision) => {
				received.push(decision);
			}, keepsDecision),
		{
			wrapper: ({ children }: { children: ReactNode }) =>
				createElement(QueryClientProvider, { client: queryClient }, children),
		},
	);
	return { ...rendered, received, redirectToLogin };
}

function answerSessionChecks(status: number, body: unknown): Request[] {
	const sessionChecks: Request[] = [];
	vi.stubGlobal(
		"fetch",
		vi.fn(async (request: Request) => {
			sessionChecks.push(request);
			if (new URL(request.url).pathname !== "/api/v1/auth/me") {
				throw new Error(`unexpected request url=${request.url}`);
			}
			return new Response(JSON.stringify(body), {
				status,
				headers: {
					"Content-Type":
						status === 200 ? "application/json" : "application/problem+json",
				},
			});
		}),
	);
	return sessionChecks;
}

const signedInMember: MemberResponse = {
	id: "mem_01jbvagescfn78y0938nkrkayd",
	email: "sam@example.com",
	display_name: "Sam Rivera",
	has_password: true,
	status: "active",
	last_login_at: "2026-09-26T08:00:00Z",
	created_at: "2026-09-01T00:00:00Z",
};

const endedSession = {
	type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#authentication_required",
	title: "Unauthorized",
	status: 401,
	detail: "authentication required",
	code: "authentication_required",
};

let sessionChecks: Request[] = [];

beforeEach(() => {
	FakeEventSource.opened = [];
	vi.useFakeTimers();
	vi.stubGlobal("EventSource", FakeEventSource);
	sessionChecks = answerSessionChecks(200, signedInMember);
});

afterEach(() => {
	cleanup();
	setEnvironment("test");
	vi.useRealTimers();
	vi.unstubAllGlobals();
});

test("connects to the decision stream of the current environment", () => {
	const { result } = renderEventStream();

	const source = latestEventSource();
	expect(new URL(source.url, window.location.origin).pathname).toBe(
		"/api/v1/dashboard/decisions/stream",
	);
	expect(streamQuery(source).get("environment")).toBe("test");
	expect(streamQuery(source).has("last_event_id")).toBe(false);
	expect(result.current.status).toBe("connecting");
});

test("moves from connecting to live to reconnecting and back to live", () => {
	const { result } = renderEventStream();

	act(() => latestEventSource().open());
	expect(result.current.status).toBe("live");

	act(() => latestEventSource().fail());
	expect(result.current.status).toBe("reconnecting");
	expect(latestEventSource().closed).toBe(true);

	act(() => vi.advanceTimersByTime(1_000));
	expect(FakeEventSource.opened).toHaveLength(2);
	expect(result.current.status).toBe("reconnecting");

	act(() => latestEventSource().open());
	expect(result.current.status).toBe("live");
});

test("delivers each decision while live", () => {
	const { received } = renderEventStream();
	act(() => latestEventSource().open());

	act(() =>
		latestEventSource().sendDecision(
			"1726488000000-0",
			decisionEvent("dec_first"),
		),
	);

	expect(received.map((decision) => decision.id)).toEqual(["dec_first"]);
	expect(received[0]).toEqual(decisionEvent("dec_first"));
});

test("buffers while paused and delivers the buffer in order on resume", () => {
	const { result, received } = renderEventStream();
	act(() => latestEventSource().open());

	act(() => result.current.pause());
	expect(result.current.status).toBe("paused");

	act(() => {
		latestEventSource().sendDecision(
			"1726488000000-0",
			decisionEvent("dec_first"),
		);
		latestEventSource().sendDecision(
			"1726488000001-0",
			decisionEvent("dec_second"),
		);
	});
	expect(received).toEqual([]);
	expect(result.current.newCount).toBe(2);

	act(() => result.current.resume());
	expect(result.current.status).toBe("live");
	expect(result.current.newCount).toBe(0);
	expect(received.map((decision) => decision.id)).toEqual([
		"dec_first",
		"dec_second",
	]);
});

test("stays paused while the connection drops and recovers", () => {
	const { result } = renderEventStream();
	act(() => latestEventSource().open());
	act(() => result.current.pause());

	act(() => latestEventSource().fail());
	expect(result.current.status).toBe("paused");

	act(() => result.current.resume());
	expect(result.current.status).toBe("reconnecting");
});

test("reconnects with the last event id", () => {
	renderEventStream();
	act(() => latestEventSource().open());
	act(() =>
		latestEventSource().sendDecision(
			"1726488000000-0",
			decisionEvent("dec_first"),
		),
	);

	act(() => latestEventSource().fail());
	act(() => vi.advanceTimersByTime(1_000));

	const reconnected = streamQuery(latestEventSource());
	expect(reconnected.get("environment")).toBe("test");
	expect(reconnected.get("last_event_id")).toBe("1726488000000-0");
});

test("doubles the reconnect delay up to 30 seconds", () => {
	renderEventStream();
	const expectedDelays = [1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000];

	for (const delay of expectedDelays) {
		const openedBefore = FakeEventSource.opened.length;
		act(() => latestEventSource().fail());

		act(() => vi.advanceTimersByTime(delay - 1));
		expect(FakeEventSource.opened).toHaveLength(openedBefore);

		act(() => vi.advanceTimersByTime(1));
		expect(FakeEventSource.opened).toHaveLength(openedBefore + 1);
	}
});

test("a received decision resets the reconnect delay", () => {
	renderEventStream();
	act(() => latestEventSource().fail());
	act(() => vi.advanceTimersByTime(1_000));
	act(() => latestEventSource().fail());
	act(() => vi.advanceTimersByTime(2_000));

	act(() => latestEventSource().open());
	act(() =>
		latestEventSource().sendDecision(
			"1726488000000-0",
			decisionEvent("dec_first"),
		),
	);
	act(() => latestEventSource().fail());
	const openedBefore = FakeEventSource.opened.length;
	act(() => vi.advanceTimersByTime(1_000));

	expect(FakeEventSource.opened).toHaveLength(openedBefore + 1);
});

test("switching environment reconnects to the other stream from its next decision", () => {
	const { result } = renderEventStream();
	act(() => latestEventSource().open());
	act(() => result.current.pause());
	act(() =>
		latestEventSource().sendDecision(
			"1726488000000-0",
			decisionEvent("dec_first"),
		),
	);
	const testSource = latestEventSource();

	act(() => setEnvironment("live"));

	expect(testSource.closed).toBe(true);
	const liveQuery = streamQuery(latestEventSource());
	expect(liveQuery.get("environment")).toBe("live");
	expect(liveQuery.has("last_event_id")).toBe(false);
	expect(result.current.newCount).toBe(0);
});

test("unmounting closes the stream and cancels a pending reconnect", () => {
	const { unmount } = renderEventStream();
	act(() => latestEventSource().fail());
	const openedBefore = FakeEventSource.opened.length;

	unmount();
	act(() => vi.advanceTimersByTime(60_000));

	expect(FakeEventSource.opened).toHaveLength(openedBefore);
});

test("unmounting a live stream closes it", () => {
	const { unmount } = renderEventStream();
	act(() => latestEventSource().open());

	unmount();

	expect(latestEventSource().closed).toBe(true);
});

test("a paused stream keeps the newest decisions and counts every new one", () => {
	const { result, received } = renderEventStream();
	act(() => latestEventSource().open());
	act(() => result.current.pause());
	const sentCount = PAUSED_DECISIONS_MAXIMUM + 3;

	act(() => {
		for (let position = 1; position <= sentCount; position++) {
			latestEventSource().sendDecision(
				`1726488000000-${position}`,
				decisionEvent(`dec_${position}`),
			);
		}
	});
	expect(result.current.newCount).toBe(sentCount);

	act(() => result.current.resume());
	expect(received).toHaveLength(PAUSED_DECISIONS_MAXIMUM);
	expect(received.at(0)?.id).toBe("dec_4");
	expect(received.at(-1)?.id).toBe(`dec_${sentCount}`);
	expect(result.current.newCount).toBe(0);
});

test("decisions the stream does not keep are neither delivered nor counted", () => {
	const { result, received } = renderEventStream(
		(decision) => decision.id !== "dec_hidden",
	);
	act(() => latestEventSource().open());

	act(() =>
		latestEventSource().sendDecision(
			"1726488000000-0",
			decisionEvent("dec_hidden"),
		),
	);
	expect(received).toEqual([]);

	act(() => result.current.pause());
	act(() => {
		latestEventSource().sendDecision(
			"1726488000001-0",
			decisionEvent("dec_hidden"),
		);
		latestEventSource().sendDecision(
			"1726488000002-0",
			decisionEvent("dec_shown"),
		);
	});
	expect(result.current.newCount).toBe(1);

	act(() => result.current.resume());
	expect(received.map((decision) => decision.id)).toEqual(["dec_shown"]);
});

test("a failed stream checks the session and reconnects while it lasts", async () => {
	renderEventStream();
	act(() => latestEventSource().open());

	act(() => latestEventSource().fail());
	await vi.waitFor(() => {
		expect(sessionChecks).toHaveLength(1);
	});
	act(() => vi.advanceTimersByTime(1_000));

	expect(FakeEventSource.opened).toHaveLength(2);
});

test("a failed stream with an ended session sends the member to login and stops reconnecting", async () => {
	sessionChecks = answerSessionChecks(401, endedSession);
	const { result, redirectToLogin } = renderEventStream();
	act(() => latestEventSource().open());

	act(() => latestEventSource().fail());
	await vi.waitFor(() => {
		expect(redirectToLogin).toHaveBeenCalledTimes(1);
	});
	act(() => vi.advanceTimersByTime(60_000));

	expect(FakeEventSource.opened).toHaveLength(1);
	expect(sessionChecks).toHaveLength(1);
	expect(result.current.status).toBe("reconnecting");
});
