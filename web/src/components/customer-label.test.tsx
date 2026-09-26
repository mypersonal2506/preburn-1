import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import { CustomerLabel } from "@/components/customer-label";
import { TooltipProvider } from "@/components/ui/tooltip";

test("shows the display name with the external id in a tooltip", async () => {
	const user = userEvent.setup();
	render(<CustomerLabel displayName="Acme" externalId="customer-42" />, {
		wrapper: TooltipProvider,
	});

	await user.hover(screen.getByText("Acme"));

	expect(await screen.findByRole("tooltip")).toHaveTextContent("customer-42");
});

test("shows the external id without a tooltip when there is no display name", async () => {
	const user = userEvent.setup();
	render(<CustomerLabel displayName={null} externalId="customer-42" />, {
		wrapper: TooltipProvider,
	});

	await user.hover(screen.getByText("customer-42"));

	expect(screen.queryByRole("tooltip")).toBeNull();
});
