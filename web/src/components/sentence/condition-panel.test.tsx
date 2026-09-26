import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactElement, useState } from "react";
import { expect, test } from "vitest";
import { AmountInput } from "@/components/inputs/amount-input";
import { PaceInput } from "@/components/inputs/pace-input";
import {
	type ConditionCatalog,
	type ConditionGroup,
	ConditionPanel,
} from "@/components/sentence/condition-panel";

type TestSignal = "pace" | "allowance_remaining";

const signalCatalog: ConditionCatalog<TestSignal> = [
	{ signal: "pace", phrase: "pace", input: PaceInput },
	{
		signal: "allowance_remaining",
		phrase: "allowance left",
		input: AmountInput,
	},
];

function renderPanel(
	initialGroup: ConditionGroup<TestSignal>,
): ConditionGroup<TestSignal>[] {
	const changes: ConditionGroup<TestSignal>[] = [];
	function PanelHarness(): ReactElement {
		const [group, setGroup] = useState(initialGroup);
		return (
			<ConditionPanel
				catalog={signalCatalog}
				value={group}
				onChange={(nextGroup) => {
					changes.push(nextGroup);
					setGroup(nextGroup);
				}}
			/>
		);
	}
	render(<PanelHarness />);
	return changes;
}

function rootGroup(): HTMLElement {
	return screen.getByRole("group", { name: "Conditions" });
}

function nestedGroups(): HTMLElement[] {
	return screen.queryAllByRole("group", { name: "Condition group" });
}

function nestedGroup(index: number): HTMLElement {
	const group = nestedGroups()[index];
	if (group === undefined) {
		throw new Error(`nested group missing index=${index}`);
	}
	return group;
}

function findOwnButton(
	group: HTMLElement,
	name: string,
): HTMLElement | undefined {
	return within(group)
		.queryAllByRole("button", { name })
		.find((button) => button.closest("fieldset") === group);
}

function getOwnButton(group: HTMLElement, name: string): HTMLElement {
	const button = findOwnButton(group, name);
	if (button === undefined) {
		throw new Error(`button missing name=${name}`);
	}
	return button;
}

test("adds, edits and removes conditions", async () => {
	const user = userEvent.setup();
	const changes = renderPanel({ all: [] });

	await user.click(screen.getByRole("button", { name: "Add condition" }));
	expect(changes.at(-1)).toEqual({
		all: [{ signal: "pace", operator: "gt", value: "" }],
	});
	expect(screen.getByRole("combobox", { name: "Signal" })).toHaveTextContent(
		"pace",
	);
	expect(
		screen.getByRole("combobox", { name: "Comparison" }),
	).toHaveTextContent("above");

	await user.type(screen.getByRole("textbox", { name: "Value" }), "2.5");
	expect(changes.at(-1)).toEqual({
		all: [{ signal: "pace", operator: "gt", value: "2.5000" }],
	});

	await user.click(screen.getByRole("button", { name: "Remove condition" }));
	expect(changes.at(-1)).toEqual({ all: [] });
	expect(
		screen.queryByRole("textbox", { name: "Value" }),
	).not.toBeInTheDocument();
});

test("rows show their signal, comparison and value input", () => {
	renderPanel({
		any: [
			{ signal: "allowance_remaining", operator: "lte", value: "0.000000000" },
		],
	});

	expect(screen.getByRole("combobox", { name: "Signal" })).toHaveTextContent(
		"allowance left",
	);
	expect(
		screen.getByRole("combobox", { name: "Comparison" }),
	).toHaveTextContent("at most");
	expect(screen.getByRole("textbox", { name: "Value" })).toHaveValue("0");
	expect(within(rootGroup()).getByText("$")).toBeInTheDocument();
});

test("Match switches between all and any and keeps the members", async () => {
	const user = userEvent.setup();
	const condition = {
		signal: "pace",
		operator: "gt",
		value: "2.0000",
	} as const;
	const changes = renderPanel({ all: [condition] });

	await user.click(screen.getByRole("button", { name: "all" }));
	expect(changes.at(-1)).toEqual({ any: [condition] });

	await user.click(screen.getByRole("button", { name: "any" }));
	expect(changes.at(-1)).toEqual({ all: [condition] });
});

test("adds nested groups up to depth 3 and refuses depth 4", async () => {
	const user = userEvent.setup();
	const changes = renderPanel({ all: [] });

	await user.click(getOwnButton(rootGroup(), "Add group"));
	expect(changes.at(-1)).toEqual({ all: [{ all: [] }] });

	await user.click(getOwnButton(nestedGroup(0), "Add group"));
	expect(changes.at(-1)).toEqual({ all: [{ all: [{ all: [] }] }] });

	const thirdLevel = nestedGroup(1);
	expect(findOwnButton(thirdLevel, "Add group")).toBeUndefined();
	await user.click(getOwnButton(thirdLevel, "Add condition"));
	expect(changes.at(-1)).toEqual({
		all: [{ all: [{ all: [{ signal: "pace", operator: "gt", value: "" }] }] }],
	});
});

test("removes a nested group with its members", async () => {
	const user = userEvent.setup();
	const changes = renderPanel({
		all: [
			{ signal: "pace", operator: "gt", value: "2.0000" },
			{ any: [{ signal: "pace", operator: "lt", value: "0.5000" }] },
		],
	});

	expect(findOwnButton(rootGroup(), "Remove group")).toBeUndefined();
	await user.click(getOwnButton(nestedGroup(0), "Remove group"));

	expect(changes.at(-1)).toEqual({
		all: [{ signal: "pace", operator: "gt", value: "2.0000" }],
	});
	expect(nestedGroups()).toEqual([]);
});

test("a row keeps its typed text when an earlier row is removed", async () => {
	const user = userEvent.setup();
	renderPanel({
		all: [
			{ signal: "pace", operator: "gt", value: "2.0000" },
			{ signal: "pace", operator: "lt", value: "" },
		],
	});
	const [, secondValue] = screen.getAllByRole("textbox", { name: "Value" });
	const [firstRemove] = screen.getAllByRole("button", {
		name: "Remove condition",
	});
	if (secondValue === undefined || firstRemove === undefined) {
		throw new Error("second row missing");
	}

	await user.type(secondValue, "2.55");
	await user.click(firstRemove);

	const [remainingValue, ...otherValues] = screen.getAllByRole("textbox", {
		name: "Value",
	});
	expect(otherValues).toEqual([]);
	expect(remainingValue).toHaveValue("2.55");
	expect(remainingValue).toHaveAccessibleDescription("Use at most 1 decimal");
});

test("an edit inside a nested group keeps the group mounted", async () => {
	const user = userEvent.setup();
	const changes = renderPanel({
		all: [{ any: [{ signal: "pace", operator: "gt", value: "" }] }],
	});
	const valueField = screen.getByRole("textbox", { name: "Value" });

	await user.type(valueField, "1.5");

	expect(valueField).toBeInTheDocument();
	expect(valueField).toHaveFocus();
	expect(changes.at(-1)).toEqual({
		all: [{ any: [{ signal: "pace", operator: "gt", value: "1.5000" }] }],
	});
});

test("the keyboard changes a comparison and a signal, which empties the value", async () => {
	const user = userEvent.setup();
	const changes = renderPanel({
		all: [{ signal: "pace", operator: "gt", value: "2.0000" }],
	});

	screen.getByRole("combobox", { name: "Comparison" }).focus();
	await user.keyboard("{Enter}");
	await user.click(await screen.findByRole("option", { name: "at most" }));
	expect(changes.at(-1)).toEqual({
		all: [{ signal: "pace", operator: "lte", value: "2.0000" }],
	});

	screen.getByRole("combobox", { name: "Signal" }).focus();
	await user.keyboard("{Enter}");
	await user.click(
		await screen.findByRole("option", { name: "allowance left" }),
	);
	expect(changes.at(-1)).toEqual({
		all: [{ signal: "allowance_remaining", operator: "lte", value: "" }],
	});
	expect(screen.getByRole("textbox", { name: "Value" })).toHaveValue("");
	expect(within(rootGroup()).getByText("$")).toBeInTheDocument();
});
