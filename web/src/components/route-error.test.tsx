import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, expect, test, vi } from "vitest";
import { Route as OverviewRoute } from "@/routes/_app/index";
import { renderApp, signedInAnswers, stubApi } from "@/routes/-render-app";

let overviewFails = true;

function FlakyOverviewPage() {
	if (overviewFails) {
		throw new Error("overview failed");
	}
	return <h1>Overview</h1>;
}

beforeAll(() => {
	OverviewRoute.update({ component: FlakyOverviewPage });
});

beforeEach(() => {
	overviewFails = true;
	vi.spyOn(console, "warn").mockImplementation(() => {});
});

afterEach(() => {
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
});

test("a failing page shows the error card inside the shell and logs the message", async () => {
	const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
	stubApi(signedInAnswers);

	renderApp("/");

	expect(
		await screen.findByRole("heading", { name: "Something went wrong" }),
	).toBeInTheDocument();
	expect(screen.getByRole("link", { name: "Customers" })).toBeInTheDocument();
	expect(consoleError).toHaveBeenCalledWith(
		"route error message=overview failed",
	);
});

test("Try again renders the page again", async () => {
	vi.spyOn(console, "error").mockImplementation(() => {});
	const user = userEvent.setup();
	stubApi(signedInAnswers);
	renderApp("/");
	await screen.findByRole("heading", { name: "Something went wrong" });

	overviewFails = false;
	await user.click(screen.getByRole("button", { name: "Try again" }));

	expect(
		await screen.findByRole("heading", { name: "Overview" }),
	).toBeInTheDocument();
	expect(
		screen.queryByRole("heading", { name: "Something went wrong" }),
	).not.toBeInTheDocument();
});
