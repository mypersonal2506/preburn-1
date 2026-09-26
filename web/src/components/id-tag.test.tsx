import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toaster } from "sonner";
import { expect, test, vi } from "vitest";
import { IdTag } from "@/components/id-tag";

const customerId = "cust_01jbvagescfn78y0938nkrkayd";

test("shows the start and end of a long id and keeps it whole for screen readers", () => {
	render(<IdTag id={customerId} />);

	expect(screen.getByText("cust_01jbvag...krkayd")).toHaveAttribute(
		"aria-hidden",
		"true",
	);
	expect(screen.getByText(customerId)).toBeInTheDocument();
});

test("shows a short id whole", () => {
	render(<IdTag id="customer-42" />);

	expect(screen.getAllByText("customer-42")).toHaveLength(2);
});

test("copies the whole id", async () => {
	const user = userEvent.setup();
	render(
		<>
			<IdTag id={customerId} />
			<Toaster />
		</>,
	);

	await user.click(screen.getByRole("button", { name: "Copy id" }));

	expect(await navigator.clipboard.readText()).toBe(customerId);
	expect(await screen.findByText("Id copied")).toBeInTheDocument();
});

test("says so when the clipboard refuses", async () => {
	const user = userEvent.setup();
	vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
		new DOMException("write refused", "NotAllowedError"),
	);
	render(
		<>
			<IdTag id={customerId} />
			<Toaster />
		</>,
	);

	await user.click(screen.getByRole("button", { name: "Copy id" }));

	expect(await screen.findByText("Copy failed")).toBeInTheDocument();
});
