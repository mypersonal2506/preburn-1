import { describe, expect, test } from "vitest";
import {
	amountToDollars,
	DecimalInputError,
	type DecimalInputProblem,
	displayedPriceToUnitPrice,
	displayedUnitCost,
	dollarsToAmount,
	paceToRatio,
	percentToRatio,
	ratioToPace,
	ratioToPercent,
	unitPriceToDisplayedPrice,
} from "@/lib/decimal";

function thrownBy(convert: () => unknown): unknown {
	try {
		convert();
	} catch (error) {
		return error;
	}
	throw new Error("conversion did not throw");
}

function expectProblem(
	convert: () => unknown,
	problem: DecimalInputProblem,
	limit: number | null,
): void {
	const error = thrownBy(convert);
	expect(error).toBeInstanceOf(DecimalInputError);
	expect(error).toMatchObject({ problem, limit });
}

describe("dollarsToAmount", () => {
	test.each([
		["12.5", "12.500000000"],
		["0", "0.000000000"],
		["-3", "-3.000000000"],
		["-0", "0.000000000"],
		[" 7 ", "7.000000000"],
		["007.10", "7.100000000"],
		["12.", "12.000000000"],
		[".5", "0.500000000"],
		["0.000000001", "0.000000001"],
		["1.12345678900", "1.123456789"],
		["999999999.999999999", "999999999.999999999"],
	])("%s dollars is the amount %s", (dollars, amount) => {
		expect(dollarsToAmount(dollars)).toBe(amount);
	});

	test.each(["", "-", ".", "abc", "1.2.3", "1e5", "$5", "1,000", "+5", "--1"])(
		"%j is not a decimal",
		(dollars) => {
			expectProblem(() => dollarsToAmount(dollars), "invalid_format", null);
		},
	);

	test("rejects more than 9 decimals", () => {
		expectProblem(
			() => dollarsToAmount("1.1234567891"),
			"too_many_decimals",
			9,
		);
	});

	test("rejects more than 9 integer digits", () => {
		expectProblem(
			() => dollarsToAmount("1234567890"),
			"too_many_integer_digits",
			9,
		);
	});
});

describe("amountToDollars", () => {
	test.each([
		["12.500000000", "12.5"],
		["0.000000000", "0"],
		["-7.200000000", "-7.2"],
		["30.000000000", "30"],
		["0.000000001", "0.000000001"],
	])("the amount %s is %s dollars", (amount, dollars) => {
		expect(amountToDollars(amount)).toBe(dollars);
	});
});

describe("percentToRatio", () => {
	test.each([
		["40", "0.4000"],
		["12.5", "0.1250"],
		["40.25", "0.4025"],
		["-50", "-0.5000"],
		["100", "1.0000"],
		["0", "0.0000"],
		["99.99", "0.9999"],
	])("%s percent is the ratio %s", (percent, ratio) => {
		expect(percentToRatio(percent)).toBe(ratio);
	});

	test("rejects a percent needing more than 4 ratio decimals", () => {
		expectProblem(() => percentToRatio("12.345"), "too_many_decimals", 2);
	});

	test("rejects a ratio above 9 integer digits", () => {
		expectProblem(
			() => percentToRatio("100000000000"),
			"too_many_integer_digits",
			11,
		);
	});
});

describe("ratioToPercent", () => {
	test.each([
		["0.4000", "40"],
		["0.4025", "40.25"],
		["-0.5000", "-50"],
		["1.0000", "100"],
		["0.0000", "0"],
	])("the ratio %s is %s percent", (ratio, percent) => {
		expect(ratioToPercent(ratio)).toBe(percent);
	});

	test("rejects an infinite ratio", () => {
		expectProblem(() => ratioToPercent("inf"), "invalid_format", null);
	});
});

describe("pace", () => {
	test.each([
		["2", "2.0000"],
		["2.5", "2.5000"],
		["0.25", "0.2500"],
	])("the pace %s is the ratio %s", (pace, ratio) => {
		expect(paceToRatio(pace)).toBe(ratio);
	});

	test("rejects a pace with more than 4 decimals", () => {
		expectProblem(() => paceToRatio("2.12345"), "too_many_decimals", 4);
	});

	test.each([
		["2.0000", "2"],
		["2.5000", "2.5"],
	])("the ratio %s is the pace %s", (ratio, pace) => {
		expect(ratioToPace(ratio)).toBe(pace);
	});
});

describe("displayedPriceToUnitPrice", () => {
	test("$3.00 per 1M input tokens", () => {
		expect(displayedPriceToUnitPrice("3.00", "input_tokens")).toEqual({
			unit_price: "3",
			unit_quantity: 1000000,
		});
	});

	test("$0.15 per second", () => {
		expect(displayedPriceToUnitPrice("0.15", "output_seconds")).toEqual({
			unit_price: "0.15",
			unit_quantity: 1,
		});
	});

	test("rejects more than 9 decimals", () => {
		expectProblem(
			() => displayedPriceToUnitPrice("0.0000000001", "characters"),
			"too_many_decimals",
			9,
		);
	});

	test.each(["-3", "-0.5"])("rejects the negative price %s", (price) => {
		expectProblem(
			() => displayedPriceToUnitPrice(price, "requests"),
			"negative",
			null,
		);
	});

	test("accepts a price of zero written with a minus sign", () => {
		expect(displayedPriceToUnitPrice("-0", "requests")).toEqual({
			unit_price: "0",
			unit_quantity: 1,
		});
	});
});

describe("unitPriceToDisplayedPrice", () => {
	test.each([
		["3.000000000", 1000000, "input_tokens", "3"],
		["0.000000150", 1, "input_tokens", "0.15"],
		["0.003000000", 1000, "output_tokens", "3"],
		["0.400000000", 1, "output_seconds", "0.4"],
		["0.300000000", 1000, "characters", "0.0003"],
	] as const)(
		"%s per %d %s is %s per displayed unit",
		(unitPrice, unitQuantity, meter, displayed) => {
			expect(unitPriceToDisplayedPrice(unitPrice, unitQuantity, meter)).toBe(
				displayed,
			);
		},
	);

	test("rejects a price needing more than 9 decimals per displayed unit", () => {
		expectProblem(
			() => unitPriceToDisplayedPrice("0.123456789", 1000000, "characters"),
			"too_many_decimals",
			9,
		);
	});

	test("rejects a price above 9 integer digits per displayed unit", () => {
		expectProblem(
			() => unitPriceToDisplayedPrice("1000.000000000", 1, "input_tokens"),
			"too_many_integer_digits",
			9,
		);
	});

	test("round trips through displayedPriceToUnitPrice", () => {
		const unitPrice = displayedPriceToUnitPrice("2.75", "output_tokens");

		expect(
			unitPriceToDisplayedPrice(
				unitPrice.unit_price,
				unitPrice.unit_quantity,
				"output_tokens",
			),
		).toBe("2.75");
	});
});

describe("displayedUnitCost", () => {
	test.each([
		["3.000000000", 1000000, "input_tokens", "3.000000000"],
		["0.300000000", 1000, "characters", "0.000300000"],
		["0.000000001", 3, "output_seconds", "0.000000000"],
		["0.000000002", 3, "output_seconds", "0.000000001"],
		["0.123456789", 1000000, "characters", "0.000000123"],
		["0.123456500", 1000000, "images", "0.000000123"],
		["0.000001500", 1000, "requests", "0.000000002"],
	] as const)(
		"%s per %d %s costs %s per displayed unit",
		(unitPrice, unitQuantity, meter, cost) => {
			expect(displayedUnitCost(unitPrice, unitQuantity, meter)).toBe(cost);
		},
	);
});
