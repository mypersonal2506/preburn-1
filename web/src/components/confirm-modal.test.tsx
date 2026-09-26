import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";
import { ConfirmModal } from "@/components/confirm-modal";
import { ApiProblem } from "@/lib/api-problem";
import type { CodeMessages } from "@/lib/form-problem";

const lastMemberProblem = new ApiProblem({
	type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#last_member",
	title: "Conflict",
	status: 409,
	code: "last_member",
	detail: "cannot remove yourself or the last member who can sign in",
	errors: [],
	retryAfterSeconds: null,
});

function renderRemoveMember({
	pending,
	error,
	codeMessages,
	onConfirm = () => {},
}: {
	pending: boolean;
	error: unknown;
	codeMessages?: CodeMessages;
	onConfirm?: () => void;
}) {
	render(
		<ConfirmModal
			open
			onOpenChange={() => {}}
			title="Remove Sam Rivera"
			description="Sam loses access at once."
			confirmLabel="Remove"
			pending={pending}
			error={error}
			codeMessages={codeMessages}
			onConfirm={onConfirm}
		/>,
	);
}

test("confirms once", async () => {
	const user = userEvent.setup();
	const onConfirm = vi.fn();
	renderRemoveMember({ pending: false, error: null, onConfirm });

	expect(
		screen.getByRole("dialog", { name: "Remove Sam Rivera" }),
	).toHaveAccessibleDescription("Sam loses access at once.");
	expect(screen.queryByRole("alert")).toBeNull();

	await user.click(screen.getByRole("button", { name: "Remove" }));

	expect(onConfirm).toHaveBeenCalledOnce();
});

test("disables the confirm button while pending", () => {
	renderRemoveMember({ pending: true, error: null });

	expect(screen.getByRole("button", { name: "Remove" })).toBeDisabled();
});

test("shows the detail of a failed request", () => {
	renderRemoveMember({ pending: false, error: lastMemberProblem });

	expect(screen.getByRole("alert")).toHaveTextContent(
		"cannot remove yourself or the last member who can sign in",
	);
});

test("shows the message chosen by the problem's code instead of its detail", () => {
	renderRemoveMember({
		pending: false,
		error: lastMemberProblem,
		codeMessages: { last_member: () => "Keep one member who can log in." },
	});

	expect(screen.getByRole("alert")).toHaveTextContent(
		"Keep one member who can log in.",
	);
	expect(screen.queryByText(/cannot remove yourself/)).toBeNull();
});

test("shows a network failure as an unexpected response", () => {
	renderRemoveMember({ pending: false, error: new TypeError("fetch failed") });

	expect(screen.getByRole("alert")).toHaveTextContent("unexpected response");
});
