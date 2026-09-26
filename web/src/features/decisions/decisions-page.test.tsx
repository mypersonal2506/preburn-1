import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { DecisionResponse } from "@/client";
import {
	acmeCustomer,
	decisionScreenAnswers,
	decisionsPage,
	deniedDecision,
	heavyVideoPolicy,
	routedDecision,
	streamedDecision,
} from "@/features/decisions/decisions-test-support";
import { setEnvironment } from "@/lib/environment-store";
import {
	FakeEventSource,
	latestEventSource,
} from "@/lib/event-source-test-support";
import {
	answerByEnvironment,
	jsonAnswer,
	problemAnswer,
	renderApp,
	stubApi,
} from "@/routes/-render-app";

const DECISIONS_ROUTE = "GET /api/v1/dashboard/decisions";
const LIVE_REFRESH_WAIT_MILLISECONDS = 1_000;

const newerRoutedDecision: DecisionResponse = {
	...routedDecision,
	id: "dec_01jbvagescfn78y0938nkrka03",
	created_at: "2026-09-26T10:16:00Z",
	customer_display_name: "Lumber Co",
	status: "reserved",
};

const newestRoutedDecision: DecisionResponse = {
	...routedDecision,
	id: "dec_01jbvagescfn78y0938nkrka04",
	created_at: "2026-09-26T10:17:00Z",
	customer_display_name: "Hilltop Creative",
	status: "reserved",
};

beforeEach(() => {
	FakeEventSource.opened = [];
	vi.stubGlobal("EventSource", FakeEventSource);
});

afterEach(() => {
	setEnvironment("test");
	window.localStorage.clear();
	vi.useRealTimers();
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
});

function decisionListRequests(requests: readonly Request[]): URLSearchParams[] {
	return requests
		.filter(
			(request) =>
				new URL(request.url).pathname === "/api/v1/dashboard/decisions",
		)
		.map((request) => new URL(request.url).searchParams);
}

function answerDecisionPages(...pages: DecisionResponse[][]): () => Response {
	let answered = 0;
	return () => {
		const page = pages[Math.min(answered, pages.length - 1)];
		answered += 1;
		if (page === undefined) {
			throw new Error("decision pages missing");
		}
		return jsonAnswer(decisionsPage(page))();
	};
}

async function findDecisionRows(): Promise<HTMLElement[]> {
	const table = await screen.findByRole("table", { name: "Decisions" });
	await waitFor(() => expect(table).toHaveAttribute("aria-busy", "false"));
	return within(table).getAllByRole("row").slice(1);
}

function useLiveTimers(): ReturnType<typeof userEvent.setup> {
	vi.useFakeTimers({ shouldAdvanceTime: true });
	return userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
}

async function passLiveRefreshWait(): Promise<void> {
	await act(async () => {
		await vi.advanceTimersByTimeAsync(LIVE_REFRESH_WAIT_MILLISECONDS);
	});
}

test("lists decisions with models by display name, outcome, cost and status", async () => {
	stubApi({
		...decisionScreenAnswers,
		[DECISIONS_ROUTE]: jsonAnswer(
			decisionsPage([routedDecision, deniedDecision]),
		),
	});
	renderApp("/decisions");

	const [routedRow, deniedRow] = await findDecisionRows();
	if (routedRow === undefined || deniedRow === undefined) {
		throw new Error("decision rows missing");
	}
	expect(
		await within(routedRow).findByText("Kling 2.5 Turbo Pro"),
	).toBeInTheDocument();
	expect(routedRow).toHaveTextContent("Acme Studio");
	expect(routedRow).toHaveTextContent("text to video");
	expect(routedRow).toHaveTextContent("from Veo 3.1 Fast");
	expect(routedRow).toHaveTextContent("Routed");
	expect(routedRow).toHaveTextContent("$0.56");
	expect(routedRow).toHaveTextContent("Settled");
	expect(within(routedRow).getByRole("link")).toHaveAttribute(
		"href",
		`/decisions/${routedDecision.id}`,
	);

	expect(deniedRow).toHaveTextContent("Veo 3.1 Fast");
	expect(deniedRow).not.toHaveTextContent("from");
	expect(deniedRow).toHaveTextContent("Denied");
	expect(deniedRow).toHaveTextContent("$0.00016");
	expect(deniedRow).toHaveTextContent("Unreserved");
});

test("outcome and feature filters change the URL and the list request", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...decisionScreenAnswers,
		[DECISIONS_ROUTE]: jsonAnswer(decisionsPage([routedDecision])),
	});
	const { router } = renderApp("/decisions");
	await findDecisionRows();

	await user.click(screen.getByRole("button", { name: "Denied" }));
	expect(router.state.location.search).toEqual({ outcome: "deny" });

	await user.click(screen.getByRole("button", { name: "Feature" }));
	await user.click(
		await screen.findByRole("option", { name: "text to video" }),
	);

	expect(router.state.location.search).toEqual({
		outcome: "deny",
		feature: "text_to_video",
	});
	const lastQuery = decisionListRequests(requests).at(-1);
	expect(lastQuery?.get("outcome")).toBe("deny");
	expect(lastQuery?.get("feature")).toBe("text_to_video");
	expect(
		screen.getByRole("button", { name: "Feature: text to video" }),
	).toBeInTheDocument();

	await user.click(screen.getByRole("button", { name: "All" }));
	expect(router.state.location.search).toEqual({ feature: "text_to_video" });
});

test("customer and policy filters from the URL show their names and clear", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...decisionScreenAnswers,
		[DECISIONS_ROUTE]: jsonAnswer(decisionsPage([routedDecision])),
		[`GET /api/v1/dashboard/customers/${acmeCustomer.id}`]:
			jsonAnswer(acmeCustomer),
		[`GET /api/v1/policies/${heavyVideoPolicy.id}`]:
			jsonAnswer(heavyVideoPolicy),
	});
	const { router } = renderApp(
		`/decisions?customer_id=${acmeCustomer.id}&policy_id=${heavyVideoPolicy.id}`,
	);

	expect(
		await screen.findByRole("button", { name: "Customer: Acme Studio" }),
	).toBeInTheDocument();
	expect(
		await screen.findByRole("button", { name: "Policy: Heavy video users" }),
	).toBeInTheDocument();
	const firstQuery = decisionListRequests(requests).at(0);
	expect(firstQuery?.get("customer_id")).toBe(acmeCustomer.id);
	expect(firstQuery?.get("policy_id")).toBe(heavyVideoPolicy.id);

	await user.click(screen.getByRole("button", { name: "Clear Policy" }));
	await user.click(screen.getByRole("button", { name: "Clear Customer" }));

	expect(router.state.location.search).toEqual({});
	const lastQuery = decisionListRequests(requests).at(-1);
	expect(lastQuery?.has("customer_id")).toBe(false);
	expect(lastQuery?.has("policy_id")).toBe(false);
});

test("customer and policy filters of the other environment read Unknown and clear", async () => {
	const user = userEvent.setup();
	stubApi({
		...decisionScreenAnswers,
		[DECISIONS_ROUTE]: jsonAnswer(decisionsPage([])),
		[`GET /api/v1/dashboard/customers/${acmeCustomer.id}`]: answerByEnvironment(
			{
				test: jsonAnswer(acmeCustomer),
				live: problemAnswer(404, "not_found"),
			},
		),
		[`GET /api/v1/policies/${heavyVideoPolicy.id}`]: answerByEnvironment({
			test: jsonAnswer(heavyVideoPolicy),
			live: problemAnswer(404, "not_found"),
		}),
	});
	const { router } = renderApp(
		`/decisions?customer_id=${acmeCustomer.id}&policy_id=${heavyVideoPolicy.id}`,
	);
	await screen.findByRole("button", { name: "Customer: Acme Studio" });
	await screen.findByRole("button", { name: "Policy: Heavy video users" });

	await user.click(screen.getByRole("radio", { name: "Live" }));

	expect(
		await screen.findByRole("button", { name: "Customer: Unknown" }),
	).toBeInTheDocument();
	expect(
		await screen.findByRole("button", { name: "Policy: Unknown" }),
	).toBeInTheDocument();
	expect(
		screen.queryByRole("heading", { name: "Something went wrong" }),
	).not.toBeInTheDocument();

	await user.click(screen.getByRole("button", { name: "Clear Customer" }));
	await user.click(screen.getByRole("button", { name: "Clear Policy" }));

	expect(router.state.location.search).toEqual({});
	expect(await screen.findByText("No decisions yet")).toBeInTheDocument();
});

test("a streamed decision matching the filters prepends and others are ignored", async () => {
	useLiveTimers();
	const requests = stubApi({
		...decisionScreenAnswers,
		[DECISIONS_ROUTE]: answerDecisionPages(
			[routedDecision],
			[newerRoutedDecision, routedDecision],
		),
	});
	renderApp("/decisions?outcome=route");
	await findDecisionRows();
	act(() => latestEventSource().open());

	act(() =>
		latestEventSource().sendDecision(
			"1727345640000-0",
			streamedDecision({
				...deniedDecision,
				id: "dec_01jbvagescfn78y0938nkrka05",
			}),
		),
	);
	await passLiveRefreshWait();
	expect(decisionListRequests(requests)).toHaveLength(1);

	act(() =>
		latestEventSource().sendDecision(
			"1727345760000-0",
			streamedDecision(newerRoutedDecision),
		),
	);
	await passLiveRefreshWait();

	expect(await screen.findByText("Lumber Co")).toBeInTheDocument();
	const rows = await findDecisionRows();
	expect(rows.map((row) => row.textContent)).toEqual([
		expect.stringContaining("Lumber Co"),
		expect.stringContaining("Acme Studio"),
	]);
	expect(decisionListRequests(requests)).toHaveLength(2);
});

test("pause buffers streamed decisions and resume inserts them in order", async () => {
	const user = useLiveTimers();
	const requests = stubApi({
		...decisionScreenAnswers,
		[DECISIONS_ROUTE]: answerDecisionPages(
			[routedDecision],
			[newestRoutedDecision, newerRoutedDecision, routedDecision],
		),
	});
	renderApp("/decisions");
	await findDecisionRows();
	act(() => latestEventSource().open());

	await user.click(screen.getByRole("button", { name: "Pause" }));
	act(() => {
		latestEventSource().sendDecision(
			"1727345760000-0",
			streamedDecision(newerRoutedDecision),
		);
		latestEventSource().sendDecision(
			"1727345820000-0",
			streamedDecision(newestRoutedDecision),
		);
	});
	await passLiveRefreshWait();

	expect(screen.getByRole("status")).toHaveTextContent("Paused, 2 new");
	expect(decisionListRequests(requests)).toHaveLength(1);
	expect(await findDecisionRows()).toHaveLength(1);

	await user.click(screen.getByRole("button", { name: "Resume" }));
	await passLiveRefreshWait();

	expect(await screen.findByText("Hilltop Creative")).toBeInTheDocument();
	const rows = await findDecisionRows();
	expect(rows.map((row) => row.textContent)).toEqual([
		expect.stringContaining("Hilltop Creative"),
		expect.stringContaining("Lumber Co"),
		expect.stringContaining("Acme Studio"),
	]);
	expect(decisionListRequests(requests)).toHaveLength(2);
});

test("pause counts only the streamed decisions the filters show", async () => {
	const user = useLiveTimers();
	const newerDeniedDecision: DecisionResponse = {
		...deniedDecision,
		id: "dec_01jbvagescfn78y0938nkrka06",
		created_at: "2026-09-26T10:16:30Z",
		customer_display_name: "Tall Oak Studio",
	};
	const requests = stubApi({
		...decisionScreenAnswers,
		[DECISIONS_ROUTE]: answerDecisionPages(
			[deniedDecision],
			[newerDeniedDecision, deniedDecision],
		),
	});
	renderApp("/decisions?outcome=deny");
	await findDecisionRows();
	act(() => latestEventSource().open());

	await user.click(screen.getByRole("button", { name: "Pause" }));
	act(() => {
		latestEventSource().sendDecision(
			"1727345760000-0",
			streamedDecision(newerRoutedDecision),
		);
		latestEventSource().sendDecision(
			"1727345790000-0",
			streamedDecision(newerDeniedDecision),
		);
		latestEventSource().sendDecision(
			"1727345820000-0",
			streamedDecision(newestRoutedDecision),
		);
	});
	await passLiveRefreshWait();

	expect(screen.getByRole("status")).toHaveTextContent("Paused, 1 new");

	await user.click(screen.getByRole("button", { name: "Resume" }));
	await passLiveRefreshWait();

	expect(await screen.findByText("Tall Oak Studio")).toBeInTheDocument();
	expect(decisionListRequests(requests)).toHaveLength(2);
});

test("a later page never refreshes for streamed decisions", async () => {
	const user = useLiveTimers();
	const requests = stubApi({
		...decisionScreenAnswers,
		[DECISIONS_ROUTE]: () =>
			jsonAnswer({ items: [routedDecision], next_cursor: "page-2" })(),
	});
	renderApp("/decisions");
	await findDecisionRows();
	act(() => latestEventSource().open());

	await user.click(screen.getByRole("button", { name: "Next" }));
	act(() =>
		latestEventSource().sendDecision(
			"1727345760000-0",
			streamedDecision(newerRoutedDecision),
		),
	);
	await passLiveRefreshWait();

	const cursors = decisionListRequests(requests).map((query) =>
		query.get("cursor"),
	);
	expect(cursors).toEqual([null, "page-2"]);
});

test("shows an empty state without decisions and a clear action with filters", async () => {
	const user = userEvent.setup();
	stubApi({
		...decisionScreenAnswers,
		[DECISIONS_ROUTE]: jsonAnswer(decisionsPage([])),
	});
	const { router } = renderApp("/decisions?outcome=cap");

	expect(await screen.findByText("No matching decisions")).toBeInTheDocument();
	await user.click(screen.getByRole("button", { name: "Clear filters" }));

	expect(router.state.location.search).toEqual({});
	const table = await screen.findByRole("table", { name: "Decisions" });
	expect(
		await within(table).findByText("No decisions yet"),
	).toBeInTheDocument();
	expect(
		within(table).getByRole("link", { name: "Get started" }),
	).toHaveAttribute("href", "/developers/get-started");
});

test("shows the error state when the list fails", async () => {
	vi.spyOn(console, "error").mockImplementation(() => undefined);
	stubApi({
		...decisionScreenAnswers,
		[DECISIONS_ROUTE]: problemAnswer(422, "invalid_cursor"),
	});
	renderApp("/decisions");

	expect(
		await screen.findByRole("heading", { name: "Something went wrong" }),
	).toBeInTheDocument();
});
