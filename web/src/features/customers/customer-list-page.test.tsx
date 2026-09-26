import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import {
	acmeMargin,
	cedarMargin,
	creatorPlan,
	customerListQueries,
	customerPage,
	distantLastDay,
	distantPeriodEnd,
	planPage,
	tallOakMargin,
} from "@/features/customers/customers-test-support";
import {
	type ApiAnswers,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

const DEFAULT_QUERY = {
	sort: "margin",
	direction: "ascending",
	revenue_filter: "all",
};

afterEach(() => {
	vi.unstubAllGlobals();
});

function listAnswers(customerAnswer: () => Response): ApiAnswers {
	return {
		...signedInAnswers,
		"GET /api/v1/plans": jsonAnswer(planPage),
		"GET /api/v1/dashboard/customers": customerAnswer,
	};
}

async function findCustomerTable(): Promise<HTMLElement> {
	return screen.findByRole("table", { name: "Customers" });
}

async function findCustomerRows(): Promise<HTMLElement[]> {
	const table = await findCustomerTable();
	await within(table).findByText("Cedar Games");
	return within(table).getAllByRole("row").slice(1);
}

test("the list asks for margin ascending and shows each customer", async () => {
	const requests = stubApi(
		listAnswers(jsonAnswer(customerPage([acmeMargin, cedarMargin]))),
	);
	renderApp("/customers");

	const [acmeRow, cedarRow] = await findCustomerRows();

	expect(customerListQueries(requests)).toEqual([DEFAULT_QUERY]);
	const table = await findCustomerTable();
	expect(
		within(table)
			.getAllByRole("columnheader")
			.map((header) => header.textContent),
	).toEqual([
		"Customer",
		"Plan",
		"Revenue",
		"AI cost",
		"Margin",
		"Pace",
		"Period ends",
	]);
	if (acmeRow === undefined || cedarRow === undefined) {
		throw new Error("customer rows missing");
	}
	expect(within(acmeRow).getByText("Acme Studio")).toBeInTheDocument();
	expect(within(acmeRow).getByText("Creator")).toBeInTheDocument();
	expect(within(acmeRow).getByText("$120.00")).toBeInTheDocument();
	expect(within(acmeRow).getByText("$84.00")).toBeInTheDocument();
	expect(within(acmeRow).getByText("30.0%")).toBeInTheDocument();
	expect(within(acmeRow).getByText("1.3x")).toBeInTheDocument();
	expect(
		within(acmeRow).getByRole("link", { name: "Acme Studio" }),
	).toHaveAttribute("href", `/customers/${acmeMargin.id}`);
	expect(
		within(cedarRow).getByRole("progressbar", {
			name: "Margin against target",
		}),
	).toHaveAttribute("aria-valuenow", "100");
});

test("Period ends shows the last day the period includes", async () => {
	stubApi(
		listAnswers(
			jsonAnswer(
				customerPage([{ ...acmeMargin, period_end: distantPeriodEnd }]),
			),
		),
	);
	renderApp("/customers");

	const table = await findCustomerTable();
	const acmeRow = (await within(table).findByText("Acme Studio")).closest("tr");

	expect(acmeRow).toHaveTextContent(distantLastDay);
});

test("customers without revenue read No revenue for margin and come last", async () => {
	stubApi(
		listAnswers(
			jsonAnswer(customerPage([acmeMargin, cedarMargin, tallOakMargin])),
		),
	);
	renderApp("/customers");

	const rows = await findCustomerRows();

	const lastRow = rows.at(-1);
	if (lastRow === undefined) {
		throw new Error("customer rows missing");
	}
	expect(within(lastRow).getByText(tallOakMargin.external_id)).toBeVisible();
	expect(within(lastRow).getByText("No revenue")).toBeInTheDocument();
	expect(within(lastRow).getByText("No plan")).toBeInTheDocument();
	expect(within(lastRow).getByText("No allowance")).toBeInTheDocument();
	expect(within(lastRow).queryByRole("progressbar")).not.toBeInTheDocument();
});

test("sorting by a column changes the URL and the request", async () => {
	const user = userEvent.setup();
	const requests = stubApi(
		listAnswers(jsonAnswer(customerPage([acmeMargin, cedarMargin]))),
	);
	const { router } = renderApp("/customers");
	const table = await findCustomerTable();
	await within(table).findByText("Cedar Games");

	expect(
		within(table).getByRole("columnheader", { name: "Margin" }),
	).toHaveAttribute("aria-sort", "ascending");

	await user.click(within(table).getByRole("button", { name: "Revenue" }));

	await waitFor(() => {
		expect(customerListQueries(requests).at(-1)).toEqual({
			...DEFAULT_QUERY,
			sort: "revenue",
		});
	});
	expect(router.state.location.search).toEqual({ sort: "revenue" });

	await user.click(within(table).getByRole("button", { name: "Revenue" }));

	await waitFor(() => {
		expect(customerListQueries(requests).at(-1)).toEqual({
			...DEFAULT_QUERY,
			sort: "revenue",
			direction: "descending",
		});
	});
	expect(router.state.location.search).toEqual({
		sort: "revenue",
		direction: "descending",
	});
});

test("filters change the URL and the request", async () => {
	const user = userEvent.setup();
	const requests = stubApi(
		listAnswers(jsonAnswer(customerPage([acmeMargin, cedarMargin]))),
	);
	const { router } = renderApp("/customers");
	await within(await findCustomerTable()).findByText("Cedar Games");

	await user.click(screen.getByRole("button", { name: "Paying" }));

	await waitFor(() => {
		expect(customerListQueries(requests).at(-1)).toEqual({
			...DEFAULT_QUERY,
			revenue_filter: "paying",
		});
	});
	expect(screen.getByRole("button", { name: "Paying" })).toHaveAttribute(
		"aria-pressed",
		"true",
	);

	await user.type(screen.getByRole("searchbox", { name: "Search" }), "acme");

	await waitFor(() => {
		expect(customerListQueries(requests).at(-1)).toEqual({
			...DEFAULT_QUERY,
			revenue_filter: "paying",
			search: "acme",
		});
	});
	expect(
		customerListQueries(requests).filter((query) => "search" in query),
	).toHaveLength(1);

	await user.click(screen.getByRole("button", { name: "Plan" }));
	await user.click(await screen.findByRole("option", { name: /^Creator/ }));

	await waitFor(() => {
		expect(customerListQueries(requests).at(-1)).toEqual({
			...DEFAULT_QUERY,
			revenue_filter: "paying",
			search: "acme",
			plan_id: creatorPlan.id,
		});
	});
	expect(router.state.location.search).toEqual({
		revenue_filter: "paying",
		search: "acme",
		plan_id: creatorPlan.id,
	});

	await user.click(screen.getByRole("button", { name: "Clear Plan" }));

	await waitFor(() => {
		expect(router.state.location.search).toEqual({
			revenue_filter: "paying",
			search: "acme",
		});
	});
	expect(screen.getByRole("button", { name: "Plan" })).toBeInTheDocument();
	expect(
		screen.queryByRole("button", { name: "Clear Plan" }),
	).not.toBeInTheDocument();
});

test("Next sends the page cursor and a filter change starts over", async () => {
	const user = userEvent.setup();
	const requests = stubApi(
		listAnswers(
			jsonAnswer(customerPage([acmeMargin, cedarMargin], "cursor-2")),
		),
	);
	renderApp("/customers");
	await within(await findCustomerTable()).findByText("Cedar Games");

	await user.click(screen.getByRole("button", { name: "Next" }));

	await waitFor(() => {
		expect(customerListQueries(requests).at(-1)).toEqual({
			...DEFAULT_QUERY,
			cursor: "cursor-2",
		});
	});

	await user.click(screen.getByRole("button", { name: "Free" }));

	await waitFor(() => {
		expect(customerListQueries(requests).at(-1)).toEqual({
			...DEFAULT_QUERY,
			revenue_filter: "free",
		});
	});
});

test("an environment too large for the list says so", async () => {
	stubApi(listAnswers(problemAnswer(422, "environment_too_large")));
	renderApp("/customers");

	const table = await findCustomerTable();

	expect(
		await within(table).findByText("Too many customers to list"),
	).toBeInTheDocument();
	expect(screen.getByRole("heading", { name: "Customers" })).toBeVisible();
});

test("an empty environment offers the setup steps", async () => {
	stubApi(listAnswers(jsonAnswer(customerPage([]))));
	renderApp("/customers");

	const table = await findCustomerTable();

	expect(await within(table).findByText("No customers yet")).toBeVisible();
	expect(
		within(table).getByRole("link", { name: "Get started" }),
	).toHaveAttribute("href", "/developers/get-started");
});

test("a filter without matches clears back to every customer", async () => {
	const user = userEvent.setup();
	const requests = stubApi(listAnswers(jsonAnswer(customerPage([]))));
	const { router } = renderApp("/customers?revenue_filter=free&sort=pace");
	const table = await findCustomerTable();

	await user.click(
		await within(table).findByRole("button", { name: "Clear filters" }),
	);

	await waitFor(() => {
		expect(router.state.location.search).toEqual({ sort: "pace" });
	});
	expect(customerListQueries(requests).at(-1)).toEqual({
		...DEFAULT_QUERY,
		sort: "pace",
	});
});
