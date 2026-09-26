import { useQuery } from "@tanstack/react-query";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";
import { expect, test, vi } from "vitest";
import { renderWithQueries } from "@/components/pickers/picker-test-support";
import { Blank } from "@/components/sentence/blank";
import {
	SentenceCard,
	type SentenceCardProps,
} from "@/components/sentence/sentence-card";
import { SentenceFacts } from "@/components/sentence/sentence-facts";

function sentenceCardProps(
	overrides: Partial<SentenceCardProps> = {},
): SentenceCardProps {
	return {
		sentence: (
			<>
				For{" "}
				<Blank placeholder="which plan" phrase="Creator">
					<span>Plans</span>
				</Blank>{" "}
				customers, deny the request.
			</>
		),
		facts: null,
		errors: [],
		name: { value: "Stop losses", onChange: vi.fn(), invalid: false },
		advanced: {
			summary: "Soft, allow if unreachable or unpriced",
			content: <p>Enforcement</p>,
		},
		action: { kind: "create", label: "Create policy" },
		pending: false,
		onSubmit: vi.fn(),
		...overrides,
	};
}

function LoadingFacts(): ReactElement {
	const matchingQuery = useQuery({
		queryKey: ["matching customers"],
		queryFn: () => new Promise<string>(() => {}),
	});
	return (
		<SentenceFacts
			facts={[{ label: "Matching customers", query: matchingQuery }]}
		/>
	);
}

test("the sentence renders with its blanks", () => {
	renderWithQueries(<SentenceCard {...sentenceCardProps()} />);

	const sentence = screen.getByRole("paragraph");
	expect(sentence).toHaveTextContent(
		"For Creator customers, deny the request.",
	);
	expect(
		within(sentence).getByRole("button", { name: "Creator" }),
	).toBeInTheDocument();
});

test("a create card submits with its action", async () => {
	const user = userEvent.setup();
	const onSubmit = vi.fn();
	renderWithQueries(<SentenceCard {...sentenceCardProps({ onSubmit })} />);

	await user.click(screen.getByRole("button", { name: "Create policy" }));
	expect(onSubmit).toHaveBeenCalledOnce();

	await user.type(screen.getByRole("textbox", { name: "Name" }), "{Enter}");
	expect(onSubmit).toHaveBeenCalledTimes(2);
});

test("opening a blank never submits", async () => {
	const user = userEvent.setup();
	const onSubmit = vi.fn();
	renderWithQueries(<SentenceCard {...sentenceCardProps({ onSubmit })} />);

	await user.click(screen.getByRole("button", { name: "Creator" }));

	expect(await screen.findByRole("dialog")).toHaveTextContent("Plans");
	expect(onSubmit).not.toHaveBeenCalled();
});

test("an edit card shows Save and Discard only when dirty", async () => {
	const user = userEvent.setup();
	const onSubmit = vi.fn();
	const onDiscard = vi.fn();
	const { rerender } = renderWithQueries(
		<SentenceCard
			{...sentenceCardProps({
				action: { kind: "edit", dirty: false, onDiscard },
				onSubmit,
			})}
		/>,
	);

	expect(
		screen.queryByRole("button", { name: "Save" }),
	).not.toBeInTheDocument();
	expect(
		screen.queryByRole("button", { name: "Discard" }),
	).not.toBeInTheDocument();

	rerender(
		<SentenceCard
			{...sentenceCardProps({
				action: { kind: "edit", dirty: true, onDiscard },
				onSubmit,
			})}
		/>,
	);
	await user.click(screen.getByRole("button", { name: "Discard" }));
	expect(onDiscard).toHaveBeenCalledOnce();
	expect(onSubmit).not.toHaveBeenCalled();
	await user.click(screen.getByRole("button", { name: "Save" }));
	expect(onSubmit).toHaveBeenCalledOnce();
});

test("the primary action is disabled while saving", () => {
	renderWithQueries(<SentenceCard {...sentenceCardProps({ pending: true })} />);

	expect(screen.getByRole("button", { name: "Create policy" })).toBeDisabled();
});

test("the primary action stays enabled while facts load", async () => {
	const user = userEvent.setup();
	const onSubmit = vi.fn();
	renderWithQueries(
		<SentenceCard
			{...sentenceCardProps({ facts: <LoadingFacts />, onSubmit })}
		/>,
	);

	expect(screen.getByRole("definition")).toHaveAttribute("aria-busy", "true");
	const createButton = screen.getByRole("button", { name: "Create policy" });
	expect(createButton).toBeEnabled();
	await user.click(createButton);
	expect(onSubmit).toHaveBeenCalledOnce();
});

test("errors show once each in one list", () => {
	renderWithQueries(
		<SentenceCard
			{...sentenceCardProps({
				errors: [
					"expected an active plan",
					"action.route_chain: expected 1 to 5 route targets",
					"expected an active plan",
				],
			})}
		/>,
	);

	expect(
		within(screen.getByRole("alert"))
			.getAllByRole("listitem")
			.map((item) => item.textContent),
	).toEqual([
		"expected an active plan",
		"action.route_chain: expected 1 to 5 route targets",
	]);
});

test("no error list shows without errors", () => {
	renderWithQueries(<SentenceCard {...sentenceCardProps()} />);

	expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

test("the name input reports each change and can be marked invalid", async () => {
	const user = userEvent.setup();
	const onNameChange = vi.fn();
	renderWithQueries(
		<SentenceCard
			{...sentenceCardProps({
				name: { value: "", onChange: onNameChange, invalid: true },
			})}
		/>,
	);
	const nameField = screen.getByRole("textbox", { name: "Name" });

	expect(nameField).toHaveAttribute("aria-invalid", "true");
	await user.type(nameField, "S");
	expect(onNameChange).toHaveBeenCalledExactlyOnceWith("S");
});

test("a card without a name input shows none", () => {
	renderWithQueries(
		<SentenceCard {...sentenceCardProps({ name: undefined })} />,
	);

	expect(
		screen.queryByRole("textbox", { name: "Name" }),
	).not.toBeInTheDocument();
});

test("the status switch reports the new status", async () => {
	const user = userEvent.setup();
	const onActiveChange = vi.fn();
	renderWithQueries(
		<SentenceCard
			{...sentenceCardProps({ status: { active: true, onActiveChange } })}
		/>,
	);
	const statusSwitch = screen.getByRole("switch", { name: "Active" });

	expect(statusSwitch).toBeChecked();
	await user.click(statusSwitch);
	expect(onActiveChange).toHaveBeenCalledExactlyOnceWith(false);
});

test("no status switch shows without a status", () => {
	renderWithQueries(<SentenceCard {...sentenceCardProps()} />);

	expect(screen.queryByRole("switch")).not.toBeInTheDocument();
});

test("Advanced shows its summary and opens its settings", async () => {
	const user = userEvent.setup();
	renderWithQueries(<SentenceCard {...sentenceCardProps()} />);
	const advancedButton = screen.getByRole("button", { name: /^Advanced/ });

	expect(advancedButton).toHaveTextContent(
		"Soft, allow if unreachable or unpriced",
	);
	await user.click(advancedButton);

	expect(
		await screen.findByRole("dialog", { name: "Advanced" }),
	).toHaveTextContent("Enforcement");
});

test("Enter in the name input submits nothing while an edit is clean", async () => {
	const user = userEvent.setup();
	const onSubmit = vi.fn();
	renderWithQueries(
		<SentenceCard
			{...sentenceCardProps({
				action: { kind: "edit", dirty: false, onDiscard: vi.fn() },
				onSubmit,
			})}
		/>,
	);

	await user.type(screen.getByRole("textbox", { name: "Name" }), "{Enter}");

	expect(onSubmit).not.toHaveBeenCalled();
});
