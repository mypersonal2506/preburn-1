import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import { MeterLabel } from "@/components/meter-label";
import { TooltipProvider } from "@/components/ui/tooltip";

test("shows the meter name with the raw key in a tooltip", async () => {
	const user = userEvent.setup();
	render(<MeterLabel meter="gpu_seconds" />, { wrapper: TooltipProvider });

	await user.hover(screen.getByText("GPU seconds"));

	expect(await screen.findByRole("tooltip")).toHaveTextContent("gpu_seconds");
});
