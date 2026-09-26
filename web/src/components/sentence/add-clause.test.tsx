import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactElement, useState } from "react";
import { expect, test, vi } from "vitest";
import { AmountInput } from "@/components/inputs/amount-input";
import { AddClause } from "@/components/sentence/add-clause";
import { Blank } from "@/components/sentence/blank";
import { formatMoney } from "@/lib/format";

function CapSentence(): ReactElement {
	const [limitAdded, setLimitAdded] = useState(false);
	const [limit, setLimit] = useState<string | null>(null);
	return (
		<p>
			Cap to 4s
			{limitAdded && (
				<>
					{" "}
					and allow at most{" "}
					<Blank
						placeholder="how much"
						phrase={limit === null ? null : formatMoney(limit)}
						defaultOpen
					>
						<AmountInput aria-label="Limit" value={limit} onChange={setLimit} />
					</Blank>{" "}
					this period
				</>
			)}{" "}
			<AddClause
				clauses={[
					...(limitAdded
						? []
						: [
								{ label: "Limit per period", onAdd: () => setLimitAdded(true) },
							]),
					{ label: "Fallback model", onAdd: () => undefined },
				]}
			/>
		</p>
	);
}

test("the menu lists the optional clauses", async () => {
	const user = userEvent.setup();
	const onAdd = vi.fn();
	render(
		<AddClause
			clauses={[
				{ label: "Limit per period", onAdd },
				{ label: "Fallback model", onAdd: vi.fn() },
			]}
		/>,
	);

	await user.click(screen.getByRole("button", { name: "Add clause" }));

	expect(
		(await screen.findAllByRole("menuitem")).map((item) => item.textContent),
	).toEqual(["Limit per period", "Fallback model"]);
	await user.click(screen.getByRole("menuitem", { name: "Limit per period" }));
	expect(onAdd).toHaveBeenCalledOnce();
});

test("adding a clause opens its first blank with focus inside", async () => {
	const user = userEvent.setup();
	render(<CapSentence />);

	await user.click(screen.getByRole("button", { name: "Add clause" }));
	await user.click(
		await screen.findByRole("menuitem", { name: "Limit per period" }),
	);

	const limitField = await screen.findByRole("textbox", { name: "Limit" });
	await waitFor(() => {
		expect(limitField).toHaveFocus();
	});
	expect(screen.queryByRole("menu")).not.toBeInTheDocument();
	expect(screen.getByRole("button", { name: "how much" })).toHaveAttribute(
		"aria-expanded",
		"true",
	);
	await user.keyboard("20");
	expect(screen.getByRole("button", { name: "$20.00" })).toBeInTheDocument();
});

test("the keyboard adds a clause and opens its first blank", async () => {
	const user = userEvent.setup();
	render(<CapSentence />);

	await user.tab();
	expect(screen.getByRole("button", { name: "Add clause" })).toHaveFocus();
	await user.keyboard("{Enter}");
	await screen.findByRole("menuitem", { name: "Limit per period" });
	await user.keyboard("{Enter}");

	const limitField = await screen.findByRole("textbox", { name: "Limit" });
	await waitFor(() => {
		expect(limitField).toHaveFocus();
	});
});

test("Escape closes the menu and returns focus to the add button", async () => {
	const user = userEvent.setup();
	render(<CapSentence />);
	const addButton = screen.getByRole("button", { name: "Add clause" });

	await user.click(addButton);
	await screen.findByRole("menu");
	await user.keyboard("{Escape}");

	await waitFor(() => {
		expect(screen.queryByRole("menu")).not.toBeInTheDocument();
	});
	expect(addButton).toHaveFocus();
});

test("adding a clause never sends focus back to the add button", async () => {
	const user = userEvent.setup();
	render(<CapSentence />);
	const addButton = screen.getByRole("button", { name: "Add clause" });
	const focusAddButton = vi.spyOn(addButton, "focus");

	await user.click(addButton);
	await user.click(
		await screen.findByRole("menuitem", { name: "Limit per period" }),
	);
	await screen.findByRole("textbox", { name: "Limit" });

	expect(focusAddButton).not.toHaveBeenCalled();
	await user.click(addButton);
	expect(
		(await screen.findAllByRole("menuitem")).map((item) => item.textContent),
	).toEqual(["Fallback model"]);
	await user.keyboard("{Escape}");
	await waitFor(() => {
		expect(screen.queryByRole("menu")).not.toBeInTheDocument();
	});
	expect(addButton).toHaveFocus();
});

test("the add button is gone when no clause is left", () => {
	render(<AddClause clauses={[]} />);

	expect(
		screen.queryByRole("button", { name: "Add clause" }),
	).not.toBeInTheDocument();
});
