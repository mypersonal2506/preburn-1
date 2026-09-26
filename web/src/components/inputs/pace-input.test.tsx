import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactElement, useState } from "react";
import { expect, test } from "vitest";
import { PaceInput } from "@/components/inputs/pace-input";

function renderPaceInput(initialRatio: string | null): (string | null)[] {
	const changes: (string | null)[] = [];
	function PaceHarness(): ReactElement {
		const [ratio, setRatio] = useState(initialRatio);
		return (
			<PaceInput
				aria-label="Pace"
				value={ratio}
				onChange={(nextRatio) => {
					changes.push(nextRatio);
					setRatio(nextRatio);
				}}
			/>
		);
	}
	render(<PaceHarness />);
	return changes;
}

function paceField(): HTMLElement {
	return screen.getByRole("textbox", { name: "Pace" });
}

test("shows the API ratio as a pace with an x", () => {
	renderPaceInput("2.5000");

	expect(paceField()).toHaveValue("2.5");
	expect(screen.getByText("x")).toBeInTheDocument();
});

test.each([
	["2", "2.0000"],
	["1.5", "1.5000"],
	["0.1", "0.1000"],
])("converts the pace %s to the ratio %s", async (text, ratio) => {
	const user = userEvent.setup();
	const changes = renderPaceInput(null);

	await user.type(paceField(), text);

	expect(changes.at(-1)).toBe(ratio);
});

test.each([
	["2.55", "Use at most 1 decimal"],
	["fast", "Enter a number"],
	["1234567890", "Number is too large"],
])("rejects %s inline with %s", async (text, message) => {
	const user = userEvent.setup();
	const changes = renderPaceInput(null);

	await user.type(paceField(), text);

	expect(changes.at(-1)).toBeNull();
	expect(paceField()).toHaveAttribute("aria-invalid", "true");
	expect(paceField()).toHaveAccessibleDescription(message);
});
