import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { PageBodyMeterDescription } from "@/client";
import { MeterPicker } from "@/components/pickers/meter-picker";
import {
	jsonResponse,
	renderWithQueries,
	stubApi,
} from "@/components/pickers/picker-test-support";

const meters: PageBodyMeterDescription = {
	items: [
		{
			meter: "input_tokens",
			description: "Tokens sent to the model.",
			unit: "token",
		},
		{
			meter: "gpu_seconds",
			description: "Seconds of GPU time.",
			unit: "second",
		},
	],
	next_cursor: null,
};

afterEach(() => {
	vi.unstubAllGlobals();
});

test("lists the meters by label and finds them by key", async () => {
	const user = userEvent.setup();
	const requestedUrls = stubApi(() => jsonResponse(meters));
	renderWithQueries(<MeterPicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a meter" }));
	expect(
		await screen.findByRole("option", { name: "input tokens" }),
	).toBeInTheDocument();
	await user.keyboard("gpu_sec");

	expect(
		screen.getByRole("option", { name: "GPU seconds" }),
	).toBeInTheDocument();
	expect(
		screen.queryByRole("option", { name: "input tokens" }),
	).not.toBeInTheDocument();
	expect(requestedUrls[0]?.pathname).toBe("/api/v1/pricing/meters");
});

test("selecting a meter hands back its key", async () => {
	const user = userEvent.setup();
	const onChange = vi.fn();
	stubApi(() => jsonResponse(meters));
	renderWithQueries(<MeterPicker value={null} onChange={onChange} />);

	await user.click(screen.getByRole("button", { name: "Select a meter" }));
	await user.click(await screen.findByRole("option", { name: "GPU seconds" }));

	expect(onChange).toHaveBeenCalledExactlyOnceWith("gpu_seconds");
});

test("the trigger shows the label of the selected meter", () => {
	stubApi(() => jsonResponse(meters));
	renderWithQueries(<MeterPicker value="output_seconds" onChange={vi.fn()} />);

	expect(
		screen.getByRole("button", { name: "output seconds" }),
	).toBeInTheDocument();
});
