import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { ParameterMappingsResponse } from "@/client";
import { AttributePicker } from "@/components/pickers/attribute-picker";
import {
	jsonResponse,
	renderWithQueries,
	stubApi,
} from "@/components/pickers/picker-test-support";

const parameterMappings: ParameterMappingsResponse = {
	models: [
		{
			provider: "fal_ai",
			model: "fal-ai/veo3.1/fast",
			parameters: {
				audio: {
					provider_parameter: "generate_audio",
					value_type: "boolean",
					allowed_values: [],
					effect: "prices",
					meter: "output_seconds",
					minimum: null,
					maximum: null,
					quantities: {},
				},
				duration: {
					provider_parameter: "duration",
					value_type: "string",
					allowed_values: ["4s", "8s"],
					effect: "sets",
					meter: "output_seconds",
					minimum: null,
					maximum: null,
					quantities: { "4s": "4", "8s": "8" },
				},
				resolution: {
					provider_parameter: "resolution",
					value_type: "string",
					allowed_values: ["720p", "4k"],
					effect: "prices",
					meter: "output_seconds",
					minimum: null,
					maximum: null,
					quantities: {},
				},
			},
		},
		{
			provider: "runwayml",
			model: "gen4_turbo",
			parameters: {
				duration: {
					provider_parameter: "duration",
					value_type: "integer",
					allowed_values: [],
					effect: "sets",
					meter: "output_seconds",
					minimum: 2,
					maximum: 10,
					quantities: {},
				},
			},
		},
		{
			provider: "runwayml",
			model: "veo3.1_fast",
			parameters: {
				duration: {
					provider_parameter: "duration",
					value_type: "integer",
					allowed_values: [4, 8],
					effect: "sets",
					meter: "output_seconds",
					minimum: null,
					maximum: null,
					quantities: {},
				},
			},
		},
	],
};

afterEach(() => {
	vi.unstubAllGlobals();
});

test("lists the overridable values of one model", async () => {
	const user = userEvent.setup();
	const requestedUrls = stubApi(() => jsonResponse(parameterMappings));
	renderWithQueries(
		<AttributePicker
			provider="fal_ai"
			model="fal-ai/veo3.1/fast"
			value={null}
			onChange={vi.fn()}
		/>,
	);

	await user.click(screen.getByRole("button", { name: "Select a value" }));
	await screen.findByRole("option", { name: "with audio" });

	expect(
		screen.getAllByRole("option").map((option) => option.textContent),
	).toEqual(["with audio", "without audio", "4s", "8s", "720p", "4K"]);
	expect(requestedUrls[0]?.pathname).toBe(
		"/api/v1/policies/parameter-mappings",
	);
});

test("selecting a value hands back its key and typed value", async () => {
	const user = userEvent.setup();
	const onChange = vi.fn();
	stubApi(() => jsonResponse(parameterMappings));
	renderWithQueries(
		<AttributePicker
			provider="runwayml"
			model="veo3.1_fast"
			value={null}
			onChange={onChange}
		/>,
	);

	await user.click(screen.getByRole("button", { name: "Select a value" }));
	await user.click(await screen.findByRole("option", { name: "8s" }));

	expect(onChange).toHaveBeenCalledExactlyOnceWith({
		key: "duration",
		value: 8,
	});
});

test("a parameter with only a range has no values to pick", async () => {
	const user = userEvent.setup();
	stubApi(() => jsonResponse(parameterMappings));
	renderWithQueries(
		<AttributePicker
			provider="runwayml"
			model="gen4_turbo"
			value={null}
			onChange={vi.fn()}
		/>,
	);

	await user.click(screen.getByRole("button", { name: "Select a value" }));

	expect(await screen.findByText("No values found")).toBeInTheDocument();
	expect(screen.queryByRole("option")).not.toBeInTheDocument();
});

test("the trigger shows the label of the selected value", () => {
	stubApi(() => jsonResponse(parameterMappings));
	renderWithQueries(
		<AttributePicker
			provider="fal_ai"
			model="fal-ai/veo3.1/fast"
			value={{ key: "audio", value: false }}
			onChange={vi.fn()}
		/>,
	);

	expect(
		screen.getByRole("button", { name: "without audio" }),
	).toBeInTheDocument();
});
