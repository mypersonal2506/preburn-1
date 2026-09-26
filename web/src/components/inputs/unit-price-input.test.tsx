import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactElement, useState } from "react";
import { expect, test } from "vitest";
import type { MeterDescription } from "@/client";
import { UnitPriceInput } from "@/components/inputs/unit-price-input";
import type { UnitPrice } from "@/lib/decimal";

type Meter = MeterDescription["meter"];

function renderUnitPriceInput(
	meter: Meter,
	initialPrice: UnitPrice | null,
): (UnitPrice | null)[] {
	const changes: (UnitPrice | null)[] = [];
	function UnitPriceHarness(): ReactElement {
		const [price, setPrice] = useState(initialPrice);
		return (
			<UnitPriceInput
				aria-label="Price"
				meter={meter}
				value={price}
				onChange={(nextPrice) => {
					changes.push(nextPrice);
					setPrice(nextPrice);
				}}
			/>
		);
	}
	render(<UnitPriceHarness />);
	return changes;
}

function priceField(): HTMLElement {
	return screen.getByRole("textbox", { name: "Price" });
}

test("token meters take a price per 1M tokens", async () => {
	const user = userEvent.setup();
	const changes = renderUnitPriceInput("input_tokens", null);

	expect(screen.getByText("per 1M input tokens")).toBeInTheDocument();
	await user.type(priceField(), "3.00");

	expect(changes.at(-1)).toEqual({ unit_price: "3", unit_quantity: 1_000_000 });
});

test("other meters take a price per unit", async () => {
	const user = userEvent.setup();
	const changes = renderUnitPriceInput("output_seconds", null);

	expect(screen.getByText("per second")).toBeInTheDocument();
	await user.type(priceField(), "0.15");

	expect(changes.at(-1)).toEqual({ unit_price: "0.15", unit_quantity: 1 });
});

test("a new meter shows the price in that meter's displayed unit", () => {
	const price: UnitPrice = { unit_price: "3", unit_quantity: 1_000_000 };
	const { rerender } = render(
		<UnitPriceInput
			aria-label="Price"
			meter="input_tokens"
			value={price}
			onChange={() => undefined}
		/>,
	);
	expect(priceField()).toHaveValue("3");

	rerender(
		<UnitPriceInput
			aria-label="Price"
			meter="output_seconds"
			value={price}
			onChange={() => undefined}
		/>,
	);

	expect(priceField()).toHaveValue("0.000003");
	expect(screen.getByText("per second")).toBeInTheDocument();
});

test("shows an API price per displayed unit", () => {
	renderUnitPriceInput("input_tokens", {
		unit_price: "0.000003000",
		unit_quantity: 1,
	});

	expect(priceField()).toHaveValue("3");
});

test("a new meter that cannot show the price asks for it again", () => {
	const changes: (UnitPrice | null)[] = [];
	function MeterSwitchHarness({ meter }: { meter: Meter }): ReactElement {
		const [price, setPrice] = useState<UnitPrice | null>({
			unit_price: "0.0375",
			unit_quantity: 1_000_000,
		});
		return (
			<UnitPriceInput
				aria-label="Price"
				meter={meter}
				value={price}
				onChange={(nextPrice) => {
					changes.push(nextPrice);
					setPrice(nextPrice);
				}}
			/>
		);
	}
	const { rerender } = render(
		<MeterSwitchHarness meter="cached_input_tokens" />,
	);
	expect(priceField()).toHaveValue("0.0375");

	rerender(<MeterSwitchHarness meter="requests" />);

	expect(priceField()).toHaveValue("");
	expect(priceField()).toHaveAttribute("aria-invalid", "true");
	expect(priceField()).toHaveAccessibleDescription(
		"Cannot show this value. Enter it again.",
	);
	expect(changes).toEqual([null]);
	expect(screen.getByText("per request")).toBeInTheDocument();
});

test("typing a price after an unshown one clears the problem", async () => {
	const user = userEvent.setup();
	const changes = renderUnitPriceInput("requests", {
		unit_price: "0.0375",
		unit_quantity: 1_000_000,
	});
	expect(priceField()).toHaveAttribute("aria-invalid", "true");

	await user.type(priceField(), "0.25");

	expect(changes.at(-1)).toEqual({ unit_price: "0.25", unit_quantity: 1 });
	expect(priceField()).toHaveAttribute("aria-invalid", "false");
});

test.each([
	["0.0000000001", "Use at most 9 decimals"],
	["$3", "Enter a number"],
	["-3", "Enter 0 or more"],
])("rejects %s inline with %s", async (text, message) => {
	const user = userEvent.setup();
	const changes = renderUnitPriceInput("output_seconds", null);

	await user.type(priceField(), text);

	expect(changes.at(-1)).toBeNull();
	expect(priceField()).toHaveAttribute("aria-invalid", "true");
	expect(priceField()).toHaveAccessibleDescription(message);
});
