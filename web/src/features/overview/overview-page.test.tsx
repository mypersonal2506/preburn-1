import { focusManager } from "@tanstack/react-query";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { OnboardingResponse, OverviewResponse } from "@/client";
import { sentRoutes } from "@/features/auth/auth-test-support";
import {
	emptyOverviewFixture,
	lumberLossCustomer,
	onboardingWithFirstCheck,
	onboardingWithoutFirstCheck,
	overviewFixture,
	overviewWithNullRows,
	studioPlanMargin,
} from "@/features/overview/overview-test-support";
import {
	type ApiAnswers,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

const ONBOARDING_ROUTE = "GET /api/v1/dashboard/onboarding";
const OVERVIEW_ROUTE = "GET /api/v1/dashboard/overview";

afterEach(() => {
	focusManager.setFocused(undefined);
	vi.unstubAllGlobals();
});

function overviewAnswers(overview: OverviewResponse): ApiAnswers {
	return {
		...signedInAnswers,
		[ONBOARDING_ROUTE]: jsonAnswer(onboardingWithFirstCheck),
		[OVERVIEW_ROUTE]: jsonAnswer(overview),
	};
}

async function findMain(): Promise<HTMLElement> {
	const main = await screen.findByRole("main");
	await within(main).findByRole("heading", { name: "Overview" });
	return main;
}

function findSection(main: HTMLElement, title: string): HTMLElement {
	const heading = within(main).getByRole("heading", { name: title });
	const section = heading.closest<HTMLElement>('[data-slot="card"]');
	if (section === null) {
		throw new Error(`section card missing title=${title}`);
	}
	return section;
}

function requestedPeriods(requests: readonly Request[]): (string | null)[] {
	return requests
		.map((request) => new URL(request.url))
		.filter((url) => url.pathname === "/api/v1/dashboard/overview")
		.map((url) => url.searchParams.get("period"));
}

test("renders every section from the overview", async () => {
	stubApi(overviewAnswers(overviewWithNullRows));
	renderApp("/");
	const main = await findMain();

	expect(await within(main).findByText("$1,240.50")).toBeInTheDocument();
	expect(
		within(main).getByText("Recognized through Sep 26, 2026"),
	).toBeInTheDocument();
	expect(within(main).getByText("$480.25")).toBeInTheDocument();
	expect(within(main).getByText("38.7% of revenue")).toBeInTheDocument();
	expect(within(main).getAllByText("61.3%")).not.toHaveLength(0);
	expect(within(main).getByText("21.3 pts above target")).toBeInTheDocument();
	expect(within(main).getByText("$95.40")).toBeInTheDocument();
	expect(within(main).getByText("By route, cap and deny")).toBeInTheDocument();
	expect(
		within(main).getByRole("button", { name: "About Cost avoided" }),
	).toBeInTheDocument();

	expect(within(main).getByText("1 plan below target")).toBeInTheDocument();
	expect(
		within(main).getByRole("link", { name: "View plans" }),
	).toHaveAttribute("href", "/plans");

	const chart = findSection(main, "Revenue and AI cost");
	expect(within(chart).getByText("Per UTC day")).toBeInTheDocument();
	await waitFor(() => {
		expect(chart.querySelector('[data-slot="chart"]')).not.toBeNull();
	});

	const planMargins = within(main).getByRole("table", { name: "Plan margins" });
	expect(within(planMargins).getByText("Creator")).toBeInTheDocument();
	expect(within(planMargins).getByText("Studio")).toBeInTheDocument();
	expect(within(planMargins).getByText("No plan")).toBeInTheDocument();
	expect(within(planMargins).getByText("$1,000.00")).toBeInTheDocument();

	const customersToWatch = within(main).getByRole("table", {
		name: "Customers to watch",
	});
	expect(
		within(customersToWatch).getByRole("link", { name: "Lumber Co" }),
	).toHaveAttribute("href", `/customers/${lumberLossCustomer.id}`);
	expect(within(customersToWatch).getByText("cedar")).toBeInTheDocument();
	expect(within(customersToWatch).getByText("-80.0%")).toBeInTheDocument();
	expect(within(customersToWatch).getByText("Denied")).toBeInTheDocument();
	expect(within(customersToWatch).getByText("None")).toBeInTheDocument();

	const decisionMix = findSection(main, "Decision mix");
	expect(within(decisionMix).getByText("1,014 decisions")).toBeInTheDocument();
	expect(within(decisionMix).getByText("Allowed")).toBeInTheDocument();
	expect(within(decisionMix).getByText("80.1%")).toBeInTheDocument();
	expect(within(decisionMix).getByText("Denied")).toBeInTheDocument();
	expect(within(decisionMix).getByText("3.2%")).toBeInTheDocument();

	const policyChanges = within(main).getByRole("table", {
		name: "Recent policy changes",
	});
	expect(
		within(policyChanges).getByRole("link", { name: "Heavy video users" }),
	).toHaveAttribute("href", "/policies/pol_01jbvagescfn78y0938nkrkayf");
	expect(
		within(policyChanges).getByText("Updated to version 3"),
	).toBeInTheDocument();
	expect(within(policyChanges).getByText("Created")).toBeInTheDocument();
});

test("plan margins judge a fixed allowance plan by sign, not its stored target", async () => {
	stubApi(
		overviewAnswers({
			...overviewFixture,
			plan_margins: [
				{
					...studioPlanMargin,
					plan_id: "pln_01jbvagescfn78y0938nkrkayk",
					name: "Free",
					mode: "fixed_allowance",
					below_target: false,
				},
				studioPlanMargin,
			],
		}),
	);
	renderApp("/");
	const main = await findMain();

	const planMargins = await within(main).findByRole("table", {
		name: "Plan margins",
	});
	const [freeRow, studioRow] = within(planMargins).getAllByRole("row").slice(1);
	if (freeRow === undefined || studioRow === undefined) {
		throw new Error("plan margin rows missing");
	}
	expect(within(freeRow).getByText("29.3%")).toHaveAttribute(
		"data-tone",
		"at_target",
	);
	expect(within(studioRow).getByText("29.3%")).toHaveAttribute(
		"data-tone",
		"below_target",
	);
});

test("the attention banner shows only the strongest message", async () => {
	stubApi(
		overviewAnswers({
			...overviewFixture,
			attention: {
				plans_below_target: 0,
				customers_above_pace: 3,
				dropped_reports: 12,
				uncosted_requests: 5,
			},
		}),
	);
	renderApp("/");
	const main = await findMain();

	expect(
		await within(main).findByText("3 customers above 2.0x pace"),
	).toBeInTheDocument();
	expect(
		within(main).getByRole("link", { name: "View customers" }),
	).toHaveAttribute("href", "/customers?sort=pace&direction=descending");
	expect(
		within(main).queryByText("12 reports dropped by the SDK"),
	).not.toBeInTheDocument();
	expect(
		within(main).queryByText("5 requests without a price"),
	).not.toBeInTheDocument();
});

test("an overview with nothing in the period shows no banner and empty sections", async () => {
	stubApi(overviewAnswers(emptyOverviewFixture));
	renderApp("/");
	const main = await findMain();

	expect(await within(main).findByText("Decision mix")).toBeInTheDocument();
	expect(within(main).queryByRole("alert")).not.toBeInTheDocument();
	expect(within(main).getAllByText("No revenue")).not.toHaveLength(0);
	expect(within(main).getByText("No usage in this period")).toBeInTheDocument();
	expect(
		within(main).getByText("No customers losing money"),
	).toBeInTheDocument();
	expect(
		within(main).getByText("No decisions in this period"),
	).toBeInTheDocument();
	expect(within(main).getByText("No policies yet")).toBeInTheDocument();
});

test("a period change refetches with the new period", async () => {
	const user = userEvent.setup();
	const requests = stubApi(overviewAnswers(overviewFixture));
	const { router } = renderApp("/");
	const main = await findMain();
	await within(main).findByText("$1,240.50");
	expect(
		within(main).getByRole("button", { name: "Current period" }),
	).toHaveAttribute("aria-pressed", "true");

	await user.click(
		within(main).getByRole("button", { name: "Previous period" }),
	);

	await waitFor(() => {
		expect(requestedPeriods(requests)).toEqual(["current", "previous"]);
	});
	expect(router.state.location.search).toEqual({ period: "previous" });
	expect(
		within(main).getByRole("button", { name: "Previous period" }),
	).toHaveAttribute("aria-pressed", "true");

	await user.click(within(main).getByRole("button", { name: "Last 30 days" }));

	await waitFor(() => {
		expect(requestedPeriods(requests)).toEqual([
			"current",
			"previous",
			"last_30_days",
		]);
	});
});

test("the period comes from the URL", async () => {
	const requests = stubApi(overviewAnswers(overviewFixture));
	renderApp("/?period=last_30_days");
	const main = await findMain();

	await within(main).findByText("$1,240.50");

	expect(requestedPeriods(requests)).toEqual(["last_30_days"]);
	expect(
		within(main).getByRole("button", { name: "Last 30 days" }),
	).toHaveAttribute("aria-pressed", "true");
});

test("the first-run empty state shows until a first check exists", async () => {
	let onboarding: OnboardingResponse = onboardingWithoutFirstCheck;
	const requests = stubApi({
		...overviewAnswers(overviewFixture),
		[ONBOARDING_ROUTE]: () => jsonAnswer(onboarding)(),
	});
	renderApp("/");
	const main = await findMain();

	expect(await within(main).findByText("No checks yet")).toBeInTheDocument();
	expect(
		within(main).getByRole("link", { name: "Get started" }),
	).toHaveAttribute("href", "/developers/get-started");
	expect(within(main).queryByText("Revenue")).not.toBeInTheDocument();
	expect(
		within(main).queryByRole("button", { name: "Current period" }),
	).not.toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain(OVERVIEW_ROUTE);

	onboarding = onboardingWithFirstCheck;
	act(() => {
		focusManager.setFocused(true);
	});

	expect(await within(main).findByText("$1,240.50")).toBeInTheDocument();
	expect(within(main).queryByText("No checks yet")).not.toBeInTheDocument();
});

test("a slow overview shows the loading skeleton", async () => {
	const answers = overviewAnswers(overviewFixture);
	vi.stubGlobal(
		"fetch",
		vi.fn(async (request: Request) => {
			const route = `${request.method} ${new URL(request.url).pathname}`;
			if (route === OVERVIEW_ROUTE) {
				return new Promise<Response>(() => {});
			}
			const answer = answers[route];
			if (answer === undefined) {
				throw new Error(`unexpected request route=${route}`);
			}
			return answer();
		}),
	);
	renderApp("/");
	const main = await findMain();

	await waitFor(() => {
		expect(main.querySelector('[aria-busy="true"]')).not.toBeNull();
	});
	expect(within(main).queryByText("Revenue")).not.toBeInTheDocument();
});

test("a failed overview shows Try again, which loads it again", async () => {
	const user = userEvent.setup();
	let overviewFails = true;
	stubApi({
		...overviewAnswers(overviewFixture),
		[OVERVIEW_ROUTE]: () =>
			overviewFails
				? problemAnswer(404, "not_found")()
				: jsonAnswer(overviewFixture)(),
	});
	renderApp("/");
	const main = await findMain();

	expect(
		await within(main).findByText("Something went wrong"),
	).toBeInTheDocument();

	overviewFails = false;
	await user.click(within(main).getByRole("button", { name: "Try again" }));

	expect(await within(main).findByText("$1,240.50")).toBeInTheDocument();
	expect(
		within(main).queryByText("Something went wrong"),
	).not.toBeInTheDocument();
});

test("an environment too large for the overview says so", async () => {
	stubApi({
		...overviewAnswers(overviewFixture),
		[OVERVIEW_ROUTE]: problemAnswer(422, "environment_too_large"),
	});
	renderApp("/");
	const main = await findMain();

	expect(
		await within(main).findByText("Too many customers to summarize"),
	).toBeInTheDocument();
	expect(
		within(main).queryByRole("button", { name: "Try again" }),
	).not.toBeInTheDocument();
});
