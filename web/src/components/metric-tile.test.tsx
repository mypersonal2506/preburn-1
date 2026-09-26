import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { MetricTile } from "@/components/metric-tile";
import { TooltipProvider } from "@/components/ui/tooltip";

test("shows its label, value, foot and help", () => {
	render(
		<MetricTile
			label="Cost avoided"
			helpTip="Estimated at check time."
			value="$1,204.10"
			foot="By route and cap"
		/>,
		{ wrapper: TooltipProvider },
	);

	expect(screen.getByText("Cost avoided")).toBeInTheDocument();
	expect(screen.getByText("$1,204.10")).toBeInTheDocument();
	expect(screen.getByText("By route and cap")).toBeInTheDocument();
	expect(
		screen.getByRole("button", { name: "About Cost avoided" }),
	).toBeInTheDocument();
});
