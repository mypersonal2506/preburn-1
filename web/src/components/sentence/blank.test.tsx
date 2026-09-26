import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactElement, useState } from "react";
import { afterEach, expect, test, vi } from "vitest";
import type { PageBodyKnownFeatureResponse } from "@/client";
import { AmountInput } from "@/components/inputs/amount-input";
import { PercentInput } from "@/components/inputs/percent-input";
import { FeaturePicker } from "@/components/pickers/feature-picker";
import {
	jsonResponse,
	renderWithQueries,
	stubApi,
} from "@/components/pickers/picker-test-support";
import { Blank } from "@/components/sentence/blank";
import { BlankPhrase } from "@/components/sentence/blank-phrase";
import { formatMoney, formatPercent } from "@/lib/format";
import { featureLabel } from "@/lib/labels";

const knownFeatures: PageBodyKnownFeatureResponse = {
	items: [
		{ feature: "chat", sources: ["decisions"] },
		{ feature: "text_to_video", sources: ["decisions", "policies"] },
	],
	next_cursor: null,
};

function AllowanceSentence(): ReactElement {
	const [amount, setAmount] = useState<string | null>(null);
	const [share, setShare] = useState<string | null>("0.4000");
	return (
		<p>
			Stop at{" "}
			<Blank
				placeholder="how much"
				phrase={amount === null ? null : formatMoney(amount)}
			>
				<AmountInput aria-label="Amount" value={amount} onChange={setAmount} />
			</Blank>{" "}
			or{" "}
			<Blank
				placeholder="what share"
				phrase={share === null ? null : formatPercent(share)}
			>
				<PercentInput aria-label="Share" value={share} onChange={setShare} />
			</Blank>
		</p>
	);
}

function FeatureSentence({
	invalidMessage,
}: {
	invalidMessage?: string;
}): ReactElement {
	const [feature, setFeature] = useState<string | null>(null);
	return (
		<p>
			using{" "}
			<FeaturePicker
				value={feature}
				onChange={setFeature}
				invalidMessage={invalidMessage}
				trigger={(label) => (
					<BlankPhrase placeholder="any feature" phrase={label} />
				)}
			/>
		</p>
	);
}

afterEach(() => {
	vi.unstubAllGlobals();
});

test("Tab moves across blanks, Enter and Space open and Escape returns focus", async () => {
	const user = userEvent.setup();
	renderWithQueries(<AllowanceSentence />);
	const amountBlank = screen.getByRole("button", { name: "how much" });
	const shareBlank = screen.getByRole("button", { name: "40.0%" });

	await user.tab();
	expect(amountBlank).toHaveFocus();
	await user.tab();
	expect(shareBlank).toHaveFocus();
	await user.tab({ shift: true });
	expect(amountBlank).toHaveFocus();

	await user.keyboard("{Enter}");
	const amountField = await screen.findByRole("textbox", { name: "Amount" });
	await waitFor(() => {
		expect(amountField).toHaveFocus();
	});
	await user.keyboard("12.5");
	expect(amountBlank).toHaveTextContent("$12.50");
	await user.keyboard("{Escape}");
	await waitFor(() => {
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});
	expect(amountBlank).toHaveFocus();

	await user.tab();
	await user.keyboard(" ");
	const shareField = await screen.findByRole("textbox", { name: "Share" });
	await waitFor(() => {
		expect(shareField).toHaveFocus();
	});
	await user.keyboard("{Escape}");
	await waitFor(() => {
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});
	expect(shareBlank).toHaveFocus();
});

test("a blank is a button that announces its popover", async () => {
	const user = userEvent.setup();
	renderWithQueries(<AllowanceSentence />);
	const amountBlank = screen.getByRole("button", { name: "how much" });

	expect(amountBlank).toHaveAttribute("type", "button");
	expect(amountBlank).toHaveAttribute("aria-haspopup", "dialog");
	expect(amountBlank).toHaveAttribute("aria-expanded", "false");
	expect(amountBlank).toHaveAttribute("aria-invalid", "false");
	await user.click(amountBlank);

	expect(amountBlank).toHaveAttribute("aria-expanded", "true");
	expect(screen.getByRole("dialog", { name: "how much" })).toBeInTheDocument();
});

test("an empty blank reads as its placeholder phrase", () => {
	renderWithQueries(
		<p>
			<Blank placeholder="which plan" phrase={null}>
				<span>Plans</span>
			</Blank>
		</p>,
	);

	expect(screen.getByRole("button", { name: "which plan" })).toHaveAttribute(
		"data-empty",
		"true",
	);
});

test("an invalid blank is underlined and shows its message in the popover", async () => {
	const user = userEvent.setup();
	renderWithQueries(
		<p>
			For{" "}
			<Blank
				placeholder="which plan"
				phrase="Legacy"
				invalidMessage="expected an active plan"
			>
				<span>Plans</span>
			</Blank>{" "}
			customers
		</p>,
	);
	const blank = screen.getByRole("button", { name: "Legacy" });

	expect(blank).toHaveAttribute("aria-invalid", "true");
	expect(blank).toHaveClass("aria-invalid:decoration-destructive");
	await user.click(blank);

	const popover = await screen.findByRole("dialog", { name: "which plan" });
	expect(popover).toHaveAccessibleDescription("expected an active plan");
	expect(within(popover).getByText("expected an active plan")).toBeVisible();
});

test("defaultOpen opens a blank as it mounts", async () => {
	renderWithQueries(
		<p>
			<Blank placeholder="how much" phrase={null} defaultOpen>
				<AmountInput aria-label="Amount" value={null} onChange={vi.fn()} />
			</Blank>
		</p>,
	);

	const amountField = await screen.findByRole("textbox", { name: "Amount" });
	await waitFor(() => {
		expect(amountField).toHaveFocus();
	});
});

test("a blank phrase as a picker trigger opens one popover with the picker", async () => {
	const user = userEvent.setup();
	stubApi(() => jsonResponse(knownFeatures));
	renderWithQueries(<FeatureSentence />);
	const featureBlank = screen.getByRole("button", { name: "any feature" });

	expect(featureBlank).toHaveAttribute("data-empty", "true");
	await user.click(featureBlank);

	expect(await screen.findAllByRole("dialog")).toHaveLength(1);
	await user.click(
		await screen.findByRole("option", { name: featureLabel("text_to_video") }),
	);

	await waitFor(() => {
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});
	expect(featureBlank).toHaveTextContent("text to video");
	expect(featureBlank).toHaveAttribute("data-empty", "false");
	expect(featureBlank).toHaveFocus();
});

test("an invalid picker blank is underlined and shows its message in the popover", async () => {
	const user = userEvent.setup();
	stubApi(() => jsonResponse(knownFeatures));
	renderWithQueries(
		<FeatureSentence invalidMessage="expected a feature name" />,
	);
	const featureBlank = screen.getByRole("button", { name: "any feature" });

	expect(featureBlank).toHaveAttribute("aria-invalid", "true");
	await user.click(featureBlank);

	expect(await screen.findByRole("dialog")).toHaveAccessibleDescription(
		"expected a feature name",
	);
});
