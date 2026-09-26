import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { OutcomeBadge } from "@/components/outcome-badge";
import { outcomeStyles } from "@/lib/outcomes";

test.each([
	["allow", "Allowed", "secondary"],
	["route", "Routed", "outline"],
	["cap", "Capped", "default"],
	["deny", "Denied", "destructive"],
] as const)(
	"outcome %s reads %s on the %s badge",
	(outcome, label, variant) => {
		render(<OutcomeBadge outcome={outcome} />);

		expect(screen.getByText(label)).toHaveAttribute("data-variant", variant);
	},
);

test.each([
	["allow", "Allow"],
	["route", "Route"],
	["cap", "Cap"],
	["deny", "Deny"],
] as const)("a policy outcome %s reads as the action %s", (outcome, label) => {
	render(<OutcomeBadge outcome={outcome} kind="policy" />);

	expect(screen.getByText(label)).toHaveAttribute(
		"data-variant",
		outcomeStyles[outcome].badgeVariant,
	);
});
