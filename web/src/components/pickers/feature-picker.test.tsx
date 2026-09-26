import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { PageBodyKnownFeatureResponse } from "@/client";
import { FeaturePicker } from "@/components/pickers/feature-picker";
import {
	jsonResponse,
	problemResponse,
	renderWithQueries,
	stubApi,
} from "@/components/pickers/picker-test-support";

const knownFeatures: PageBodyKnownFeatureResponse = {
	items: [
		{ feature: "chat", sources: ["decisions"] },
		{ feature: "text_to_video", sources: ["decisions", "policies"] },
	],
	next_cursor: null,
};

afterEach(() => {
	vi.unstubAllGlobals();
});

test("lists the known features by label and finds them by key", async () => {
	const user = userEvent.setup();
	const requestedUrls = stubApi(() => jsonResponse(knownFeatures));
	renderWithQueries(<FeaturePicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a feature" }));
	expect(
		await screen.findByRole("option", { name: "text to video" }),
	).toBeInTheDocument();
	await user.keyboard("text_to");

	expect(
		screen.getByRole("option", { name: "text to video" }),
	).toBeInTheDocument();
	expect(
		screen.queryByRole("option", { name: "chat" }),
	).not.toBeInTheDocument();
	expect(requestedUrls[0]?.pathname).toBe("/api/v1/dashboard/features");
});

test("selecting a feature hands back its key", async () => {
	const user = userEvent.setup();
	const onChange = vi.fn();
	stubApi(() => jsonResponse(knownFeatures));
	renderWithQueries(<FeaturePicker value={null} onChange={onChange} />);

	await user.click(screen.getByRole("button", { name: "Select a feature" }));
	await user.click(
		await screen.findByRole("option", { name: "text to video" }),
	);

	expect(onChange).toHaveBeenCalledExactlyOnceWith("text_to_video");
});

test("the trigger shows the label of the selected feature", () => {
	stubApi(() => jsonResponse(knownFeatures));
	renderWithQueries(
		<FeaturePicker value="image_to_video" onChange={vi.fn()} />,
	);

	expect(
		screen.getByRole("button", { name: "image to video" }),
	).toBeInTheDocument();
});

test("a failed load shows the problem detail", async () => {
	const user = userEvent.setup();
	stubApi(() =>
		problemResponse({
			type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#database_unavailable",
			title: "Service Unavailable",
			status: 503,
			code: "database_unavailable",
			detail: "database unavailable",
		}),
	);
	renderWithQueries(<FeaturePicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a feature" }));

	expect(await screen.findByRole("alert")).toHaveTextContent(
		"database unavailable",
	);
});

test("a new feature key is offered after the known matches and handed back", async () => {
	const user = userEvent.setup();
	const onChange = vi.fn();
	stubApi(() => jsonResponse(knownFeatures));
	renderWithQueries(<FeaturePicker value={null} onChange={onChange} />);

	await user.click(screen.getByRole("button", { name: "Select a feature" }));
	await screen.findByRole("option", { name: "text to video" });
	await user.keyboard("text_to");

	const [knownOption, newOption, ...otherOptions] =
		screen.getAllByRole("option");
	expect(knownOption).toHaveTextContent("text to video");
	expect(knownOption).toHaveAttribute("aria-selected", "true");
	expect(newOption).toHaveTextContent("text to");
	expect(newOption).toHaveTextContent("New feature");
	expect(otherOptions).toEqual([]);
	await user.click(screen.getByRole("option", { name: /New feature/ }));

	expect(onChange).toHaveBeenCalledExactlyOnceWith("text_to");
});

test("a new feature can be picked before any feature is known", async () => {
	const user = userEvent.setup();
	const onChange = vi.fn();
	stubApi(() => jsonResponse({ items: [], next_cursor: null }));
	renderWithQueries(<FeaturePicker value={null} onChange={onChange} />);

	await user.click(screen.getByRole("button", { name: "Select a feature" }));
	await screen.findByText("No features found");
	await user.keyboard("image_to_video{Enter}");

	expect(onChange).toHaveBeenCalledExactlyOnceWith("image_to_video");
});

test("a known feature key is not offered as new", async () => {
	const user = userEvent.setup();
	stubApi(() => jsonResponse(knownFeatures));
	renderWithQueries(<FeaturePicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a feature" }));
	await screen.findByRole("option", { name: "chat" });
	await user.keyboard("chat");

	expect(screen.getAllByRole("option")).toHaveLength(1);
	expect(screen.queryByText("New feature")).not.toBeInTheDocument();
});

test.each([
	["Text_to_video", "Use a-z, 0-9 and _, letter first"],
	["2d_render", "Use a-z, 0-9 and _, letter first"],
	["text-to-video", "Use a-z, 0-9 and _, letter first"],
	[`a${"b".repeat(64)}`, "Use at most 64 characters"],
])("%s is refused with %s", async (search, hint) => {
	const user = userEvent.setup();
	stubApi(() => jsonResponse(knownFeatures));
	renderWithQueries(<FeaturePicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a feature" }));
	await screen.findByRole("option", { name: "chat" });
	await user.keyboard(search);

	expect(screen.getByText(hint)).toBeInTheDocument();
	expect(screen.queryByText("New feature")).not.toBeInTheDocument();
});

test("the longest feature key the server accepts is offered", async () => {
	const user = userEvent.setup();
	const longestFeature = `a${"b".repeat(63)}`;
	stubApi(() => jsonResponse(knownFeatures));
	renderWithQueries(<FeaturePicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a feature" }));
	await screen.findByRole("option", { name: "chat" });
	await user.keyboard(longestFeature);

	expect(screen.getByText("New feature")).toBeInTheDocument();
});
