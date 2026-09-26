import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { MarginPill } from "@/components/margin-pill";

test.each([
	["-0.1200", "0.4000", "-12.0%", "negative"],
	["0.2500", "0.4000", "25.0%", "below_target"],
	["0.4000", "0.4000", "40.0%", "at_target"],
	["0.5500", "0.4000", "55.0%", "at_target"],
	["0.1000", null, "10.0%", "at_target"],
	["-0.0500", null, "-5.0%", "negative"],
	["0.0000", "0.0000", "0.0%", "at_target"],
] as const)(
	"margin %s against target %s reads %s with the %s tone",
	(margin, targetMargin, text, tone) => {
		render(<MarginPill margin={margin} targetMargin={targetMargin} />);

		expect(screen.getByText(text)).toHaveAttribute("data-tone", tone);
	},
);

test.each([null, "-inf"])(
	"a margin of %s, cost without revenue, reads No revenue",
	(margin) => {
		render(<MarginPill margin={margin} targetMargin="0.4000" />);

		expect(screen.getByText("No revenue")).not.toHaveAttribute("data-tone");
	},
);
