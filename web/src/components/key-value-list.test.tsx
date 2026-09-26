import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { KeyValueList } from "@/components/key-value-list";

test("hides items without a value and keeps zero", () => {
	render(
		<KeyValueList
			items={[
				{ label: "Plan", value: "Creator" },
				{ label: "Display name", value: null },
				{ label: "Stripe customer", value: undefined },
				{ label: "Note", value: "" },
				{ label: "Matched policy", value: false },
				{ label: "Decisions", value: 0 },
			]}
		/>,
	);

	expect(screen.getByText("Plan")).toBeInTheDocument();
	expect(screen.getByText("Creator")).toBeInTheDocument();
	expect(screen.getByText("Decisions")).toBeInTheDocument();
	expect(screen.getByText("0")).toBeInTheDocument();
	for (const hiddenLabel of [
		"Display name",
		"Stripe customer",
		"Note",
		"Matched policy",
	]) {
		expect(screen.queryByText(hiddenLabel)).toBeNull();
	}
});
