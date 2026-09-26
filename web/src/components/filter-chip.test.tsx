import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";
import { FilterChip } from "@/components/filter-chip";

test("a pressed chip reports its state", () => {
	render(
		<>
			<FilterChip label="All" pressed={false} />
			<FilterChip label="Paying" pressed />
		</>,
	);

	expect(screen.getByRole("button", { name: "Paying" })).toHaveAttribute(
		"aria-pressed",
		"true",
	);
	expect(screen.getByRole("button", { name: "All" })).toHaveAttribute(
		"aria-pressed",
		"false",
	);
});

test("a chip with a value shows it and clears it", async () => {
	const user = userEvent.setup();
	const onClear = vi.fn();
	render(<FilterChip label="Outcome" value="Denied" onClear={onClear} />);

	expect(
		screen.getByRole("button", { name: "Outcome: Denied" }),
	).toBeInTheDocument();

	await user.click(screen.getByRole("button", { name: "Clear Outcome" }));

	expect(onClear).toHaveBeenCalledOnce();
});

test("a chip without a value has nothing to clear", () => {
	render(<FilterChip label="Outcome" onClear={() => {}} />);

	expect(screen.getByRole("button", { name: "Outcome" })).toBeInTheDocument();
	expect(screen.queryByRole("button", { name: "Clear Outcome" })).toBeNull();
});
