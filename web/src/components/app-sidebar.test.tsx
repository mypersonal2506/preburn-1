import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { getCurrentMemberQueryKey } from "@/client/@tanstack/react-query.gen";
import { getEnvironment, setEnvironment } from "@/lib/environment-store";
import { getThemeChoice, setThemeChoice } from "@/lib/theme";
import {
	installationSettings,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	signedInMember,
	stubApi,
} from "@/routes/-render-app";

const PHONE_WIDTH = 390;

afterEach(() => {
	setEnvironment("test");
	setThemeChoice("system");
	window.localStorage.clear();
	vi.unstubAllGlobals();
});

async function findSidebar(): Promise<HTMLElement> {
	await screen.findByRole("heading", { name: "Overview" });
	const sidebar = document.querySelector<HTMLElement>('[data-slot="sidebar"]');
	if (sidebar === null) {
		throw new Error("sidebar missing");
	}
	return sidebar;
}

test("the sidebar lists the v0.1 groups and items only", async () => {
	stubApi(signedInAnswers);
	renderApp("/");

	const sidebar = await findSidebar();

	for (const groupLabel of ["Monitor", "Control", "Developers"]) {
		expect(within(sidebar).getByText(groupLabel)).toBeInTheDocument();
	}
	expect(within(sidebar).queryByText("Plan ahead")).not.toBeInTheDocument();
	expect(
		within(sidebar)
			.getAllByRole("link")
			.map((link) => link.textContent),
	).toEqual([
		"Overview",
		"Customers",
		"Decisions",
		"Policies",
		"Plans",
		"Get started",
		"API keys",
		"SDK",
		"Settings",
	]);
});

test("at phone width the navigation sheet closes once a page opens", async () => {
	const user = userEvent.setup();
	vi.stubGlobal("innerWidth", PHONE_WIDTH);
	stubApi({
		...signedInAnswers,
		"GET /api/v1/plans": jsonAnswer({ items: [], next_cursor: null }),
	});
	const { router } = renderApp("/account");
	await screen.findByRole("heading", { name: "Account" });

	await user.click(screen.getByRole("button", { name: "Toggle Sidebar" }));
	const sheet = await screen.findByRole("dialog");
	await user.click(within(sheet).getByRole("link", { name: "Plans" }));

	expect(await screen.findByRole("heading", { name: "Plans" })).toBeVisible();
	expect(router.state.location.pathname).toBe("/plans");
	await waitFor(() => {
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});
});

test("the current page is marked active", async () => {
	stubApi(signedInAnswers);
	renderApp("/settings/members");

	await screen.findByRole("heading", { name: "Members" });

	expect(screen.getByRole("link", { name: "Settings" })).toHaveAttribute(
		"data-active",
		"true",
	);
	expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute(
		"data-active",
		"false",
	);
});

test("the top shows the installation name", async () => {
	stubApi(signedInAnswers);
	renderApp("/");

	const sidebar = await findSidebar();

	expect(
		await within(sidebar).findByText(installationSettings.installation_name),
	).toBeInTheDocument();
});

test("the top shows the hidden mark before the wordmark", async () => {
	stubApi(signedInAnswers);
	renderApp("/");

	const sidebar = await findSidebar();

	const mark = within(sidebar).getByText("Preburn").previousElementSibling;
	expect(mark).toHaveAttribute("data-slot", "preburn-mark");
	expect(mark).toHaveAttribute("aria-hidden", "true");
});

test("the default installation name is not shown twice", async () => {
	stubApi({
		...signedInAnswers,
		"GET /api/v1/settings": jsonAnswer({
			...installationSettings,
			installation_name: "Preburn",
		}),
	});
	const { queryClient } = renderApp("/");

	const sidebar = await findSidebar();
	await waitFor(() => {
		expect(queryClient.isFetching()).toBe(0);
	});

	expect(within(sidebar).getAllByText("Preburn")).toHaveLength(1);
});

test("switching environment updates the store and the next request's header", async () => {
	const user = userEvent.setup();
	const requests = stubApi(signedInAnswers);
	renderApp("/");
	await findSidebar();
	await waitFor(() => {
		expect(requests.at(-1)?.headers.get("X-Preburn-Environment")).toBe("test");
	});
	const sentBeforeSwitch = requests.length;

	await user.click(screen.getByRole("radio", { name: "Live" }));

	expect(getEnvironment()).toBe("live");
	expect(screen.getByRole("radio", { name: "Live" })).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await waitFor(() => {
		expect(requests.length).toBeGreaterThan(sentBeforeSwitch);
	});
	expect(requests.at(-1)?.headers.get("X-Preburn-Environment")).toBe("live");
});

test("the account menu shows the email and switches the theme", async () => {
	const user = userEvent.setup();
	stubApi(signedInAnswers);
	renderApp("/");
	await findSidebar();

	await user.click(
		screen.getByRole("button", { name: signedInMember.display_name }),
	);
	const menu = await screen.findByRole("menu");
	expect(
		within(menu).getByRole("menuitem", { name: signedInMember.email }),
	).toBeInTheDocument();
	await user.click(within(menu).getByRole("menuitemradio", { name: "Dark" }));

	expect(getThemeChoice()).toBe("dark");
	expect(document.documentElement).toHaveClass("dark");
});

test("the email in the account menu opens the account page", async () => {
	const user = userEvent.setup();
	stubApi(signedInAnswers);
	const { router } = renderApp("/");
	await findSidebar();

	await user.click(
		screen.getByRole("button", { name: signedInMember.display_name }),
	);
	await user.click(
		await screen.findByRole("menuitem", { name: signedInMember.email }),
	);

	expect(
		await screen.findByRole("heading", { name: "Account" }),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/account");
});

test("log out ends the session, clears cached data and opens login", async () => {
	const user = userEvent.setup();
	const requests = stubApi(signedInAnswers);
	const { router, queryClient } = renderApp("/");
	await findSidebar();

	await user.click(
		screen.getByRole("button", { name: signedInMember.display_name }),
	);
	await user.click(await screen.findByRole("menuitem", { name: "Log out" }));

	expect(
		await screen.findByRole("heading", { name: "Log in" }),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/login");
	expect(
		requests.some(
			(request) =>
				request.method === "POST" &&
				new URL(request.url).pathname === "/api/v1/auth/logout",
		),
	).toBe(true);
	expect(queryClient.getQueryData(getCurrentMemberQueryKey())).toBeUndefined();
});

test("log out with an ended session opens login without a toast and clears cached data", async () => {
	const user = userEvent.setup();
	stubApi({
		...signedInAnswers,
		"POST /api/v1/auth/logout": problemAnswer(401, "authentication_required"),
	});
	const { router, queryClient } = renderApp("/");
	await findSidebar();

	await user.click(
		screen.getByRole("button", { name: signedInMember.display_name }),
	);
	await user.click(await screen.findByRole("menuitem", { name: "Log out" }));

	expect(
		await screen.findByRole("heading", { name: "Log in" }),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/login");
	await waitFor(() => {
		expect(
			queryClient.getQueryData(getCurrentMemberQueryKey()),
		).toBeUndefined();
	});
	expect(screen.queryByText("Log out failed")).not.toBeInTheDocument();
});
