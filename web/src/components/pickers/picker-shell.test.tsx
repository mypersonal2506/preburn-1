import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";
import {
	type PickerGroup,
	PickerShell,
	type PickerShellProps,
} from "@/components/pickers/picker-shell";
import { ApiProblem } from "@/lib/api-problem";

const planGroups: PickerGroup<string>[] = [
	{
		options: [
			{
				value: "pln_creator",
				label: "Creator",
				detail: null,
				choice: "creator",
			},
			{ value: "pln_free", label: "Free", detail: null, choice: "free" },
			{ value: "pln_studio", label: "Studio", detail: null, choice: "studio" },
		],
	},
];

function renderShell(props: Partial<PickerShellProps<string>> = {}): void {
	render(
		<PickerShell
			placeholder="Select a plan"
			searchPlaceholder="Search plans"
			emptyText="No plans found"
			selectedValue={null}
			selectedLabel={null}
			groups={planGroups}
			loading={false}
			error={null}
			onSelect={vi.fn()}
			{...props}
		/>,
	);
}

test("the keyboard opens the list, arrows move, Enter selects and focus returns", async () => {
	const user = userEvent.setup();
	const onSelect = vi.fn();
	renderShell({ onSelect });
	const trigger = screen.getByRole("button", { name: "Select a plan" });

	await user.tab();
	expect(trigger).toHaveFocus();
	await user.keyboard("{Enter}");
	expect(await screen.findByPlaceholderText("Search plans")).toHaveFocus();
	expect(screen.getByRole("option", { name: "Creator" })).toHaveAttribute(
		"aria-selected",
		"true",
	);

	await user.keyboard("{ArrowDown}");
	expect(screen.getByRole("option", { name: "Free" })).toHaveAttribute(
		"aria-selected",
		"true",
	);
	await user.keyboard("{Enter}");

	expect(onSelect).toHaveBeenCalledExactlyOnceWith("free");
	await waitFor(() => {
		expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
	});
	expect(trigger).toHaveFocus();
});

test("Escape closes the list without selecting and returns focus", async () => {
	const user = userEvent.setup();
	const onSelect = vi.fn();
	renderShell({ onSelect });
	const trigger = screen.getByRole("button", { name: "Select a plan" });

	await user.click(trigger);
	await screen.findByRole("listbox");
	await user.keyboard("{Escape}");

	await waitFor(() => {
		expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
	});
	expect(trigger).toHaveFocus();
	expect(onSelect).not.toHaveBeenCalled();
});

test("typing filters the options and shows the empty state when none match", async () => {
	const user = userEvent.setup();
	renderShell();

	await user.click(screen.getByRole("button", { name: "Select a plan" }));
	await user.keyboard("stu");

	expect(screen.getByRole("option", { name: "Studio" })).toBeInTheDocument();
	expect(
		screen.queryByRole("option", { name: "Creator" }),
	).not.toBeInTheDocument();

	await user.keyboard("x");
	expect(screen.getByText("No plans found")).toBeInTheDocument();
});

test("the trigger shows the selected label and the list marks the selected option", async () => {
	const user = userEvent.setup();
	renderShell({ selectedValue: "pln_free", selectedLabel: "Free" });

	await user.click(screen.getByRole("button", { name: "Free" }));

	expect(screen.getByRole("option", { name: "Free" })).toHaveAttribute(
		"data-checked",
		"true",
	);
	expect(screen.getByRole("option", { name: "Creator" })).toHaveAttribute(
		"data-checked",
		"false",
	);
});

test("the trigger reads Loading while the selected option loads", () => {
	renderShell({
		groups: [],
		loading: true,
		selectedValue: "pln_free",
		selectedLabel: null,
	});

	expect(screen.getByRole("button", { name: "Loading" })).toBeInTheDocument();
});

test("the list shows a loading state instead of the empty state", async () => {
	const user = userEvent.setup();
	renderShell({ groups: [], loading: true });

	await user.click(screen.getByRole("button", { name: "Select a plan" }));

	expect(await screen.findByRole("status")).toHaveTextContent("Loading");
	expect(screen.queryByText("No plans found")).not.toBeInTheDocument();
});

test("the list shows the problem detail when options fail to load", async () => {
	const user = userEvent.setup();
	renderShell({
		groups: [],
		error: new ApiProblem({
			type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#database_unavailable",
			title: "Service Unavailable",
			status: 503,
			code: "database_unavailable",
			detail: "database unavailable",
			errors: [],
			retryAfterSeconds: null,
		}),
	});

	await user.click(screen.getByRole("button", { name: "Select a plan" }));

	expect(await screen.findByRole("alert")).toHaveTextContent(
		"database unavailable",
	);
	expect(screen.queryByText("No plans found")).not.toBeInTheDocument();
});

test("groups show their headings and options show their detail", async () => {
	const user = userEvent.setup();
	renderShell({
		groups: [
			{
				heading: "fal_ai",
				options: [
					{
						value: "fal_ai/fal-ai/veo3.1/fast",
						label: "Veo 3.1 Fast",
						detail: "$0.15 per second",
						choice: "veo",
					},
				],
			},
		],
	});

	await user.click(screen.getByRole("button", { name: "Select a plan" }));

	const group = screen.getByRole("group", { name: "fal_ai" });
	expect(group).toHaveTextContent("Veo 3.1 Fast");
	expect(group).toHaveTextContent("$0.15 per second");
});

test("search reports the typed text after a pause and keeps unmatched options", async () => {
	const user = userEvent.setup();
	const onSearchChange = vi.fn();
	renderShell({ onSearchChange });

	await user.click(screen.getByRole("button", { name: "Select a plan" }));
	await user.keyboard("zzz");

	await waitFor(() => {
		expect(onSearchChange).toHaveBeenLastCalledWith("zzz");
	});
	expect(onSearchChange).not.toHaveBeenCalledWith("z");
	expect(screen.getByRole("option", { name: "Creator" })).toBeInTheDocument();
});

test("closing the list clears the search", async () => {
	const user = userEvent.setup();
	const onSearchChange = vi.fn();
	renderShell({ onSearchChange });
	const trigger = screen.getByRole("button", { name: "Select a plan" });

	await user.click(trigger);
	await user.keyboard("zzz");
	await waitFor(() => {
		expect(onSearchChange).toHaveBeenLastCalledWith("zzz");
	});
	await user.keyboard("{Escape}");

	await waitFor(() => {
		expect(onSearchChange).toHaveBeenLastCalledWith("");
	});
	await user.click(trigger);
	expect(await screen.findByPlaceholderText("Search plans")).toHaveValue("");
});

test("Load more loads the next page and keeps the list open", async () => {
	const user = userEvent.setup();
	const load = vi.fn();
	const onSelect = vi.fn();
	renderShell({ onSelect, nextPage: { loading: false, load } });

	await user.click(screen.getByRole("button", { name: "Select a plan" }));
	await user.click(screen.getByRole("option", { name: "Load more" }));

	expect(load).toHaveBeenCalledOnce();
	expect(onSelect).not.toHaveBeenCalled();
	expect(screen.getByRole("listbox")).toBeInTheDocument();
});

test("Load more is disabled while the next page loads", async () => {
	const user = userEvent.setup();
	renderShell({ nextPage: { loading: true, load: vi.fn() } });

	await user.click(screen.getByRole("button", { name: "Select a plan" }));

	expect(screen.getByRole("option", { name: "Loading" })).toHaveAttribute(
		"aria-disabled",
		"true",
	);
});

test("an invalid picker marks its trigger invalid", () => {
	renderShell({ invalid: true });

	expect(screen.getByRole("button", { name: "Select a plan" })).toHaveAttribute(
		"aria-invalid",
		"true",
	);
});

test("a trigger replaces the button and gets the selected label", async () => {
	const user = userEvent.setup();
	renderShell({
		selectedValue: "pln_free",
		selectedLabel: "Free",
		trigger: (label) => (
			<button type="button" data-testid="plan blank">
				{label ?? "which plan"}
			</button>
		),
	});
	const trigger = screen.getByTestId("plan blank");

	expect(screen.getAllByRole("button")).toEqual([trigger]);
	expect(trigger).toHaveAccessibleName("Free");
	expect(trigger).toHaveAttribute("aria-haspopup", "dialog");
	expect(trigger).toHaveAttribute("aria-expanded", "false");
	await user.click(trigger);

	expect(trigger).toHaveAttribute("aria-expanded", "true");
	expect(screen.getByRole("option", { name: "Free" })).toHaveAttribute(
		"data-checked",
		"true",
	);
});

test("a trigger gets null without a selection and Loading while it loads", () => {
	const labels: (string | null)[] = [];
	function recordLabel(label: string | null) {
		labels.push(label);
		return <button type="button">{label ?? "which plan"}</button>;
	}
	renderShell({ trigger: recordLabel });
	renderShell({
		groups: [],
		loading: true,
		selectedValue: "pln_free",
		selectedLabel: null,
		trigger: recordLabel,
	});

	expect(labels).toEqual([null, "Loading"]);
});

test("a trigger gets the id and the invalid state", () => {
	renderShell({
		id: "plan",
		invalid: true,
		trigger: (label) => <button type="button">{label ?? "which plan"}</button>,
	});

	const trigger = screen.getByRole("button", { name: "which plan" });
	expect(trigger).toHaveAttribute("id", "plan");
	expect(trigger).toHaveAttribute("aria-invalid", "true");
});

test("an invalid message marks the trigger invalid and shows in the list", async () => {
	const user = userEvent.setup();
	renderShell({ invalidMessage: "expected an active plan" });
	const trigger = screen.getByRole("button", { name: "Select a plan" });

	expect(trigger).toHaveAttribute("aria-invalid", "true");
	await user.click(trigger);

	expect(await screen.findByRole("dialog")).toHaveAccessibleDescription(
		"expected an active plan",
	);
});

test("defaultOpen opens the list on mount with the search focused", async () => {
	renderShell({ defaultOpen: true });

	expect(await screen.findByPlaceholderText("Search plans")).toHaveFocus();
});

test("a search offer comes after the matching options", async () => {
	const user = userEvent.setup();
	const onSelect = vi.fn();
	renderShell({
		onSelect,
		searchOffer: (search) => ({
			kind: "option",
			option: {
				value: search,
				label: `New plan ${search}`,
				detail: null,
				choice: search,
			},
		}),
	});

	await user.click(screen.getByRole("button", { name: "Select a plan" }));
	await user.keyboard("fre");

	expect(
		screen.getAllByRole("option").map((option) => option.textContent),
	).toEqual(["Free", "New plan fre"]);
	expect(screen.getByRole("option", { name: "Free" })).toHaveAttribute(
		"aria-selected",
		"true",
	);
	await user.click(screen.getByRole("option", { name: "New plan fre" }));
	expect(onSelect).toHaveBeenCalledExactlyOnceWith("fre");
});

test("a search offer keeps the empty state away when nothing else matches", async () => {
	const user = userEvent.setup();
	renderShell({
		searchOffer: (search) => ({
			kind: "option",
			option: { value: search, label: search, detail: null, choice: search },
		}),
	});

	await user.click(screen.getByRole("button", { name: "Select a plan" }));
	await user.keyboard("enterprise");

	expect(screen.getByRole("option", { name: "enterprise" })).toHaveAttribute(
		"aria-selected",
		"true",
	);
	expect(screen.queryByText("No plans found")).not.toBeInTheDocument();
});

test("a search hint shows why the text cannot be used", async () => {
	const user = userEvent.setup();
	renderShell({
		searchOffer: () => ({ kind: "hint", hint: "Use lowercase letters" }),
	});

	await user.click(screen.getByRole("button", { name: "Select a plan" }));
	await user.keyboard("X");

	expect(screen.getByText("Use lowercase letters")).toBeInTheDocument();
	expect(screen.queryByRole("option")).not.toBeInTheDocument();
});

test("no search offer is asked for while the search is empty", async () => {
	const user = userEvent.setup();
	const searchOffer = vi.fn(() => null);
	renderShell({ searchOffer });

	await user.click(screen.getByRole("button", { name: "Select a plan" }));

	expect(screen.getAllByRole("option")).toHaveLength(3);
	expect(searchOffer).not.toHaveBeenCalled();
});
