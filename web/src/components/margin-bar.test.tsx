import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { MarginBar } from "@/components/margin-bar";

test.each([
	["0.2000", "0.4000", "50", "below_target"],
	["0.1000", "0.4000", "25", "below_target"],
	["0.4000", "0.4000", "100", "at_target"],
	["0.7000", "0.4000", "100", "at_target"],
	["0.3000", null, "100", "at_target"],
	["0.1000", "0.0000", "100", "at_target"],
	["-0.2500", "0.4000", "0", "negative"],
] as const)(
	"margin %s against target %s fills %s percent with the %s tone",
	(margin, targetMargin, filledPercent, tone) => {
		render(<MarginBar margin={margin} targetMargin={targetMargin} />);

		const bar = screen.getByRole("progressbar", {
			name: "Margin against target",
		});
		expect(bar).toHaveAttribute("aria-valuenow", filledPercent);
		expect(bar).toHaveAttribute("data-tone", tone);
	},
);

test("a customer without revenue has no bar", () => {
	render(<MarginBar margin={null} targetMargin="0.4000" />);

	expect(screen.queryByRole("progressbar")).toBeNull();
});
