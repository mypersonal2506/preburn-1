import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactElement, useState } from "react";
import { expect, test } from "vitest";
import { PercentInput } from "@/components/inputs/percent-input";

function renderPercentInput(initialRatio: string | null): (string | null)[] {
	const changes: (string | null)[] = [];
	function PercentHarness(): ReactElement {
		const [ratio, setRatio] = useState(initialRatio);
		return (
			<PercentInput
				aria-label="Target margin"
				value={ratio}
				onChange={(nextRatio) => {
					changes.push(nextRatio);
					setRatio(nextRatio);
				}}
			/>
		);
	}
	render(<PercentHarness />);
	return changes;
}

function percentField(): HTMLElement {
	return screen.getByRole("textbox", { name: "Target margin" });
}

test("shows the API ratio as a percent", () => {
	renderPercentInput("0.4025");

	expect(percentField()).toHaveValue("40.25");
	expect(screen.getByText("%")).toBeInTheDocument();
});

test.each([
	["40", "0.4000"],
	["-50", "-0.5000"],
	["0.01", "0.0001"],
])("converts %s percent to the ratio %s", async (text, ratio) => {
	const user = userEvent.setup();
	const changes = renderPercentInput(null);

	await user.type(percentField(), text);

	expect(changes.at(-1)).toBe(ratio);
});

test.each([
	["12.345", "Use at most 2 decimals"],
	["forty", "Enter a number"],
])("rejects %s inline with %s", async (text, message) => {
	const user = userEvent.setup();
	const changes = renderPercentInput(null);

	await user.type(percentField(), text);

	expect(changes.at(-1)).toBeNull();
	expect(percentField()).toHaveAttribute("aria-invalid", "true");
	expect(percentField()).toHaveAccessibleDescription(message);
});

test("an infinite ratio shows the field empty and asks for a value", () => {
	const changes = renderPercentInput("inf");

	expect(percentField()).toHaveValue("");
	expect(percentField()).toHaveAttribute("aria-invalid", "true");
	expect(percentField()).toHaveAccessibleDescription(
		"Cannot show this value. Enter it again.",
	);
	expect(changes).toEqual([null]);
});
