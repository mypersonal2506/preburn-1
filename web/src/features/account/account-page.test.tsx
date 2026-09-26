import { screen, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { MemberResponse } from "@/client";
import { getCurrentMemberQueryKey } from "@/client/@tanstack/react-query.gen";
import {
	detailedProblemAnswer,
	sentBody,
	sentRoutes,
} from "@/features/auth/auth-test-support";
import {
	jsonAnswer,
	renderApp,
	signedInAnswers,
	signedInMember,
	stubApi,
} from "@/routes/-render-app";

const CURRENT_PASSWORD = "correct horse battery";
const NEW_PASSWORD = "tangerine submarine lighthouse";

const renamedMember: MemberResponse = {
	...signedInMember,
	display_name: "Samantha Rivera",
};

afterEach(() => {
	vi.unstubAllGlobals();
});

async function findSection(title: string): Promise<HTMLElement> {
	const heading = await screen.findByRole("heading", { name: title });
	const section = heading.closest<HTMLElement>('[data-slot="card"]');
	if (section === null) {
		throw new Error(`section card missing title=${title}`);
	}
	return section;
}

async function changePassword(
	user: UserEvent,
	passwordConfirmation: string,
): Promise<void> {
	const section = await findSection("Password");
	await user.type(
		within(section).getByLabelText("Current password"),
		CURRENT_PASSWORD,
	);
	await user.type(within(section).getByLabelText("New password"), NEW_PASSWORD);
	await user.type(
		within(section).getByLabelText("Confirm password"),
		passwordConfirmation,
	);
	await user.click(
		within(section).getByRole("button", { name: "Change password" }),
	);
}

test("saving the name updates the member in the cache and the sidebar", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...signedInAnswers,
		"PATCH /api/v1/auth/me": jsonAnswer(renamedMember),
	});
	const { queryClient } = renderApp("/account");
	const section = await findSection("Profile");
	const nameInput = within(section).getByLabelText("Name");
	expect(nameInput).toHaveValue(signedInMember.display_name);

	await user.clear(nameInput);
	await user.type(nameInput, renamedMember.display_name);
	await user.click(within(section).getByRole("button", { name: "Save" }));

	expect(await screen.findByText("Name saved")).toBeInTheDocument();
	expect(await sentBody(requests, "PATCH /api/v1/auth/me")).toEqual({
		display_name: renamedMember.display_name,
	});
	expect(queryClient.getQueryData(getCurrentMemberQueryKey())).toEqual(
		renamedMember,
	);
	const sidebar = document.querySelector<HTMLElement>('[data-slot="sidebar"]');
	if (sidebar === null) {
		throw new Error("sidebar missing");
	}
	expect(
		await within(sidebar).findByText(renamedMember.display_name),
	).toBeInTheDocument();
	expect(
		sentRoutes(requests).filter((route) => route === "GET /api/v1/auth/me"),
	).toHaveLength(1);
});

test("a blank name shows the name rule and sends nothing", async () => {
	const user = userEvent.setup();
	const requests = stubApi(signedInAnswers);
	renderApp("/account");
	const section = await findSection("Profile");

	await user.clear(within(section).getByLabelText("Name"));
	await user.type(within(section).getByLabelText("Name"), "   ");
	await user.click(within(section).getByRole("button", { name: "Save" }));

	expect(
		await within(section).findByText("Use 1 to 80 characters"),
	).toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain("PATCH /api/v1/auth/me");
});

test("changing the password sends the current and new password", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...signedInAnswers,
		"PATCH /api/v1/auth/me": jsonAnswer(signedInMember),
	});
	renderApp("/account");

	await changePassword(user, NEW_PASSWORD);

	expect(await screen.findByText("Password changed")).toBeInTheDocument();
	expect(await sentBody(requests, "PATCH /api/v1/auth/me")).toEqual({
		password: { current: CURRENT_PASSWORD, new: NEW_PASSWORD },
	});
	const section = await findSection("Password");
	expect(within(section).getByLabelText("Current password")).toHaveValue("");
	expect(within(section).getByLabelText("New password")).toHaveValue("");
	expect(within(section).getByLabelText("Confirm password")).toHaveValue("");
});

test("a wrong current password shows under its field", async () => {
	const user = userEvent.setup();
	stubApi({
		...signedInAnswers,
		"PATCH /api/v1/auth/me": detailedProblemAnswer(422, "validation_failed", {
			errors: [
				{
					location: "body.password.current",
					message: "does not match the current password",
				},
			],
		}),
	});
	renderApp("/account");

	await changePassword(user, NEW_PASSWORD);

	const section = await findSection("Password");
	expect(
		await within(section).findByText("Current password is wrong"),
	).toBeInTheDocument();
	expect(within(section).getByLabelText("Current password")).toHaveAttribute(
		"aria-invalid",
		"true",
	);
});

test("the password change rate limit shows the wait in seconds", async () => {
	const user = userEvent.setup();
	stubApi({
		...signedInAnswers,
		"PATCH /api/v1/auth/me": detailedProblemAnswer(429, "rate_limited", {
			retryAfterSeconds: 30,
		}),
	});
	renderApp("/account");

	await changePassword(user, NEW_PASSWORD);

	expect(
		await screen.findByText("Too many attempts. Try again in 30 seconds."),
	).toBeInTheDocument();
});

test("a password confirmation mismatch blocks submit", async () => {
	const user = userEvent.setup();
	const requests = stubApi(signedInAnswers);
	renderApp("/account");

	await changePassword(user, "tangerine submarine lighthouse keeper");

	expect(await screen.findByText("Passwords do not match")).toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain("PATCH /api/v1/auth/me");
});
