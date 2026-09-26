import { screen } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { getCurrentMemberQueryKey } from "@/client/@tanstack/react-query.gen";
import {
	detailedProblemAnswer,
	sentBody,
	sentRoutes,
} from "@/features/auth/auth-test-support";
import {
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	signedInMember,
	stubApi,
} from "@/routes/-render-app";

const PASSWORD = "correct horse battery";

afterEach(() => {
	vi.unstubAllGlobals();
});

async function logIn(user: UserEvent): Promise<void> {
	await user.type(await screen.findByLabelText("Email"), "sam@example.com");
	await user.type(screen.getByLabelText("Password"), PASSWORD);
	await user.click(screen.getByRole("button", { name: "Log in" }));
}

test("a login opens the overview with the member cached", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...signedInAnswers,
		"POST /api/v1/auth/login": jsonAnswer(signedInMember),
	});
	const { router, queryClient } = renderApp("/login");

	await logIn(user);

	expect(
		await screen.findByRole("heading", { name: "Overview" }),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/");
	expect(await sentBody(requests, "POST /api/v1/auth/login")).toEqual({
		email: "sam@example.com",
		password: PASSWORD,
	});
	expect(queryClient.getQueryData(getCurrentMemberQueryKey())).toEqual(
		signedInMember,
	);
	expect(sentRoutes(requests)).not.toContain("GET /api/v1/auth/me");
});

test("a wrong email or password shows the login failure", async () => {
	const user = userEvent.setup();
	stubApi({
		...signedInAnswers,
		"POST /api/v1/auth/login": problemAnswer(401, "login_failed"),
	});
	const { router } = renderApp("/login");

	await logIn(user);

	expect(
		await screen.findByText("Wrong email or password"),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/login");
});

test("the rate limit shows the wait in minutes, rounded up", async () => {
	const user = userEvent.setup();
	stubApi({
		...signedInAnswers,
		"POST /api/v1/auth/login": detailedProblemAnswer(429, "rate_limited", {
			retryAfterSeconds: 841,
		}),
	});
	renderApp("/login");

	await logIn(user);

	expect(
		await screen.findByText("Too many attempts. Try again in 15 minutes."),
	).toBeInTheDocument();
});

test("empty fields show their messages and send nothing", async () => {
	const user = userEvent.setup();
	const requests = stubApi(signedInAnswers);
	renderApp("/login");

	await user.click(await screen.findByRole("button", { name: "Log in" }));

	expect(await screen.findByText("Enter your email")).toBeInTheDocument();
	expect(screen.getByText("Enter your password")).toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain("POST /api/v1/auth/login");
});
