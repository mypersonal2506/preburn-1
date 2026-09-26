import type { ReactElement } from "react";
import {
	ConvertedInput,
	type TextConversion,
	type TypedInputProps,
} from "@/components/inputs/converted-input";
import { percentToRatio, ratioToPercent } from "@/lib/decimal";

/** Props of PercentInput. */
export type PercentInputProps = TypedInputProps<string>;

const PERCENT_CONVERSION: TextConversion<string> = {
	format: ratioToPercent,
	parse: percentToRatio,
};

/**
 * A percent typed as a number, such as "40", reported as the exact API ratio
 * string, "0.4000". Rejects text that is not a decimal and more than 2
 * decimals.
 */
export function PercentInput(props: PercentInputProps): ReactElement {
	return (
		<ConvertedInput
			{...props}
			conversion={PERCENT_CONVERSION}
			inputMode="decimal"
			suffix="%"
		/>
	);
}
