import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import {
	creatorPlan,
	freePlan,
	legacyPlan,
	planPage,
} from "@/features/plans/plans-test-support";
import {
	type ApiAnswers,
	installationSettings,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

const LIST_ROUTE = "GET /api/v1/plans";

afterEach(() => {
	vi.unstubAllGlobals();
});

function listAnswers(planAnswer: () => Response): ApiAnswers {
	return { ...signedInAnswers, [LIST_ROUTE]: planAnswer };
}

test("lists each plan with its rule, customers and status", async () => {
	stubApi(
		listAnswers(jsonAnswer(planPage([creatorPlan, freePlan, legacyPlan]))),
	);
	renderApp("/plans");

	const table = await screen.findByRole("table", { name: "Plans" });
	await within(table).findByText("Creator");

	expect(
		within(table)
			.getAllByRole("columnheader")
			.map((header) => header.textContent),
	).toEqual(["Plan", "Rule", "Customers", "Status"]);
	const rows = within(table)
		.getAllByRole("row")
		.slice(1)
		.map((row) =>
			within(row)
				.getAllByRole("cell")
				.map((cell) => cell.textContent),
		);
	expect(rows).toEqual([
		["Creator", "40.0% margin target", "12", "Active"],
		["Free", "$2.00 fixed allowance", "1,204", "Active"],
		["Legacy", "25.0% margin target", "0", "Archived"],
	]);
	expect(within(table).getByRole("link", { name: "Creator" })).toHaveAttribute(
		"href",
		`/plans/${creatorPlan.id}`,
	);
	expect(screen.getByRole("link", { name: "New plan" })).toHaveAttribute(
		"href",
		"/plans/new",
	);
});

test("the default plan carries a Default badge", async () => {
	stubApi({
		...listAnswers(jsonAnswer(planPage([creatorPlan, freePlan]))),
		"GET /api/v1/settings": jsonAnswer({
			...installationSettings,
			default_plan_id: freePlan.id,
		}),
	});
	renderApp("/plans");

	const table = await screen.findByRole("table", { name: "Plans" });
	const freeRow = (await within(table).findByText("Free")).closest("tr");
	const creatorRow = within(table).getByText("Creator").closest("tr");
	if (freeRow === null || creatorRow === null) {
		throw new Error("plan rows missing");
	}

	expect(await within(freeRow).findByText("Default")).toBeInTheDocument();
	expect(within(creatorRow).queryByText("Default")).not.toBeInTheDocument();
});

test("Next asks for the page after the cursor", async () => {
	const user = userEvent.setup();
	const requests = stubApi(
		listAnswers(jsonAnswer(planPage([creatorPlan], "cursor-2"))),
	);
	renderApp("/plans");

	const table = await screen.findByRole("table", { name: "Plans" });
	await within(table).findByText("Creator");
	await user.click(screen.getByRole("button", { name: "Next" }));

	await vi.waitFor(() => {
		expect(
			requests
				.map((request) => new URL(request.url))
				.filter((url) => url.pathname === "/api/v1/plans")
				.map((url) => url.searchParams.get("cursor")),
		).toEqual([null, "cursor-2"]);
	});
});

test("an environment without plans shows the empty state", async () => {
	stubApi(listAnswers(jsonAnswer(planPage([]))));
	renderApp("/plans");

	expect(await screen.findByText("No plans yet")).toBeInTheDocument();
	expect(
		within(screen.getByRole("table", { name: "Plans" })).getByRole("link", {
			name: "New plan",
		}),
	).toHaveAttribute("href", "/plans/new");
});

test("a failed load offers Try again", async () => {
	const user = userEvent.setup();
	const requests = stubApi(listAnswers(problemAnswer(400, "invalid_cursor")));
	renderApp("/plans");

	expect(await screen.findByText("Something went wrong")).toBeInTheDocument();
	await user.click(screen.getByRole("button", { name: "Try again" }));

	await vi.waitFor(() => {
		expect(
			requests.filter(
				(request) => new URL(request.url).pathname === "/api/v1/plans",
			),
		).toHaveLength(2);
	});
});
