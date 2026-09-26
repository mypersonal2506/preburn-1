import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import {
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

afterEach(() => {
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
});

test("pending setup redirects to /setup", async () => {
	stubApi({
		...signedInAnswers,
		"GET /api/v1/setup/status": jsonAnswer({ setup_required: true }),
	});

	const { router } = renderApp("/");

	expect(
		await screen.findByRole("heading", { name: "Set up" }),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/setup");
});

test("no session redirects to /login", async () => {
	stubApi({
		...signedInAnswers,
		"GET /api/v1/auth/me": problemAnswer(401, "authentication_required"),
	});

	const { router } = renderApp("/customers");

	expect(
		await screen.findByRole("heading", { name: "Log in" }),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/login");
});

test("a session reaches the overview stub inside the shell", async () => {
	stubApi(signedInAnswers);

	const { router } = renderApp("/");

	const main = await screen.findByRole("main");
	expect(
		await within(main).findByRole("heading", { name: "Overview" }),
	).toBeInTheDocument();
	expect(screen.getByRole("link", { name: "Customers" })).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/");
	await waitFor(() => {
		expect(document.title).toBe("Overview - Preburn");
	});
});

test("a completed setup is not asked for again, a pending one is", async () => {
	let setupRequired = true;
	const requests = stubApi({
		...signedInAnswers,
		"GET /api/v1/setup/status": () =>
			jsonAnswer({ setup_required: setupRequired })(),
	});
	const { router } = renderApp("/");
	await screen.findByRole("heading", { name: "Set up" });

	setupRequired = false;
	await router.navigate({ to: "/" });
	await router.navigate({ to: "/customers" });

	expect(
		await screen.findByRole("heading", { name: "Customers" }),
	).toBeInTheDocument();
	const setupStatusRequests = requests.filter(
		(request) => new URL(request.url).pathname === "/api/v1/setup/status",
	);
	expect(setupStatusRequests).toHaveLength(2);
});

test("an API failure while loading the shell shows the error card", async () => {
	const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
	vi.spyOn(console, "warn").mockImplementation(() => {});
	stubApi({
		...signedInAnswers,
		"GET /api/v1/auth/me": problemAnswer(404, "not_found"),
	});

	renderApp("/");

	expect(
		await screen.findByRole("heading", { name: "Something went wrong" }),
	).toBeInTheDocument();
	expect(consoleError).toHaveBeenCalledWith(
		"route error message=api problem status=404 code=not_found",
	);
});

test("a slow shell load shows the pending skeleton", async () => {
	vi.stubGlobal(
		"fetch",
		vi.fn(() => new Promise<Response>(() => {})),
	);

	renderApp("/");

	await waitFor(
		() => {
			expect(document.querySelector('[aria-busy="true"]')).toBeInTheDocument();
		},
		{ timeout: 2000 },
	);
	expect(screen.queryByRole("heading")).not.toBeInTheDocument();
});
