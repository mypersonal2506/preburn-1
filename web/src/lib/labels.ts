import type {
	MeterDescription,
	ModelAttributeValue,
	PolicyCondition,
} from "@/client";
import { displayedUnitCost } from "@/lib/decimal";
import { formatQuantity, formatUnitCost } from "@/lib/format";

/** An attribute value of a request or a price condition. */
export type AttributeValue = ModelAttributeValue["value"];

type Meter = MeterDescription["meter"];

type ConditionOperator = PolicyCondition["operator"];

type MeterWords = {
	name: string;
	priceUnit: string;
	usage: (quantity: string, isOne: boolean) => string;
};

const KEY_SEPARATOR_PATTERN = /_/g;
const THOUSANDS_SUFFIX_PATTERN = /(\d)k\b/g;
const WHOLE_NUMBER_PATTERN = /^\d+$/;

const METER_WORDS: Record<Meter, MeterWords> = {
	input_tokens: tokenWords("input"),
	cached_input_tokens: tokenWords("cached input"),
	cache_write_input_tokens: tokenWords("cache write"),
	output_tokens: tokenWords("output"),
	reasoning_tokens: tokenWords("reasoning"),
	input_audio_tokens: tokenWords("input audio"),
	output_audio_tokens: tokenWords("output audio"),
	input_seconds: {
		name: "input seconds",
		priceUnit: "second",
		usage: (quantity) => `${quantity}s of input`,
	},
	output_seconds: {
		name: "output seconds",
		priceUnit: "second",
		usage: (quantity) => `${quantity}s of output`,
	},
	gpu_seconds: {
		name: "GPU seconds",
		priceUnit: "GPU second",
		usage: (quantity) => `${quantity}s of GPU time`,
	},
	characters: countWords("character", "characters"),
	images: countWords("image", "images"),
	megapixels: countWords("megapixel", "megapixels"),
	audio_minutes: {
		name: "audio minutes",
		priceUnit: "minute",
		usage: (quantity) => `${quantity} min of audio`,
	},
	requests: countWords("request", "requests"),
	search_requests: countWords("search request", "search requests"),
};

const ATTRIBUTE_VALUE_PHRASES = new Map<string, (value: string) => string>([
	["context_tier", (value) => `${value} context`],
	[
		"duration",
		(value) => (WHOLE_NUMBER_PATTERN.test(value) ? `${value}s` : value),
	],
	["quality", (value) => `${value === "hd" ? "HD" : value} quality`],
	["region", (value) => `${value} region`],
	["resolution", (value) => value],
	["service_tier", (value) => `${value} tier`],
	["size", (value) => value],
]);

const CONDITION_OPERATOR_PHRASES: Record<ConditionOperator, string> = {
	gt: "above",
	gte: "at least",
	lt: "below",
	lte: "at most",
	eq: "exactly",
	ne: "not",
};

/** Humanizes a feature key for display, such as "text to video". */
export function featureLabel(feature: string): string {
	return humanizeKey(feature);
}

/** Names a meter for display, such as "input tokens" or "GPU seconds". */
export function meterLabel(meter: Meter): string {
	return METER_WORDS[meter].name;
}

/**
 * Names the unit a meter's prices are shown per, such as "1M input tokens"
 * or "second", the unit unitPriceLabel prices per.
 */
export function meterPriceUnitLabel(meter: Meter): string {
	return METER_WORDS[meter].priceUnit;
}

/**
 * Describes an API usage quantity of a meter, such as "1,200 input tokens",
 * "1 image" or "8s of output".
 */
export function usageLabel(meter: Meter, quantity: string): string {
	return METER_WORDS[meter].usage(
		formatQuantity(quantity),
		Number(quantity) === 1,
	);
}

/**
 * Describes an API unit_price of unitQuantity units per displayed unit of
 * the meter, such as "$3.00 per 1M input tokens" or "$0.15 per second".
 * Token meters show prices per 1M tokens, every other meter per unit.
 */
export function unitPriceLabel(
	unitPrice: string,
	unitQuantity: number,
	meter: Meter,
): string {
	const cost = formatUnitCost(
		displayedUnitCost(unitPrice, unitQuantity, meter),
	);
	return `${cost} per ${METER_WORDS[meter].priceUnit}`;
}

/**
 * Describes one attribute value for display, such as "with audio",
 * "without audio", "720p", "HD quality", "above 200K context" or "4s" for a
 * duration of "4s" or 4. Unknown keys read as the key and the value, such
 * as "mode fast".
 */
export function attributeValueLabel(
	key: string,
	value: AttributeValue,
): string {
	if (typeof value === "boolean") {
		return `${value ? "with" : "without"} ${humanizeKey(key)}`;
	}
	const text = humanizeKey(String(value)).replace(
		THOUSANDS_SUFFIX_PATTERN,
		"$1K",
	);
	const phrase = ATTRIBUTE_VALUE_PHRASES.get(key);
	return phrase === undefined ? `${humanizeKey(key)} ${text}` : phrase(text);
}

/**
 * Phrases a policy condition operator as the sentence builder reads it:
 * "above", "at least", "below", "at most", "exactly" or "not".
 */
export function conditionOperatorLabel(operator: ConditionOperator): string {
	return CONDITION_OPERATOR_PHRASES[operator];
}

function tokenWords(kind: string): MeterWords {
	return {
		name: `${kind} tokens`,
		priceUnit: `1M ${kind} tokens`,
		usage: (quantity, isOne) =>
			`${quantity} ${kind} ${isOne ? "token" : "tokens"}`,
	};
}

function countWords(singular: string, plural: string): MeterWords {
	return {
		name: plural,
		priceUnit: singular,
		usage: (quantity, isOne) => `${quantity} ${isOne ? singular : plural}`,
	};
}

function humanizeKey(key: string): string {
	return key.replace(KEY_SEPARATOR_PATTERN, " ");
}
