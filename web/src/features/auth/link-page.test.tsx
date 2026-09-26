import { screen, waitFor } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { LinkDetailsResponse, MemberResponse } from "@/client";
import { getCurrentMemberQueryKey } from "@/client/@tanstack/react-query.gen";
import { sentBody, sentRoutes } from "@/features/auth/auth-test-support";
import {
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

const LINK_TOKEN = crypto.randomUUID();
const PASSWORD = "correct horse battery";

const invitedMember: MemberResponse = {
	id: "mem_01jbvb0x3w9rqhtaz0g8m2k7ne",
	email: "jordan@example.com",
	display_name: "Jordan Lee",
	has_password: true,
	status: "active",
	last_login_at: null,
	created_at: "2026-09-02T00:00:00Z",
};

afterEach(() => {
	vi.unstubAllGlobals();
});

function linkDetails(
	purpose: LinkDetailsResponse["purpose"],
): LinkDetailsResponse {
	return {
		purpose,
		email: invitedMember.email,
		display_name: invitedMember.display_name,
	};
}

async function setPassword(
	user: UserEvent,
	passwordConfirmation: string,
): Promise<void> {
	await user.type(screen.getByLabelText("New password"), PASSWORD);
	await user.type(
		screen.getByLabelText("Confirm password"),
		passwordConfirmation,
	);
	await user.click(screen.getByRole("button", { name: "Set password" }));
}

test("an invite link shows the email, sets the password and opens the overview", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...signedInAnswers,
		"POST /api/v1/auth/links/inspect": jsonAnswer(linkDetails("invite")),
		"POST /api/v1/auth/links/consume": jsonAnswer(invitedMember),
	});
	const { router, queryClient } = renderApp(`/link#${LINK_TOKEN}`);

	expect(
		await screen.findByRole("heading", { name: "Accept invite" }),
	).toBeInTheDocument();
	expect(screen.getByLabelText("Email")).toHaveValue(invitedMember.email);
	await waitFor(() => {
		expect(router.state.location.hash).toBe("");
	});
	expect(await sentBody(requests, "POST /api/v1/auth/links/inspect")).toEqual({
		token: LINK_TOKEN,
	});

	await setPassword(user, PASSWORD);

	expect(
		await screen.findByRole("heading", { name: "Overview" }),
	).toBeInTheDocument();
	expect(await sentBody(requests, "POST /api/v1/auth/links/consume")).toEqual({
		token: LINK_TOKEN,
		password: PASSWORD,
	});
	expect(queryClient.getQueryData(getCurrentMemberQueryKey())).toEqual(
		invitedMember,
	);
	expect(sentRoutes(requests)).not.toContain("GET /api/v1/auth/me");
});

test("a reset link shows the reset state", async () => {
	stubApi({
		...signedInAnswers,
		"POST /api/v1/auth/links/inspect": jsonAnswer(
			linkDetails("password_reset"),
		),
	});
	renderApp(`/link#${LINK_TOKEN}`);

	expect(
		await screen.findByRole("heading", { name: "Reset password" }),
	).toBeInTheDocument();
	expect(screen.getByLabelText("Email")).toHaveValue(invitedMember.email);
	expect(screen.getByLabelText("New password")).toBeInTheDocument();
});

test("an expired link asks for a new link", async () => {
	stubApi({
		...signedInAnswers,
		"POST /api/v1/auth/links/inspect": problemAnswer(410, "link_expired"),
	});
	renderApp(`/link#${LINK_TOKEN}`);

	expect(
		await screen.findByRole("heading", { name: "Link expired" }),
	).toBeInTheDocument();
	expect(
		screen.getByText("Ask another member for a new link."),
	).toBeInTheDocument();
	expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
});

test("a link without a token shows the expired state and sends nothing", async () => {
	const requests = stubApi(signedInAnswers);
	renderApp("/link");

	expect(
		await screen.findByRole("heading", { name: "Link expired" }),
	).toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain("POST /api/v1/auth/links/inspect");
});

test("a link used up while the form was open shows the expired state", async () => {
	const user = userEvent.setup();
	stubApi({
		...signedInAnswers,
		"POST /api/v1/auth/links/inspect": jsonAnswer(linkDetails("invite")),
		"POST /api/v1/auth/links/consume": problemAnswer(410, "link_expired"),
	});
	renderApp(`/link#${LINK_TOKEN}`);
	await screen.findByRole("heading", { name: "Accept invite" });

	await setPassword(user, PASSWORD);

	expect(
		await screen.findByRole("heading", { name: "Link expired" }),
	).toBeInTheDocument();
});

test("a password confirmation mismatch blocks submit", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...signedInAnswers,
		"POST /api/v1/auth/links/inspect": jsonAnswer(linkDetails("invite")),
	});
	renderApp(`/link#${LINK_TOKEN}`);
	await screen.findByRole("heading", { name: "Accept invite" });

	await setPassword(user, "correct horse battery staple");

	expect(await screen.findByText("Passwords do not match")).toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain("POST /api/v1/auth/links/consume");
});
