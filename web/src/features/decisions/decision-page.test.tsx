import { screen, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import type { DecisionDetailResponse } from "@/client";
import {
	decisionScreenAnswers,
	heavyVideoPolicy,
	routedDecisionDetail,
} from "@/features/decisions/decisions-test-support";
import { formatDateTime } from "@/lib/format";
import {
	jsonAnswer,
	problemAnswer,
	renderApp,
	stubApi,
} from "@/routes/-render-app";

const DECISION_ROUTE = `GET /api/v1/dashboard/decisions/${routedDecisionDetail.id}`;

const deniedDecisionDetail: DecisionDetailResponse = {
	...routedDecisionDetail,
	outcome: "deny",
	reason: "hard_limit_reached",
	provider: routedDecisionDetail.requested_provider,
	model: routedDecisionDetail.requested_model,
	overrides: {},
	matched_policy_id: null,
	matched_policy_version: null,
	reserved_amount: "0.000000000",
	estimate_basis: "none",
	settled_at: null,
	expires_at: routedDecisionDetail.created_at,
	status: "unreserved",
	ledger_entries: [],
};

afterEach(() => {
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
});

async function findSection(title: string): Promise<HTMLElement> {
	const heading = await screen.findByRole("heading", { name: title });
	const section = heading.closest<HTMLElement>('[data-slot="card"]');
	if (section === null) {
		throw new Error(`section card missing title=${title}`);
	}
	return section;
}

test("shows the summary, lifecycle, request, cost, signals and ledger entries", async () => {
	stubApi({
		...decisionScreenAnswers,
		[DECISION_ROUTE]: jsonAnswer(routedDecisionDetail),
		[`GET /api/v1/policies/${heavyVideoPolicy.id}`]:
			jsonAnswer(heavyVideoPolicy),
	});
	renderApp(`/decisions/${routedDecisionDetail.id}`);

	expect(
		await screen.findByRole("heading", {
			name: "Routed: text to video",
			level: 1,
		}),
	).toBeInTheDocument();
	expect(
		await screen.findByText(
			"Heavy video users routed Veo 3.1 Fast to Kling 2.5 Turbo Pro, at 5s.",
		),
	).toBeInTheDocument();
	expect(screen.getByRole("link", { name: "Acme Studio" })).toHaveAttribute(
		"href",
		`/customers/${routedDecisionDetail.customer_id}`,
	);

	const lifecycle = await findSection("Lifecycle");
	expect(lifecycle).toHaveTextContent(
		`Checked${formatDateTime(routedDecisionDetail.created_at)}`,
	);
	expect(lifecycle).toHaveTextContent("Reserved$0.56");
	expect(lifecycle).toHaveTextContent(
		`Settled${formatDateTime("2026-09-26T10:16:10Z")}`,
	);
	expect(lifecycle).not.toHaveTextContent("Expires");

	const request = await findSection("Request");
	expect(request).toHaveTextContent("Customer useruser-9");
	expect(request).toHaveTextContent("Attributes8s, with audio");
	expect(request).toHaveTextContent("Overrides5s");
	expect(
		within(request).getByRole("link", { name: /Heavy video users/ }),
	).toHaveAttribute("href", `/policies/${heavyVideoPolicy.id}`);
	expect(request).toHaveTextContent("Heavy video users, version 3");

	const cost = await findSection("Cost");
	expect(cost).toHaveTextContent("Requested cost$1.20");
	expect(cost).toHaveTextContent("Estimated cost$0.56");
	expect(cost).toHaveTextContent("Request estimate");

	const signals = await findSection("Signals");
	expect(signals).toHaveTextContent("Sep 1 to Sep 30, 2026 UTC");
	expect(signals).toHaveTextContent("Pace2.4x");
	expect(signals).toHaveTextContent("Projected margin20.0%");
	expect(signals).toHaveTextContent("Allowance left-$2.56");

	const ledger = await findSection("Ledger entries");
	const [, entryRow] = within(ledger).getAllByRole("row");
	expect(entryRow).toHaveTextContent("5s of output");
	expect(entryRow).toHaveTextContent("$0.35");
	expect(entryRow).toHaveTextContent("Report");
});

test("summarizes a decision without a matched policy and shows nothing reserved", async () => {
	const requests = stubApi({
		...decisionScreenAnswers,
		[DECISION_ROUTE]: jsonAnswer(deniedDecisionDetail),
	});
	renderApp(`/decisions/${routedDecisionDetail.id}`);

	expect(
		await screen.findByText(
			"Denied Veo 3.1 Fast because the hard limit was reached.",
		),
	).toBeInTheDocument();
	const lifecycle = await findSection("Lifecycle");
	expect(lifecycle).toHaveTextContent("ReservedNothing");
	expect(lifecycle).toHaveTextContent("Unreserved");
	expect(await findSection("Ledger entries")).toHaveTextContent(
		"No usage reported",
	);
	expect(
		requests.some((request) => request.url.includes("/api/v1/policies/")),
	).toBe(false);
});

test.each<{
	status: DecisionDetailResponse["status"];
	row: string;
	hidden: string[];
}>([
	{
		status: "reserved",
		row: `Expires${formatDateTime(routedDecisionDetail.expires_at)}`,
		hidden: ["Settled", "Released", "Expired"],
	},
	{
		status: "released",
		row: `ReleasedBefore ${formatDateTime(routedDecisionDetail.expires_at)}`,
		hidden: ["Settled", "Expired", "Expires"],
	},
	{
		status: "expired",
		row: `Expired${formatDateTime(routedDecisionDetail.expires_at)}`,
		hidden: ["Settled", "Released", "Expires"],
	},
])(
	"the lifecycle of a $status decision ends with its row",
	async ({ status, row, hidden }) => {
		stubApi({
			...decisionScreenAnswers,
			[DECISION_ROUTE]: jsonAnswer({
				...routedDecisionDetail,
				status,
				settled_at: null,
				ledger_entries: [],
			}),
			[`GET /api/v1/policies/${heavyVideoPolicy.id}`]:
				jsonAnswer(heavyVideoPolicy),
		});
		renderApp(`/decisions/${routedDecisionDetail.id}`);

		const lifecycle = await findSection("Lifecycle");
		expect(lifecycle).toHaveTextContent(row);
		for (const label of hidden) {
			expect(within(lifecycle).queryByText(label)).not.toBeInTheDocument();
		}
	},
);

test("a missing decision shows not found with a link to the list", async () => {
	stubApi({
		...decisionScreenAnswers,
		[DECISION_ROUTE]: problemAnswer(404, "not_found"),
	});
	renderApp(`/decisions/${routedDecisionDetail.id}`);

	expect(await screen.findByText("Decision not found")).toBeInTheDocument();
	expect(screen.getByRole("link", { name: "View decisions" })).toHaveAttribute(
		"href",
		"/decisions",
	);
});

test("a failing decision shows the error state", async () => {
	vi.spyOn(console, "error").mockImplementation(() => undefined);
	stubApi({
		...decisionScreenAnswers,
		[DECISION_ROUTE]: problemAnswer(422, "validation_failed"),
	});
	renderApp(`/decisions/${routedDecisionDetail.id}`);

	expect(
		await screen.findByRole("heading", { name: "Something went wrong" }),
	).toBeInTheDocument();
});
