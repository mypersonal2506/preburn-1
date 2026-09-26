import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactElement, useState } from "react";
import { expect, test } from "vitest";
import { DurationInput } from "@/components/inputs/duration-input";

function renderDurationInput(initialSeconds: number | null): (number | null)[] {
	const changes: (number | null)[] = [];
	function DurationHarness(): ReactElement {
		const [seconds, setSeconds] = useState(initialSeconds);
		return (
			<DurationInput
				aria-label="Hold time"
				value={seconds}
				onChange={(nextSeconds) => {
					changes.push(nextSeconds);
					setSeconds(nextSeconds);
				}}
			/>
		);
	}
	render(<DurationHarness />);
	return changes;
}

function secondsField(): HTMLElement {
	return screen.getByRole("textbox", { name: "Hold time" });
}

test("offers presets from 30 seconds to 1 hour", () => {
	renderDurationInput(null);

	expect(
		screen.getAllByRole("button").map((preset) => preset.textContent),
	).toEqual(["30 sec", "1 min", "5 min", "10 min", "30 min", "1 hour"]);
	expect(screen.getByText("seconds")).toBeInTheDocument();
});

test("a preset sets its seconds and shows as pressed", async () => {
	const user = userEvent.setup();
	const changes = renderDurationInput(null);

	await user.click(screen.getByRole("button", { name: "10 min" }));

	expect(changes.at(-1)).toBe(600);
	expect(secondsField()).toHaveValue("600");
	expect(screen.getByRole("button", { name: "10 min" })).toHaveAttribute(
		"aria-pressed",
		"true",
	);
	expect(screen.getByRole("button", { name: "1 min" })).toHaveAttribute(
		"aria-pressed",
		"false",
	);
});

test("a value matching a preset shows that preset as pressed", () => {
	renderDurationInput(3600);

	expect(secondsField()).toHaveValue("3600");
	expect(screen.getByRole("button", { name: "1 hour" })).toHaveAttribute(
		"aria-pressed",
		"true",
	);
});

test("custom seconds convert to a whole number", async () => {
	const user = userEvent.setup();
	const changes = renderDurationInput(null);

	await user.type(secondsField(), "45");

	expect(changes.at(-1)).toBe(45);
	for (const preset of screen.getAllByRole("button")) {
		expect(preset).toHaveAttribute("aria-pressed", "false");
	}
});

test.each([
	["1.5", "Enter a whole number"],
	["ten", "Enter a number"],
	["1234567890", "Number is too large"],
])("rejects %s inline with %s", async (text, message) => {
	const user = userEvent.setup();
	const changes = renderDurationInput(null);

	await user.type(secondsField(), text);

	expect(changes.at(-1)).toBeNull();
	expect(secondsField()).toHaveAttribute("aria-invalid", "true");
	expect(secondsField()).toHaveAccessibleDescription(message);
});
