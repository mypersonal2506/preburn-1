import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { StatusDot } from "@/components/status-dot";

test.each([
	["active", "Active"],
	["disabled", "Disabled"],
	["archived", "Archived"],
	["deprecated", "Deprecated"],
	["reserved", "Reserved"],
	["settled", "Settled"],
	["released", "Released"],
	["expired", "Expired"],
	["unreserved", "Unreserved"],
] as const)("status %s reads %s", (status, label) => {
	render(<StatusDot status={status} />);

	expect(screen.getByText(label)).toBeInTheDocument();
});
