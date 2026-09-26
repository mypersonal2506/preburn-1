import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import { ModelLabel } from "@/components/model-label";
import { TooltipProvider } from "@/components/ui/tooltip";

test.each([
	["Veo 3.1 Fast", "Veo 3.1 Fast"],
	[null, "veo3.1-fast"],
])(
	"with display name %s shows %s and the provider and model in a tooltip",
	async (displayName, text) => {
		const user = userEvent.setup();
		render(
			<ModelLabel
				provider="fal_ai"
				model="veo3.1-fast"
				displayName={displayName}
			/>,
			{ wrapper: TooltipProvider },
		);

		await user.hover(screen.getByText(text));

		expect(await screen.findByRole("tooltip")).toHaveTextContent(
			"fal_ai/veo3.1-fast",
		);
	},
);
