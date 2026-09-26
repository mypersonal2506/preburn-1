import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactElement, useState } from "react";
import { expect, test, vi } from "vitest";
import { CountInput } from "@/features/policies/count-input";

function CountField({
	onChange,
}: {
	onChange: (count: string | null) => void;
}): ReactElement {
	const [count, setCount] = useState<string | null>(null);
	return (
		<CountInput
			aria-label="Requests"
			value={count}
			onChange={(nextCount) => {
				setCount(nextCount);
				onChange(nextCount);
			}}
		/>
	);
}

test("reports typed digits as a whole-number string without leading zeros", async () => {
	const user = userEvent.setup();
	const onChange = vi.fn();
	render(<CountField onChange={onChange} />);

	await user.type(screen.getByLabelText("Requests"), "0020");

	expect(onChange).toHaveBeenLastCalledWith("20");
});

test.each([
	["1.5", "Enter a whole number"],
	["-3", "Enter 0 or more"],
	["ten", "Enter a number"],
	["1234567890", "Number is too large"],
])("rejects %s with %s", async (text, message) => {
	const user = userEvent.setup();
	const onChange = vi.fn();
	render(<CountField onChange={onChange} />);

	await user.type(screen.getByLabelText("Requests"), text);

	expect(screen.getByText(message)).toBeInTheDocument();
	expect(onChange).toHaveBeenLastCalledWith(null);
});

test("shows the API value", () => {
	render(<CountInput aria-label="Requests" value="100" onChange={vi.fn()} />);

	expect(screen.getByLabelText("Requests")).toHaveValue("100");
});
