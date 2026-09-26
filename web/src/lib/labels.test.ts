import { describe, expect, test } from "vitest";
import type { MeterDescription } from "@/client";
import { displayedPriceToUnitPrice } from "@/lib/decimal";
import {
	attributeValueLabel,
	conditionOperatorLabel,
	featureLabel,
	meterLabel,
	meterPriceUnitLabel,
	unitPriceLabel,
	usageLabel,
} from "@/lib/labels";

const meters: MeterDescription["meter"][] = [
	"input_tokens",
	"cached_input_tokens",
	"cache_write_input_tokens",
	"output_tokens",
	"reasoning_tokens",
	"input_audio_tokens",
	"output_audio_tokens",
	"input_seconds",
	"output_seconds",
	"gpu_seconds",
	"characters",
	"images",
	"megapixels",
	"audio_minutes",
	"requests",
	"search_requests",
];

describe("featureLabel", () => {
	test.each([
		["text_to_video", "text to video"],
		["chat", "chat"],
		["image_2", "image 2"],
	])("%s is %s", (feature, label) => {
		expect(featureLabel(feature)).toBe(label);
	});
});

describe("meterLabel", () => {
	test.each([
		["input_tokens", "input tokens"],
		["cache_write_input_tokens", "cache write tokens"],
		["gpu_seconds", "GPU seconds"],
		["audio_minutes", "audio minutes"],
		["search_requests", "search requests"],
	] as const)("%s is %s", (meter, label) => {
		expect(meterLabel(meter)).toBe(label);
	});
});

describe("usageLabel", () => {
	test.each([
		["output_seconds", "8", "8s of output"],
		["input_seconds", "12.5", "12.5s of input"],
		["gpu_seconds", "90", "90s of GPU time"],
		["input_tokens", "1200", "1,200 input tokens"],
		["input_tokens", "1", "1 input token"],
		["output_tokens", "1.000000", "1 output token"],
		["images", "3", "3 images"],
		["images", "1", "1 image"],
		["megapixels", "1.5", "1.5 megapixels"],
		["audio_minutes", "2.5", "2.5 min of audio"],
		["characters", "5000", "5,000 characters"],
		["requests", "1", "1 request"],
		["search_requests", "2", "2 search requests"],
	] as const)("%s %s is %s", (meter, quantity, label) => {
		expect(usageLabel(meter, quantity)).toBe(label);
	});
});

describe("unitPriceLabel", () => {
	test.each([
		["3.000000000", 1000000, "input_tokens", "$3.00 per 1M input tokens"],
		["0.000000150", 1, "output_tokens", "$0.15 per 1M output tokens"],
		["0.400000000", 1, "output_seconds", "$0.40 per second"],
		["0.300000000", 1000, "characters", "$0.0003 per character"],
		["0.040000000", 1, "images", "$0.04 per image"],
		["0.006000000", 1, "audio_minutes", "$0.006 per minute"],
		["0.001000000", 3, "gpu_seconds", "$0.0003333 per GPU second"],
	] as const)("%s per %d %s is %s", (unitPrice, unitQuantity, meter, label) => {
		expect(unitPriceLabel(unitPrice, unitQuantity, meter)).toBe(label);
	});

	test.each(meters)(
		"%s names 1M units exactly when prices convert per 1M units",
		(meter) => {
			const perMillion =
				displayedPriceToUnitPrice("1", meter).unit_quantity === 1000000;

			expect(unitPriceLabel("1.000000000", 1, meter).includes(" per 1M ")).toBe(
				perMillion,
			);
		},
	);
});

describe("attributeValueLabel", () => {
	test.each([
		["audio", true, "with audio"],
		["audio", false, "without audio"],
		["resolution", "720p", "720p"],
		["resolution", "4k", "4K"],
		["quality", "hd", "HD quality"],
		["quality", "low", "low quality"],
		["context_tier", "above_200k", "above 200K context"],
		["context_tier", "standard", "standard context"],
		["service_tier", "batch", "batch tier"],
		["region", "us-east-1", "us-east-1 region"],
		["size", "1024x768", "1024x768"],
		["mode", "fast", "mode fast"],
		["prompt_style", "vivid", "prompt style vivid"],
		["seed", 42, "seed 42"],
		["duration", "4s", "4s"],
		["duration", "5", "5s"],
		["duration", 8, "8s"],
		["stream", true, "with stream"],
		["constructor", "fast", "constructor fast"],
	] as const)("%s %s is %s", (key, value, label) => {
		expect(attributeValueLabel(key, value)).toBe(label);
	});
});

describe("meterPriceUnitLabel", () => {
	test.each([
		["input_tokens", "1M input tokens"],
		["cache_write_input_tokens", "1M cache write tokens"],
		["output_seconds", "second"],
		["gpu_seconds", "GPU second"],
		["audio_minutes", "minute"],
		["images", "image"],
	] as const)("%s is %s", (meter, label) => {
		expect(meterPriceUnitLabel(meter)).toBe(label);
	});

	test.each(meters)("%s is the unit unitPriceLabel prices per", (meter) => {
		expect(unitPriceLabel("1.000000000", 1, meter)).toMatch(
			new RegExp(` per ${meterPriceUnitLabel(meter)}$`),
		);
	});
});

describe("conditionOperatorLabel", () => {
	test.each([
		["gt", "above"],
		["gte", "at least"],
		["lt", "below"],
		["lte", "at most"],
		["eq", "exactly"],
		["ne", "not"],
	] as const)("%s is %s", (operator, label) => {
		expect(conditionOperatorLabel(operator)).toBe(label);
	});
});
