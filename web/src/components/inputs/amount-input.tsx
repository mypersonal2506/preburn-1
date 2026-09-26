import type { ReactElement } from "react";
import {
	ConvertedInput,
	type TextConversion,
	type TypedInputProps,
} from "@/components/inputs/converted-input";
import { amountToDollars, dollarsToAmount } from "@/lib/decimal";

/** Props of AmountInput. */
export type AmountInputProps = TypedInputProps<string>;

const AMOUNT_CONVERSION: TextConversion<string> = {
	format: amountToDollars,
	parse: dollarsToAmount,
};

/**
 * A USD amount typed in dollars, such as "12.5", reported as the exact API
 * amount string, "12.500000000". Rejects text that is not a decimal, more
 * than 9 decimals and more than 9 integer digits.
 */
export function AmountInput(props: AmountInputProps): ReactElement {
	return (
		<ConvertedInput
			{...props}
			conversion={AMOUNT_CONVERSION}
			inputMode="decimal"
			prefix="$"
		/>
	);
}
