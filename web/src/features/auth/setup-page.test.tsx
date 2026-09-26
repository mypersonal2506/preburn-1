import { screen, waitFor } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { getCurrentMemberQueryKey } from "@/client/@tanstack/react-query.gen";
import {
	detailedProblemAnswer,
	sentBody,
	sentRoutes,
} from "@/features/auth/auth-test-support";
import {
	type ApiAnswers,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	signedInMember,
	stubApi,
} from "@/routes/-render-app";

const SETUP_TOKEN = crypto.randomUUID();
const PASSWORD = "correct horse battery";

const pendingSetupAnswers: ApiAnswers = {
	...signedInAnswers,
	"GET /api/v1/setup/status": jsonAnswer({ setup_required: true }),
};

afterEach(() => {
	vi.unstubAllGlobals();
});

async function fillSetupForm(
	user: UserEvent,
	passwordConfirmation: string,
): Promise<void> {
	await user.type(await screen.findByLabelText("Email"), "sam@example.com");
	await user.type(screen.getByLabelText("Name"), "Sam Rivera");
	await user.type(screen.getByLabelText("Password"), PASSWORD);
	await user.type(
		screen.getByLabelText("Confirm password"),
		passwordConfirmation,
	);
}

test("setup reads the fragment token, removes it from the URL and sends it", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...pendingSetupAnswers,
		"POST /api/v1/setup": jsonAnswer(signedInMember),
	});
	const { router, queryClient } = renderApp(`/setup#${SETUP_TOKEN}`);

	await screen.findByRole("heading", { name: "Set up" });
	await waitFor(() => {
		expect(router.state.location.hash).toBe("");
	});
	expect(router.state.location.href).toBe("/setup");
	expect(router.state.location.state.fragmentToken).toBe(SETUP_TOKEN);

	await fillSetupForm(user, PASSWORD);
	await user.click(screen.getByRole("button", { name: "Complete setup" }));

	expect(
		await screen.findByRole("heading", { name: "Overview" }),
	).toBeInTheDocument();
	expect(await sentBody(requests, "POST /api/v1/setup")).toEqual({
		token: SETUP_TOKEN,
		email: "sam@example.com",
		display_name: "Sam Rivera",
		password: PASSWORD,
	});
	expect(queryClient.getQueryData(getCurrentMemberQueryKey())).toEqual(
		signedInMember,
	);
	expect(sentRoutes(requests)).not.toContain("GET /api/v1/auth/me");
	expect(router.state.location.href).not.toContain(SETUP_TOKEN);
});

test("a password confirmation mismatch blocks submit", async () => {
	const user = userEvent.setup();
	const requests = stubApi(pendingSetupAnswers);
	renderApp(`/setup#${SETUP_TOKEN}`);

	await fillSetupForm(user, "correct horse battery staple");
	await user.click(screen.getByRole("button", { name: "Complete setup" }));

	expect(await screen.findByText("Passwords do not match")).toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain("POST /api/v1/setup");
});

test("a short password shows the password rule and sends nothing", async () => {
	const user = userEvent.setup();
	const requests = stubApi(pendingSetupAnswers);
	renderApp(`/setup#${SETUP_TOKEN}`);

	await user.type(await screen.findByLabelText("Email"), "sam@example.com");
	await user.type(screen.getByLabelText("Name"), "Sam Rivera");
	await user.type(screen.getByLabelText("Password"), "too short");
	await user.type(screen.getByLabelText("Confirm password"), "too short");
	await user.click(screen.getByRole("button", { name: "Complete setup" }));

	expect(
		await screen.findByText("Use 12 to 256 characters"),
	).toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain("POST /api/v1/setup");
});

test("an outdated setup link says to use the newest one", async () => {
	const user = userEvent.setup();
	stubApi({
		...pendingSetupAnswers,
		"POST /api/v1/setup": problemAnswer(403, "setup_token_invalid"),
	});
	renderApp(`/setup#${SETUP_TOKEN}`);

	await fillSetupForm(user, PASSWORD);
	await user.click(screen.getByRole("button", { name: "Complete setup" }));

	expect(
		await screen.findByText(
			"This setup link is out of date. Use the newest one.",
		),
	).toBeInTheDocument();
});

test("an email the server rejects shows under the email field", async () => {
	const user = userEvent.setup();
	stubApi({
		...pendingSetupAnswers,
		"POST /api/v1/setup": detailedProblemAnswer(422, "validation_failed", {
			errors: [
				{
					location: "body.email",
					message: "expected an email address of at most 254 characters",
				},
			],
		}),
	});
	renderApp(`/setup#${SETUP_TOKEN}`);

	await fillSetupForm(user, PASSWORD);
	await user.click(screen.getByRole("button", { name: "Complete setup" }));

	expect(
		await screen.findByText("Enter a valid email address"),
	).toBeInTheDocument();
	expect(screen.getByLabelText("Email")).toHaveAttribute(
		"aria-invalid",
		"true",
	);
});

test("setup without a link token asks for the setup link", async () => {
	const requests = stubApi(pendingSetupAnswers);
	renderApp("/setup");

	expect(
		await screen.findByText("Open the setup link from the server log."),
	).toBeInTheDocument();
	expect(screen.queryByLabelText("Email")).not.toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain("POST /api/v1/setup");
});

test("a completed installation sends setup to the login page", async () => {
	stubApi(signedInAnswers);
	const { router } = renderApp(`/setup#${SETUP_TOKEN}`);

	expect(
		await screen.findByRole("heading", { name: "Log in" }),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/login");
});
