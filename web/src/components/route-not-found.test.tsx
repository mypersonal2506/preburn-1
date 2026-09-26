import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { renderApp, signedInAnswers, stubApi } from "@/routes/-render-app";

afterEach(() => {
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
});

test("an unknown path shows the not-found page and its link opens Overview", async () => {
	const consoleWarn = vi.spyOn(console, "warn");
	const user = userEvent.setup();
	stubApi(signedInAnswers);
	const { router } = renderApp("/unknown-page");

	expect(
		await screen.findByRole("heading", { name: "Page not found" }),
	).toBeInTheDocument();
	await user.click(screen.getByRole("link", { name: "Go to Overview" }));

	expect(
		await screen.findByRole("heading", { name: "Overview" }),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/");
	expect(consoleWarn).not.toHaveBeenCalled();
});

test("an unknown path inside a section shows the not-found page inside the shell", async () => {
	stubApi(signedInAnswers);

	renderApp("/customers/cust_01jbvagescfn78y0938nkrkayd/unknown");

	const main = await screen.findByRole("main");
	expect(
		await within(main).findByRole("heading", { name: "Page not found" }),
	).toBeInTheDocument();
	expect(screen.getByRole("link", { name: "Plans" })).toBeInTheDocument();
});
