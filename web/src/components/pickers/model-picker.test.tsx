import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { ModelResponse, PageBodyModelResponse } from "@/client";
import { ModelPicker } from "@/components/pickers/model-picker";
import {
	jsonResponse,
	renderWithQueries,
	stubApi,
} from "@/components/pickers/picker-test-support";

const claudeSonnet: ModelResponse = {
	provider: "anthropic",
	model: "claude-sonnet-5",
	display_name: "Claude Sonnet 5",
	status: "active",
	key_prices: [
		{
			meter: "cache_write_input_tokens",
			unit_price: "3.750000000",
			unit_quantity: 1_000_000,
		},
		{
			meter: "cached_input_tokens",
			unit_price: "0.300000000",
			unit_quantity: 1_000_000,
		},
		{
			meter: "input_tokens",
			unit_price: "3.000000000",
			unit_quantity: 1_000_000,
		},
		{
			meter: "output_tokens",
			unit_price: "15.000000000",
			unit_quantity: 1_000_000,
		},
	],
};

const veoFast: ModelResponse = {
	provider: "fal_ai",
	model: "fal-ai/veo3.1/fast",
	display_name: "Veo 3.1 Fast",
	status: "active",
	key_prices: [
		{ meter: "output_seconds", unit_price: "0.150000000", unit_quantity: 1 },
	],
};

const klingTurbo: ModelResponse = {
	provider: "fal_ai",
	model: "fal-ai/kling-video/v2.5-turbo/pro",
	display_name: "Kling 2.5 Turbo Pro",
	status: "active",
	key_prices: [
		{ meter: "output_seconds", unit_price: "0.070000000", unit_quantity: 1 },
	],
};

const gptLuna: ModelResponse = {
	provider: "openai",
	model: "gpt-6-luna",
	display_name: "GPT-6 Luna",
	status: "active",
	key_prices: [
		{
			meter: "input_tokens",
			unit_price: "0.400000000",
			unit_quantity: 1_000_000,
		},
	],
};

const retiredModel: ModelResponse = {
	provider: "openai",
	model: "gpt-3.5-turbo-0301",
	display_name: null,
	status: "deprecated",
	key_prices: [],
};

function modelPage(
	items: ModelResponse[],
	nextCursor: string | null = null,
): PageBodyModelResponse {
	return { items, next_cursor: nextCursor };
}

afterEach(() => {
	vi.unstubAllGlobals();
});

test("lists models by provider with display name, headline price and unpriced state", async () => {
	const user = userEvent.setup();
	const requestedUrls = stubApi(() =>
		jsonResponse(modelPage([claudeSonnet, veoFast, retiredModel])),
	);
	renderWithQueries(<ModelPicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a model" }));

	const anthropic = await screen.findByRole("group", { name: "anthropic" });
	expect(
		within(anthropic).getByRole("option", { name: /Claude Sonnet 5/ }),
	).toHaveTextContent("$3.00 per 1M input tokens");
	expect(
		within(screen.getByRole("group", { name: "fal_ai" })).getByRole("option", {
			name: /Veo 3.1 Fast/,
		}),
	).toHaveTextContent("$0.15 per second");
	expect(
		within(screen.getByRole("group", { name: "openai" })).getByRole("option", {
			name: /gpt-3.5-turbo-0301/,
		}),
	).toHaveTextContent("Unpriced");
	expect(requestedUrls[0]?.pathname).toBe("/api/v1/pricing/models");
	expect(requestedUrls[0]?.searchParams.get("include_deprecated")).toBe("true");
});

test("cheapestFirst sorts by headline price within each meter, unpriced last", async () => {
	const user = userEvent.setup();
	stubApi(() =>
		jsonResponse(
			modelPage([retiredModel, veoFast, claudeSonnet, klingTurbo, gptLuna]),
		),
	);
	renderWithQueries(
		<ModelPicker value={null} onChange={vi.fn()} cheapestFirst />,
	);

	await user.click(screen.getByRole("button", { name: "Select a model" }));
	await screen.findByRole("option", { name: /Veo 3.1 Fast/ });

	expect(
		screen.getAllByRole("option").map((option) => option.textContent),
	).toEqual([
		"GPT-6 Luna$0.40 per 1M input tokens",
		`${retiredModel.model}Unpriced`,
		"Claude Sonnet 5$3.00 per 1M input tokens",
		"Kling 2.5 Turbo Pro$0.07 per second",
		"Veo 3.1 Fast$0.15 per second",
	]);
});

test("the search goes to the server", async () => {
	const user = userEvent.setup();
	const requestedUrls = stubApi((url) =>
		jsonResponse(
			modelPage(
				url.searchParams.get("search") === "veo"
					? [veoFast]
					: [claudeSonnet, veoFast],
			),
		),
	);
	renderWithQueries(<ModelPicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a model" }));
	await screen.findByRole("option", { name: /Claude Sonnet 5/ });
	await user.keyboard("veo");

	await waitFor(() => {
		expect(
			screen.queryByRole("option", { name: /Claude Sonnet 5/ }),
		).not.toBeInTheDocument();
	});
	expect(screen.getByRole("option", { name: /Veo 3.1 Fast/ })).toBeVisible();
	expect(requestedUrls.at(-1)?.searchParams.get("search")).toBe("veo");
});

test("selecting a model hands back the model", async () => {
	const user = userEvent.setup();
	const onChange = vi.fn();
	stubApi(() => jsonResponse(modelPage([claudeSonnet, veoFast])));
	renderWithQueries(<ModelPicker value={null} onChange={onChange} />);

	await user.click(screen.getByRole("button", { name: "Select a model" }));
	await user.click(await screen.findByRole("option", { name: /Veo 3.1 Fast/ }));

	expect(onChange).toHaveBeenCalledExactlyOnceWith(veoFast);
});

test("the trigger shows the display name of the selected model", async () => {
	const requestedUrls = stubApi((url) =>
		jsonResponse(
			modelPage(url.searchParams.get("provider") === "fal_ai" ? [veoFast] : []),
		),
	);
	renderWithQueries(
		<ModelPicker
			value={{ provider: "fal_ai", model: "fal-ai/veo3.1/fast" }}
			onChange={vi.fn()}
		/>,
	);

	expect(
		await screen.findByRole("button", { name: "Veo 3.1 Fast" }),
	).toBeInTheDocument();
	const lookup = requestedUrls.find(
		(url) => url.searchParams.get("provider") === "fal_ai",
	);
	expect(lookup?.searchParams.get("search")).toBe("fal-ai/veo3.1/fast");
	expect(lookup?.searchParams.get("include_deprecated")).toBe("true");
});

test("the trigger shows the model name of a model without a display name", async () => {
	stubApi(() => jsonResponse(modelPage([retiredModel])));
	renderWithQueries(
		<ModelPicker
			value={{ provider: "openai", model: "gpt-3.5-turbo-0301" }}
			onChange={vi.fn()}
		/>,
	);

	expect(
		await screen.findByRole("button", { name: "gpt-3.5-turbo-0301" }),
	).toBeInTheDocument();
});

test("Load more requests the next page with its cursor", async () => {
	const user = userEvent.setup();
	const requestedUrls = stubApi((url) =>
		jsonResponse(
			url.searchParams.get("cursor") === "page-two"
				? modelPage([retiredModel])
				: modelPage([claudeSonnet], "page-two"),
		),
	);
	renderWithQueries(<ModelPicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a model" }));
	await user.click(await screen.findByRole("option", { name: "Load more" }));

	expect(
		await screen.findByRole("option", { name: /gpt-3.5-turbo-0301/ }),
	).toBeInTheDocument();
	expect(screen.getByRole("option", { name: /Claude Sonnet 5/ })).toBeVisible();
	expect(
		screen.queryByRole("option", { name: "Load more" }),
	).not.toBeInTheDocument();
	expect(requestedUrls.at(-1)?.searchParams.get("cursor")).toBe("page-two");
});
