import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import {
	detailedProblemAnswer,
	sentBody,
	sentRoutes,
} from "@/features/auth/auth-test-support";
import {
	addedMember,
	invitedMember,
	memberPage,
	noContentAnswer,
	problemWithDetailAnswer,
	removedMember,
	resetLink,
	statusAnswer,
} from "@/features/settings/settings-test-support";
import { formatDateTime } from "@/lib/format";
import {
	type ApiAnswers,
	jsonAnswer,
	renderApp,
	signedInAnswers,
	signedInMember,
	stubApi,
} from "@/routes/-render-app";

const MEMBERS_ROUTE = "GET /api/v1/members";
const ADD_ROUTE = "POST /api/v1/members";

afterEach(() => {
	vi.unstubAllGlobals();
});

function memberAnswers(extraAnswers: ApiAnswers = {}): ApiAnswers {
	return {
		...signedInAnswers,
		[MEMBERS_ROUTE]: jsonAnswer(memberPage),
		...extraAnswers,
	};
}

async function findMemberRow(displayName: string): Promise<HTMLElement> {
	const table = await screen.findByRole("table", { name: "Members" });
	const nameCell = await within(table).findByText(displayName);
	const row = nameCell.closest<HTMLElement>("tr");
	if (row === null) {
		throw new Error(`member row missing name=${displayName}`);
	}
	return row;
}

async function chooseRowAction(
	user: UserEvent,
	displayName: string,
	action: string,
): Promise<void> {
	const row = await findMemberRow(displayName);
	await user.click(within(row).getByRole("button", { name: "Row actions" }));
	await user.click(await screen.findByRole("menuitem", { name: action }));
}

test("the list shows each member's name, email, status and last login", async () => {
	stubApi(memberAnswers());
	renderApp("/settings/members");

	expect(await screen.findByRole("tab", { name: "Members" })).toHaveAttribute(
		"aria-selected",
		"true",
	);
	const table = await screen.findByRole("table", { name: "Members" });
	expect(
		within(table)
			.getAllByRole("columnheader")
			.map((header) => header.textContent),
	).toEqual(["Name", "Email", "Status", "Last login", "Actions"]);
	const samRow = await findMemberRow(signedInMember.display_name);
	expect(within(samRow).getByText(signedInMember.email)).toBeInTheDocument();
	expect(within(samRow).getByText("Active")).toBeInTheDocument();
	expect(within(samRow).queryByText("Invited")).not.toBeInTheDocument();
	const jordanRow = await findMemberRow(invitedMember.display_name);
	expect(within(jordanRow).getByText("Invited")).toBeInTheDocument();
	expect(within(jordanRow).getByText("Never")).toBeInTheDocument();
	const alexRow = await findMemberRow(removedMember.display_name);
	expect(within(alexRow).getByText("Disabled")).toBeInTheDocument();
	expect(
		within(alexRow).getByText(formatDateTime("2026-09-12T09:30:00Z")),
	).toBeInTheDocument();
});

test("add shows the link once and it is not retrievable afterwards", async () => {
	const user = userEvent.setup();
	const requests = stubApi(
		memberAnswers({ [ADD_ROUTE]: statusAnswer(201, addedMember) }),
	);
	const { queryClient } = renderApp("/settings/members");
	await findMemberRow(signedInMember.display_name);

	await user.click(screen.getByRole("button", { name: "Add member" }));
	const addDialog = await screen.findByRole("dialog", { name: "Add member" });
	await user.type(
		within(addDialog).getByLabelText("Email"),
		` ${addedMember.member.email} `,
	);
	await user.type(
		within(addDialog).getByLabelText("Name"),
		addedMember.member.display_name,
	);
	await user.click(within(addDialog).getByRole("button", { name: "Add" }));

	const linkDialog = await screen.findByRole("dialog", { name: "Invite link" });
	expect(await sentBody(requests, ADD_ROUTE)).toEqual({
		email: addedMember.member.email,
		display_name: addedMember.member.display_name,
	});
	expect(
		within(linkDialog).getByText(
			"Send this link to Morgan Ellis. It works once and expires in 24 hours.",
		),
	).toBeInTheDocument();
	expect(
		within(linkDialog).getByText(addedMember.link_url),
	).toBeInTheDocument();
	expect(
		within(linkDialog).getByRole("button", { name: "Copy link" }),
	).toBeInTheDocument();
	await waitFor(() => {
		expect(
			sentRoutes(requests).filter((route) => route === MEMBERS_ROUTE),
		).toHaveLength(2);
	});

	await user.click(within(linkDialog).getByRole("button", { name: "Done" }));

	await waitFor(() => {
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});
	expect(screen.queryByText(addedMember.link_url)).not.toBeInTheDocument();
	await waitFor(() => {
		expect(queryClient.getMutationCache().getAll()).toEqual([]);
	});
	expect(
		JSON.stringify(
			queryClient
				.getQueryCache()
				.getAll()
				.map((query) => query.state.data),
		),
	).not.toContain(addedMember.link_url);
});

test("adding a taken email shows the problem in the modal", async () => {
	const user = userEvent.setup();
	stubApi(
		memberAnswers({
			[ADD_ROUTE]: detailedProblemAnswer(409, "member_email_taken", {}),
		}),
	);
	renderApp("/settings/members");
	await findMemberRow(signedInMember.display_name);

	await user.click(screen.getByRole("button", { name: "Add member" }));
	const addDialog = await screen.findByRole("dialog", { name: "Add member" });
	await user.type(
		within(addDialog).getByLabelText("Email"),
		signedInMember.email,
	);
	await user.type(within(addDialog).getByLabelText("Name"), "Sam");
	await user.click(within(addDialog).getByRole("button", { name: "Add" }));

	expect(
		await within(addDialog).findByText("A member with this email exists."),
	).toBeInTheDocument();
	expect(screen.queryByRole("dialog", { name: "Invite link" })).toBeNull();
});

test("an invalid email from the server shows under the email field", async () => {
	const user = userEvent.setup();
	stubApi(
		memberAnswers({
			[ADD_ROUTE]: detailedProblemAnswer(422, "validation_failed", {
				errors: [{ location: "body.email", message: "must be an email" }],
			}),
		}),
	);
	renderApp("/settings/members");
	await findMemberRow(signedInMember.display_name);

	await user.click(screen.getByRole("button", { name: "Add member" }));
	const addDialog = await screen.findByRole("dialog", { name: "Add member" });
	await user.type(within(addDialog).getByLabelText("Email"), "jordan@example");
	await user.type(within(addDialog).getByLabelText("Name"), "Jordan");
	await user.click(within(addDialog).getByRole("button", { name: "Add" }));

	expect(
		await within(addDialog).findByText("Enter a valid email address"),
	).toBeInTheDocument();
	expect(within(addDialog).getByLabelText("Email")).toHaveAttribute(
		"aria-invalid",
		"true",
	);
});

test("removing yourself shows the last_member message chosen by code", async () => {
	const user = userEvent.setup();
	const lastMemberDetail =
		"cannot remove yourself or the last member who can sign in";
	const requests = stubApi(
		memberAnswers({
			[`DELETE /api/v1/members/${signedInMember.id}`]: problemWithDetailAnswer(
				409,
				"last_member",
				lastMemberDetail,
			),
		}),
	);
	renderApp("/settings/members");

	await chooseRowAction(user, signedInMember.display_name, "Remove");
	const confirmDialog = await screen.findByRole("dialog", {
		name: `Remove ${signedInMember.display_name}`,
	});
	await user.click(
		within(confirmDialog).getByRole("button", { name: "Remove" }),
	);

	expect(
		await within(confirmDialog).findByText(
			"Cannot remove yourself or the last member who can log in.",
		),
	).toBeInTheDocument();
	expect(within(confirmDialog).queryByText(lastMemberDetail)).toBeNull();
	expect(sentRoutes(requests)).toContain(
		`DELETE /api/v1/members/${signedInMember.id}`,
	);
	expect(
		sentRoutes(requests).filter((route) => route === MEMBERS_ROUTE),
	).toHaveLength(1);
});

test("removing a member refreshes the list", async () => {
	const user = userEvent.setup();
	const requests = stubApi(
		memberAnswers({
			[`DELETE /api/v1/members/${invitedMember.id}`]: noContentAnswer,
		}),
	);
	renderApp("/settings/members");

	await chooseRowAction(user, invitedMember.display_name, "Remove");
	const confirmDialog = await screen.findByRole("dialog", {
		name: `Remove ${invitedMember.display_name}`,
	});
	await user.click(
		within(confirmDialog).getByRole("button", { name: "Remove" }),
	);

	expect(await screen.findByText("Member removed")).toBeInTheDocument();
	await waitFor(() => {
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});
	await waitFor(() => {
		expect(
			sentRoutes(requests).filter((route) => route === MEMBERS_ROUTE),
		).toHaveLength(2);
	});
});

test("a reset link shows once with a copy button", async () => {
	const user = userEvent.setup();
	const requests = stubApi(
		memberAnswers({
			[`POST /api/v1/members/${invitedMember.id}/reset-link`]: statusAnswer(
				201,
				resetLink,
			),
		}),
	);
	const { queryClient } = renderApp("/settings/members");

	await chooseRowAction(user, invitedMember.display_name, "Reset link");

	const linkDialog = await screen.findByRole("dialog", { name: "Reset link" });
	expect(
		await within(linkDialog).findByText(resetLink.link_url),
	).toBeInTheDocument();
	expect(
		within(linkDialog).getByText(
			"Send this link to Jordan Lee. It works once and expires in 24 hours.",
		),
	).toBeInTheDocument();
	expect(sentRoutes(requests)).toContain(
		`POST /api/v1/members/${invitedMember.id}/reset-link`,
	);

	await user.click(within(linkDialog).getByRole("button", { name: "Done" }));

	await waitFor(() => {
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});
	expect(screen.queryByText(resetLink.link_url)).not.toBeInTheDocument();
	await waitFor(() => {
		expect(queryClient.getMutationCache().getAll()).toEqual([]);
	});
});

test("a reset link for a member removed meanwhile shows member_disabled", async () => {
	const user = userEvent.setup();
	stubApi(
		memberAnswers({
			[`POST /api/v1/members/${invitedMember.id}/reset-link`]:
				problemWithDetailAnswer(
					409,
					"member_disabled",
					"the member is disabled",
				),
		}),
	);
	renderApp("/settings/members");

	await chooseRowAction(user, invitedMember.display_name, "Reset link");

	const linkDialog = await screen.findByRole("dialog", { name: "Reset link" });
	expect(
		await within(linkDialog).findByText("This member was removed."),
	).toBeInTheDocument();
	expect(within(linkDialog).queryByText(/Send this link/)).toBeNull();
});

test("a removed member has no actions", async () => {
	const user = userEvent.setup();
	stubApi(memberAnswers());
	renderApp("/settings/members");
	const row = await findMemberRow(removedMember.display_name);

	await user.click(within(row).getByRole("button", { name: "Row actions" }));

	expect(
		await screen.findByRole("menuitem", { name: "Reset link" }),
	).toHaveAttribute("aria-disabled", "true");
	expect(screen.getByRole("menuitem", { name: "Remove" })).toHaveAttribute(
		"aria-disabled",
		"true",
	);
});

test("a failed list offers Try again", async () => {
	const user = userEvent.setup();
	let membersFail = true;
	stubApi(
		memberAnswers({
			[MEMBERS_ROUTE]: () =>
				membersFail
					? detailedProblemAnswer(422, "invalid_cursor", {})()
					: jsonAnswer(memberPage)(),
		}),
	);
	renderApp("/settings/members");
	const retry = await screen.findByRole("button", { name: "Try again" });
	membersFail = false;

	await user.click(retry);

	expect(await findMemberRow(signedInMember.display_name)).toBeInTheDocument();
});
