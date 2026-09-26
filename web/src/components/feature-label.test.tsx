import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import { FeatureLabel } from "@/components/feature-label";
import { TooltipProvider } from "@/components/ui/tooltip";

test("shows the humanized feature with the raw key in a tooltip", async () => {
	const user = userEvent.setup();
	render(<FeatureLabel feature="text_to_video" />, {
		wrapper: TooltipProvider,
	});

	await user.hover(screen.getByText("text to video"));

	expect(await screen.findByRole("tooltip")).toHaveTextContent("text_to_video");
});
