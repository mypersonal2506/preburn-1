import type { ReactElement } from "react";
import {
	ConvertedInput,
	type TextConversion,
	type TypedInputProps,
} from "@/components/inputs/converted-input";
import { DecimalInputError } from "@/lib/decimal";

/** Props of CountInput. */
export type CountInputProps = TypedInputProps<string>;

const COUNT_DIGITS_MAXIMUM = 9;
const COUNT_TEXT_PATTERN = /^(-?)(\d+)(\.\d*)?$/;
const LEADING_ZEROS_PATTERN = /^0+(?=\d)/;

const COUNT_CONVERSION: TextConversion<string> = {
	format: String,
	parse: parseCount,
};

/**
 * A whole count of 0 or more typed as digits, such as "20", reported as the
 * API's whole-number string without leading zeros, "20". Rejects text that
 * is not a number, decimals, negative numbers and more than 9 digits.
 */
export function CountInput(props: CountInputProps): ReactElement {
	return (
		<ConvertedInput
			{...props}
			conversion={COUNT_CONVERSION}
			inputMode="numeric"
		/>
	);
}

function parseCount(text: string): string {
	const match = COUNT_TEXT_PATTERN.exec(text.trim());
	if (match === null) {
		throw new DecimalInputError("invalid_format", null);
	}
	const [, sign, digits = "", fraction] = match;
	if (fraction !== undefined) {
		throw new DecimalInputError("too_many_decimals", 0);
	}
	if (sign === "-") {
		throw new DecimalInputError("negative", null);
	}
	const count = digits.replace(LEADING_ZEROS_PATTERN, "");
	if (count.length > COUNT_DIGITS_MAXIMUM) {
		throw new DecimalInputError(
			"too_many_integer_digits",
			COUNT_DIGITS_MAXIMUM,
		);
	}
	return count;
}
