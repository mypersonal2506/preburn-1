import { useQuery } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactElement, useState } from "react";
import { expect, test, vi } from "vitest";
import { AmountInput } from "@/components/inputs/amount-input";
import { renderWithQueries } from "@/components/pickers/picker-test-support";
import { SentenceFacts } from "@/components/sentence/sentence-facts";
import { useFactsDraft } from "@/components/sentence/use-facts-draft";

function QuoteHarness({
	loadQuote,
}: {
	loadQuote: (amount: string | null) => Promise<string>;
}): ReactElement {
	const [amount, setAmount] = useState<string | null>("1.000000000");
	const factsDraft = useFactsDraft(amount);
	const quoteQuery = useQuery({
		queryKey: ["quote", factsDraft],
		queryFn: () => loadQuote(factsDraft),
	});
	return (
		<>
			<AmountInput aria-label="Amount" value={amount} onChange={setAmount} />
			<SentenceFacts
				facts={[{ label: "Cost per request", query: quoteQuery }]}
			/>
		</>
	);
}

test("each fact shows its value, loading, failure or absence", () => {
	renderWithQueries(
		<SentenceFacts
			facts={[
				{
					label: "Matching customers",
					query: { status: "success", fetchStatus: "idle", data: "12" },
				},
				{
					label: "Cost per request",
					query: {
						status: "pending",
						fetchStatus: "fetching",
						data: undefined,
					},
				},
				{
					label: "Other policies",
					query: { status: "error", fetchStatus: "idle", data: undefined },
				},
				{
					label: "Route saving",
					query: { status: "pending", fetchStatus: "idle", data: undefined },
				},
			]}
		/>,
	);

	expect(screen.getAllByRole("term").map((term) => term.textContent)).toEqual([
		"Matching customers",
		"Cost per request",
		"Other policies",
		"Route saving",
	]);
	const [matching, cost, otherPolicies, routeSaving] =
		screen.getAllByRole("definition");
	expect(matching).toHaveTextContent("12");
	expect(matching).toHaveAttribute("aria-busy", "false");
	expect(cost).toHaveAttribute("aria-busy", "true");
	expect(otherPolicies).toHaveTextContent("Unavailable");
	expect(routeSaving).toHaveTextContent("-");
});

test("a fact keeps its value while it refreshes", () => {
	renderWithQueries(
		<SentenceFacts
			facts={[
				{
					label: "Matching customers",
					query: { status: "success", fetchStatus: "fetching", data: "12" },
				},
			]}
		/>,
	);

	const matching = screen.getByRole("definition");
	expect(matching).toHaveTextContent("12");
	expect(matching).toHaveAttribute("aria-busy", "true");
});

test("facts load 400 ms after the draft stops changing", async () => {
	const user = userEvent.setup();
	const loadQuote = vi.fn(async (amount: string | null) => `quote ${amount}`);
	renderWithQueries(<QuoteHarness loadQuote={loadQuote} />);

	expect(await screen.findByText("quote 1.000000000")).toBeInTheDocument();
	const typingStarted = performance.now();
	await user.clear(screen.getByRole("textbox", { name: "Amount" }));
	await user.type(screen.getByRole("textbox", { name: "Amount" }), "12.5");
	const typingSeconds = (performance.now() - typingStarted) / 1000;

	expect(typingSeconds).toBeLessThan(0.4);
	expect(loadQuote).toHaveBeenCalledOnce();
	expect(await screen.findByText("quote 12.500000000")).toBeInTheDocument();
	expect(loadQuote.mock.calls).toEqual([["1.000000000"], ["12.500000000"]]);
});

test("the first draft loads at once", async () => {
	const loadQuote = vi.fn(async (amount: string | null) => `quote ${amount}`);
	renderWithQueries(<QuoteHarness loadQuote={loadQuote} />);

	await waitFor(
		() => {
			expect(loadQuote).toHaveBeenCalledExactlyOnceWith("1.000000000");
		},
		{ timeout: 200 },
	);
});
