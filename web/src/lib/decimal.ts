import type { CreateOverrideRequest, MeterDescription } from "@/client";

/**
 * Why decimal text was rejected: it is not a plain decimal, it has more
 * decimals or integer digits than the value allows, or it is below zero
 * where the value cannot be.
 */
export type DecimalInputProblem =
	| "invalid_format"
	| "too_many_decimals"
	| "too_many_integer_digits"
	| "negative";

/** A USD price with the unit quantity it pays for, as the API takes them. */
export type UnitPrice = Pick<
	CreateOverrideRequest,
	"unit_price" | "unit_quantity"
>;

type Meter = MeterDescription["meter"];

const AMOUNT_DECIMALS = 9;
const RATIO_DECIMALS = 4;
const PERCENT_DECIMALS = RATIO_DECIMALS - 2;
const INTEGER_DIGITS_MAXIMUM = 9;
const PERCENT_INTEGER_DIGITS_MAXIMUM = INTEGER_DIGITS_MAXIMUM + 2;
const AMOUNT_SCALED_LIMIT =
	10n ** BigInt(INTEGER_DIGITS_MAXIMUM + AMOUNT_DECIMALS);
const TOKEN_DISPLAYED_UNIT_QUANTITY = 1_000_000;
const DECIMAL_TEXT_PATTERN = /^-?(\d+\.?\d*|\.\d+)$/;
const TRAILING_ZEROS_PATTERN = /\.0+$|(\.\d*?[1-9])0+$/;

const DISPLAYED_UNIT_QUANTITIES: Record<Meter, number> = {
	input_tokens: TOKEN_DISPLAYED_UNIT_QUANTITY,
	cached_input_tokens: TOKEN_DISPLAYED_UNIT_QUANTITY,
	cache_write_input_tokens: TOKEN_DISPLAYED_UNIT_QUANTITY,
	output_tokens: TOKEN_DISPLAYED_UNIT_QUANTITY,
	reasoning_tokens: TOKEN_DISPLAYED_UNIT_QUANTITY,
	input_audio_tokens: TOKEN_DISPLAYED_UNIT_QUANTITY,
	output_audio_tokens: TOKEN_DISPLAYED_UNIT_QUANTITY,
	input_seconds: 1,
	output_seconds: 1,
	gpu_seconds: 1,
	characters: 1,
	images: 1,
	megapixels: 1,
	audio_minutes: 1,
	requests: 1,
	search_requests: 1,
};

/**
 * Decimal text a conversion rejected. `limit` is the largest number of
 * decimals or integer digits the value allows, null for invalid_format and
 * negative.
 */
export class DecimalInputError extends Error {
	readonly problem: DecimalInputProblem;
	readonly limit: number | null;

	constructor(problem: DecimalInputProblem, limit: number | null) {
		super(`decimal input invalid problem=${problem} limit=${limit}`);
		this.name = "DecimalInputError";
		this.problem = problem;
		this.limit = limit;
	}
}

/**
 * Converts dollars typed by a person, such as "12.5", to the API amount
 * string with 9 decimals, "12.500000000". Accepts a leading minus sign,
 * surrounding spaces, leading zeros and trailing zeros. Throws
 * DecimalInputError for other text, more than 9 decimals or more than 9
 * integer digits.
 */
export function dollarsToAmount(dollars: string): string {
	return writeScaled(readAmount(dollars), AMOUNT_DECIMALS);
}

/**
 * Converts an API amount string, such as "12.500000000", to the shortest
 * exact dollar text, "12.5". Throws DecimalInputError for text that is not
 * an amount.
 */
export function amountToDollars(amount: string): string {
	return writeShortest(readAmount(amount), AMOUNT_DECIMALS);
}

/**
 * Converts a percent typed by a person, such as "40", to the API ratio
 * string with 4 decimals, "0.4000". Throws DecimalInputError for text that
 * is not a decimal, more than 2 decimals or more than 11 integer digits.
 */
export function percentToRatio(percent: string): string {
	return writeScaled(
		readScaled(percent, PERCENT_DECIMALS, PERCENT_INTEGER_DIGITS_MAXIMUM),
		RATIO_DECIMALS,
	);
}

/**
 * Converts an API ratio string, such as "0.4025", to the shortest exact
 * percent text, "40.25". Throws DecimalInputError for text that is not a
 * finite ratio, "inf" included.
 */
export function ratioToPercent(ratio: string): string {
	return writeShortest(readRatio(ratio), PERCENT_DECIMALS);
}

/**
 * Converts a pace typed by a person, such as "2.5", to the API ratio string
 * with 4 decimals, "2.5000". Throws DecimalInputError for text that is not a
 * decimal, more than 4 decimals or more than 9 integer digits.
 */
export function paceToRatio(pace: string): string {
	return writeScaled(readRatio(pace), RATIO_DECIMALS);
}

/**
 * Converts an API ratio string, such as "2.5000", to the shortest exact pace
 * text, "2.5". Throws DecimalInputError for text that is not a finite ratio.
 */
export function ratioToPace(ratio: string): string {
	return writeShortest(readRatio(ratio), RATIO_DECIMALS);
}

/**
 * Converts a USD price per displayed unit of the meter, such as "3.00" per
 * 1M input tokens, to the API unit_price and unit_quantity,
 * `{unit_price: "3", unit_quantity: 1000000}`. Token meters display prices
 * per 1M tokens, every other meter per unit. Throws DecimalInputError for
 * text that is not a decimal, a price below zero, more than 9 decimals or
 * more than 9 integer digits.
 */
export function displayedPriceToUnitPrice(
	price: string,
	meter: Meter,
): UnitPrice {
	const amount = readAmount(price);
	if (amount < 0n) {
		throw new DecimalInputError("negative", null);
	}
	return {
		unit_price: writeShortest(amount, AMOUNT_DECIMALS),
		unit_quantity: DISPLAYED_UNIT_QUANTITIES[meter],
	};
}

/**
 * Converts an API unit_price of unitQuantity units to the exact USD price
 * per displayed unit of the meter as the shortest decimal text, such as "3"
 * for "0.000003000" per 1 input token. Throws DecimalInputError when the
 * price per displayed unit needs more than 9 decimals or more than 9
 * integer digits, so it can be entered and converted back unchanged.
 */
export function unitPriceToDisplayedPrice(
	unitPrice: string,
	unitQuantity: number,
	meter: Meter,
): string {
	const priceTimesDisplayedQuantity = readPriceTimesDisplayedQuantity(
		unitPrice,
		meter,
	);
	const quantity = BigInt(unitQuantity);
	if (priceTimesDisplayedQuantity % quantity !== 0n) {
		throw new DecimalInputError("too_many_decimals", AMOUNT_DECIMALS);
	}
	const displayedPrice = priceTimesDisplayedQuantity / quantity;
	if (displayedPrice >= AMOUNT_SCALED_LIMIT) {
		throw new DecimalInputError(
			"too_many_integer_digits",
			INTEGER_DIGITS_MAXIMUM,
		);
	}
	return writeShortest(displayedPrice, AMOUNT_DECIMALS);
}

/**
 * Returns the USD cost of one displayed unit of the meter for a non-negative
 * API unit_price of unitQuantity units, as an amount string with 9 decimals
 * rounded half up, such as "3.000000000" per 1M input tokens. It is for
 * display. Inputs use unitPriceToDisplayedPrice, which never rounds.
 */
export function displayedUnitCost(
	unitPrice: string,
	unitQuantity: number,
	meter: Meter,
): string {
	const priceTimesDisplayedQuantity = readPriceTimesDisplayedQuantity(
		unitPrice,
		meter,
	);
	const quantity = BigInt(unitQuantity);
	return writeScaled(
		(priceTimesDisplayedQuantity * 2n + quantity) / (quantity * 2n),
		AMOUNT_DECIMALS,
	);
}

function readAmount(amount: string): bigint {
	return readScaled(amount, AMOUNT_DECIMALS, INTEGER_DIGITS_MAXIMUM);
}

function readRatio(ratio: string): bigint {
	return readScaled(ratio, RATIO_DECIMALS, INTEGER_DIGITS_MAXIMUM);
}

function readPriceTimesDisplayedQuantity(
	unitPrice: string,
	meter: Meter,
): bigint {
	return readAmount(unitPrice) * BigInt(DISPLAYED_UNIT_QUANTITIES[meter]);
}

function readScaled(
	text: string,
	decimals: number,
	integerDigitsMaximum: number,
): bigint {
	const trimmed = text.trim();
	if (!DECIMAL_TEXT_PATTERN.test(trimmed)) {
		throw new DecimalInputError("invalid_format", null);
	}
	const negative = trimmed.startsWith("-");
	const [integerDigits = "", fractionDigits = ""] = trimmed
		.replace("-", "")
		.split(".");
	const significantInteger = integerDigits.replace(/^0+/, "");
	const significantFraction = fractionDigits.replace(/0+$/, "");
	if (significantInteger.length > integerDigitsMaximum) {
		throw new DecimalInputError(
			"too_many_integer_digits",
			integerDigitsMaximum,
		);
	}
	if (significantFraction.length > decimals) {
		throw new DecimalInputError("too_many_decimals", decimals);
	}
	const magnitude = BigInt(
		significantInteger + significantFraction.padEnd(decimals, "0"),
	);
	return negative ? -magnitude : magnitude;
}

function writeScaled(value: bigint, decimals: number): string {
	const digits = (value < 0n ? -value : value)
		.toString()
		.padStart(decimals + 1, "0");
	const sign = value < 0n ? "-" : "";
	return `${sign}${digits.slice(0, -decimals)}.${digits.slice(-decimals)}`;
}

function writeShortest(value: bigint, decimals: number): string {
	return writeScaled(value, decimals).replace(TRAILING_ZEROS_PATTERN, "$1");
}
