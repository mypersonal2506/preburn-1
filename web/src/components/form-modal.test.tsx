import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";
import { FormModal } from "@/components/form-modal";
import { Input } from "@/components/ui/input";

function renderAddMember({
	pending,
	rootError,
	onSubmit = () => {},
}: {
	pending: boolean;
	rootError?: string;
	onSubmit?: () => void;
}) {
	return render(
		<FormModal
			open
			onOpenChange={() => {}}
			title="Add member"
			submitLabel="Add"
			pending={pending}
			rootError={rootError}
			onSubmit={onSubmit}
		>
			<Input aria-label="Email" />
		</FormModal>,
	);
}

test("shows the root error", () => {
	renderAddMember({ pending: false, rootError: "Email is already a member" });

	expect(screen.getByRole("alert")).toHaveTextContent(
		"Email is already a member",
	);
});

test("shows no alert without a root error", () => {
	renderAddMember({ pending: false });

	expect(screen.queryByRole("alert")).toBeNull();
	expect(screen.getByRole("dialog", { name: "Add member" })).toBeVisible();
});

test("disables submit while pending", () => {
	renderAddMember({ pending: true });

	expect(screen.getByRole("button", { name: "Add" })).toBeDisabled();
	expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
});

test("submits once from the keyboard", async () => {
	const user = userEvent.setup();
	const onSubmit = vi.fn();
	renderAddMember({ pending: false, onSubmit });

	await user.type(
		screen.getByRole("textbox", { name: "Email" }),
		"sam@example.com{Enter}",
	);

	expect(onSubmit).toHaveBeenCalledOnce();
});

test("submits without the browser's own validation", async () => {
	const user = userEvent.setup();
	const onSubmit = vi.fn();
	render(
		<FormModal
			open
			onOpenChange={() => {}}
			title="Add member"
			submitLabel="Add"
			pending={false}
			onSubmit={onSubmit}
		>
			<Input aria-label="Email" type="email" required />
		</FormModal>,
	);

	await user.click(screen.getByRole("button", { name: "Add" }));

	expect(onSubmit).toHaveBeenCalledOnce();
});
