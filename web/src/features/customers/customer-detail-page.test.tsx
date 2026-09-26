import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import type { CustomerDetailResponse, PlanResponse } from "@/client";
import {
	acmeDetail,
	creatorPlan,
	distantLastDay,
	distantPeriodEnd,
	videoModelPage,
} from "@/features/customers/customers-test-support";
import {
	type ApiAnswers,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

const freeDetail: CustomerDetailResponse = {
	...acmeDetail,
	plan_name: "Free",
	target_margin: "0.0000",
	signals: {
		...acmeDetail.signals,
		allowance_remaining: "-1.200000000",
		cost_allowance: "0.000000000",
		cost_to_date: "1.200000000",
		pace: "inf",
		period_revenue_net: "0.000000000",
		projected_margin: "-inf",
	},
};

const previousPeriod = {
	...acmeDetail.history[0],
	period_start: "2026-08-01T00:00:00Z",
	period_end: "2026-09-01T00:00:00Z",
	revenue: "90.000000000",
	cost: "40.000000000",
	margin: "0.5556",
	uncosted_count: 0,
	decision_counts: { allow: 8, route: 0, cap: 0, deny: 1 },
};

afterEach(() => {
	vi.unstubAllGlobals();
});

function detailAnswers(
	detailAnswer: () => Response,
	plan: PlanResponse = creatorPlan,
): ApiAnswers {
	return {
		...signedInAnswers,
		[`GET /api/v1/dashboard/customers/${acmeDetail.id}`]: detailAnswer,
		[`GET /api/v1/plans/${plan.id}`]: jsonAnswer(plan),
		"GET /api/v1/pricing/models": jsonAnswer(videoModelPage),
	};
}

async function findTile(label: string): Promise<HTMLElement> {
	const labelText = await screen.findByText(label, {
		selector: '[data-slot="card"] div',
	});
	const tile = labelText.closest<HTMLElement>('[data-slot="card"]');
	if (tile === null) {
		throw new Error(`metric tile missing label=${label}`);
	}
	return tile;
}

async function findSection(title: string): Promise<HTMLElement> {
	const heading = await screen.findByRole("heading", { name: title });
	const section = heading.closest<HTMLElement>('[data-slot="card"]');
	if (section === null) {
		throw new Error(`section card missing title=${title}`);
	}
	return section;
}

function signalValue(section: HTMLElement, label: string): string | null {
	const term = within(section).getByText(label, { selector: "dt" });
	return term.nextElementSibling?.textContent ?? null;
}

test("the header names the customer, its plan, period end and decisions", async () => {
	stubApi(detailAnswers(jsonAnswer(acmeDetail)));
	renderApp(`/customers/${acmeDetail.id}`);

	expect(
		await screen.findByRole("heading", { level: 1, name: "Acme Studio" }),
	).toBeInTheDocument();
	expect(screen.getByText("Creator, ends in 3 days")).toBeInTheDocument();
	expect(
		screen.getByRole("button", { name: "Copy id" }).parentElement,
	).toHaveTextContent(acmeDetail.external_id);
	expect(screen.getByRole("link", { name: "View decisions" })).toHaveAttribute(
		"href",
		`/decisions?customer_id=${acmeDetail.id}`,
	);
});

test("the tiles show revenue, cost against the allowance, margin and pace", async () => {
	stubApi(detailAnswers(jsonAnswer(acmeDetail)));
	renderApp(`/customers/${acmeDetail.id}`);

	const revenueTile = await findTile("Revenue");
	expect(within(revenueTile).getByText("$120.00")).toBeInTheDocument();
	const costTile = await findTile("AI cost");
	expect(within(costTile).getByText("$84.00")).toBeInTheDocument();
	expect(within(costTile).getByText("$72.00 allowance")).toBeInTheDocument();
	expect(
		within(costTile).getByRole("progressbar", { name: "Allowance used" }),
	).toHaveAttribute("aria-valuenow", "100");
	const marginTile = await findTile("Projected margin");
	expect(within(marginTile).getByText("22.2%")).toBeInTheDocument();
	expect(
		await within(marginTile).findByText("17.8 pts below target"),
	).toBeInTheDocument();
	const paceTile = await findTile("Pace");
	expect(within(paceTile).getByText("1.3x")).toBeInTheDocument();
	expect(
		within(paceTile).getByText("90.0% of period elapsed"),
	).toBeInTheDocument();
});

test("a fixed allowance plan shows the projected margin without a target gap", async () => {
	const fixedAllowancePlan: PlanResponse = {
		...creatorPlan,
		mode: "fixed_allowance",
		allowance: "72.000000000",
	};
	stubApi(detailAnswers(jsonAnswer(acmeDetail), fixedAllowancePlan));
	const { queryClient } = renderApp(`/customers/${acmeDetail.id}`);

	const marginTile = await findTile("Projected margin");
	await waitFor(() => {
		expect(queryClient.isFetching()).toBe(0);
	});
	expect(within(marginTile).getByText("22.2%")).toBeInTheDocument();
	expect(within(marginTile).queryByText(/target/)).not.toBeInTheDocument();
});

test("a period end beyond 7 days shows the last day the period includes", async () => {
	stubApi(
		detailAnswers(jsonAnswer({ ...acmeDetail, period_end: distantPeriodEnd })),
	);
	renderApp(`/customers/${acmeDetail.id}`);

	expect(
		await screen.findByText(`Creator, ends ${distantLastDay}`),
	).toBeInTheDocument();
});

test("signals read inf pace as No allowance and -inf margin as No revenue", async () => {
	stubApi(detailAnswers(jsonAnswer(freeDetail)));
	renderApp(`/customers/${acmeDetail.id}`);

	const paceTile = await findTile("Pace");
	expect(within(paceTile).getByText("No allowance")).toBeInTheDocument();
	const costTile = await findTile("AI cost");
	expect(within(costTile).getByText("No allowance")).toBeInTheDocument();
	expect(within(costTile).queryByRole("progressbar")).not.toBeInTheDocument();
	const marginTile = await findTile("Projected margin");
	expect(within(marginTile).getByText("No revenue")).toBeInTheDocument();
	expect(within(marginTile).queryByText(/target/)).not.toBeInTheDocument();
	const signals = await findSection("Signals");
	expect(signalValue(signals, "Pace")).toBe("No allowance");
	expect(signalValue(signals, "Projected margin")).toBe("No revenue");
	expect(signalValue(signals, "Allowance left")).toBe("-$1.20");
	expect(signalValue(signals, "Spend this period")).toBe("$1.20");
	expect(signalValue(signals, "Revenue this period")).toBe("$0.00");
	expect(signalValue(signals, "Period elapsed")).toBe("90.0%");
	expect(signalValue(signals, "Requests this period")).toBe("14");
	expect(
		within(signals).getByRole("button", { name: "About Reserved" }),
	).toBeInTheDocument();
});

test("the period history shows only once there is more than one period", async () => {
	stubApi(detailAnswers(jsonAnswer(acmeDetail)));
	renderApp(`/customers/${acmeDetail.id}`);
	await findSection("Signals");

	expect(
		screen.queryByRole("heading", { name: "Period history" }),
	).not.toBeInTheDocument();
});

test("two periods show the period history", async () => {
	stubApi(
		detailAnswers(
			jsonAnswer({
				...acmeDetail,
				history: [...acmeDetail.history, previousPeriod],
			}),
		),
	);
	renderApp(`/customers/${acmeDetail.id}`);

	const history = await findSection("Period history");
	await waitFor(() => {
		expect(history.querySelector('[data-slot="chart"]')).not.toBeNull();
	});
});

test("usage and recent decisions list the period's requests", async () => {
	stubApi(detailAnswers(jsonAnswer(acmeDetail)));
	renderApp(`/customers/${acmeDetail.id}`);

	const usage = await screen.findByRole("table", {
		name: "Usage by feature and model",
	});
	const [usageRow] = within(usage).getAllByRole("row").slice(1);
	if (usageRow === undefined) {
		throw new Error("usage row missing");
	}
	expect(within(usageRow).getByText("text to video")).toBeInTheDocument();
	expect(
		await within(usageRow).findByText("Kling 2.5 Turbo Pro"),
	).toBeInTheDocument();
	expect(within(usageRow).getByText("12")).toBeInTheDocument();
	expect(within(usageRow).getByText("1")).toBeInTheDocument();
	expect(within(usageRow).getByText("$84.00")).toBeInTheDocument();

	const decisions = screen.getByRole("table", { name: "Recent decisions" });
	const [decisionRow] = within(decisions).getAllByRole("row").slice(1);
	if (decisionRow === undefined) {
		throw new Error("decision row missing");
	}
	expect(within(decisionRow).getByText("1 hour ago")).toBeInTheDocument();
	expect(within(decisionRow).getByText("Routed")).toBeInTheDocument();
	expect(decisionRow).toHaveTextContent("Kling 2.5 Turbo Profrom Veo 3.1 Fast");
	expect(within(decisionRow).getByText("$0.35")).toBeInTheDocument();
	expect(within(decisionRow).getByText("Settled")).toBeInTheDocument();
	expect(
		within(decisionRow).getByRole("link", { name: "1 hour ago" }),
	).toHaveAttribute("href", `/decisions/${acmeDetail.recent_decisions[0]?.id}`);
});

test("an unknown customer shows not found with a way back", async () => {
	stubApi(detailAnswers(problemAnswer(404, "not_found")));
	renderApp(`/customers/${acmeDetail.id}`);

	expect(await screen.findByText("Customer not found")).toBeInTheDocument();
	expect(screen.getByRole("link", { name: "View customers" })).toHaveAttribute(
		"href",
		"/customers",
	);
});
