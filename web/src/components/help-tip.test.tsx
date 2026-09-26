import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import { HelpTip } from "@/components/help-tip";
import { TooltipProvider } from "@/components/ui/tooltip";

test("explains its topic on focus", async () => {
	const user = userEvent.setup();
	render(<HelpTip topic="pace">Spend against the allowance so far.</HelpTip>, {
		wrapper: TooltipProvider,
	});

	await user.tab();

	expect(screen.getByRole("button", { name: "About pace" })).toHaveFocus();
	expect(await screen.findByRole("tooltip")).toHaveTextContent(
		"Spend against the allowance so far.",
	);
});
