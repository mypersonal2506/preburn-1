import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactElement, useState } from "react";
import { expect, test } from "vitest";
import { AmountInput } from "@/components/inputs/amount-input";

const RESET_AMOUNT = "5.000000000";

function renderAmountInput(initialAmount: string | null): (string | null)[] {
	const changes: (string | null)[] = [];
	function AmountHarness(): ReactElement {
		const [amount, setAmount] = useState(initialAmount);
		return (
			<>
				<AmountInput
					aria-label="Allowance"
					value={amount}
					onChange={(nextAmount) => {
						changes.push(nextAmount);
						setAmount(nextAmount);
					}}
				/>
				<button type="button" onClick={() => setAmount(RESET_AMOUNT)}>
					Reset
				</button>
			</>
		);
	}
	render(<AmountHarness />);
	return changes;
}

function amountField(): HTMLElement {
	return screen.getByRole("textbox", { name: "Allowance" });
}

test("shows the API amount as dollars", () => {
	renderAmountInput("12.500000000");

	expect(amountField()).toHaveValue("12.5");
	expect(screen.getByText("$")).toBeInTheDocument();
});

test("converts typed dollars to the exact API amount", async () => {
	const user = userEvent.setup();
	const changes = renderAmountInput(null);

	await user.type(amountField(), "999999999.999999999");
	expect(changes.at(-1)).toBe("999999999.999999999");

	await user.clear(amountField());
	await user.type(amountField(), "0.1");
	expect(changes.at(-1)).toBe("0.100000000");
	expect(amountField()).not.toHaveAttribute("aria-invalid", "true");
});

test.each([
	["12.3.4", "Enter a number"],
	["1.0000000001", "Use at most 9 decimals"],
	["1234567890", "Number is too large"],
])("rejects %s inline with %s", async (text, message) => {
	const user = userEvent.setup();
	const changes = renderAmountInput(null);

	await user.type(amountField(), text);

	expect(changes.at(-1)).toBeNull();
	expect(amountField()).toHaveAttribute("aria-invalid", "true");
	expect(amountField()).toHaveAccessibleDescription(message);
	expect(amountField()).toHaveValue(text);
});

test("an empty field has no amount and no error", async () => {
	const user = userEvent.setup();
	const changes = renderAmountInput("12.500000000");

	await user.clear(amountField());

	expect(changes.at(-1)).toBeNull();
	expect(amountField()).not.toHaveAttribute("aria-invalid", "true");
	expect(amountField()).not.toHaveAccessibleDescription();
});

test("keeps the typed text while it means the same amount", async () => {
	const user = userEvent.setup();
	const changes = renderAmountInput(null);

	await user.type(amountField(), "12.50");

	expect(changes.at(-1)).toBe("12.500000000");
	expect(amountField()).toHaveValue("12.50");
});

test("follows an amount set from outside", async () => {
	const user = userEvent.setup();
	renderAmountInput(null);

	await user.type(amountField(), "abc");
	await user.click(screen.getByRole("button", { name: "Reset" }));

	expect(amountField()).toHaveValue("5");
	expect(amountField()).not.toHaveAttribute("aria-invalid", "true");
});

test("an invalid input from outside is marked invalid", () => {
	render(
		<AmountInput
			aria-label="Allowance"
			invalid
			value="1.000000000"
			onChange={() => undefined}
		/>,
	);

	expect(amountField()).toHaveAttribute("aria-invalid", "true");
});
