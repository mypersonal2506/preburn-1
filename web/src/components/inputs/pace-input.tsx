import type { ReactElement } from "react";
import {
	ConvertedInput,
	type TextConversion,
	type TypedInputProps,
} from "@/components/inputs/converted-input";
import { DecimalInputError, paceToRatio, ratioToPace } from "@/lib/decimal";

/** Props of PaceInput. */
export type PaceInputProps = TypedInputProps<string>;

const PACE_DECIMALS = 1;
const ONE_DECIMAL_RATIO_PATTERN = /\.\d000$/;

const PACE_CONVERSION: TextConversion<string> = {
	format: ratioToPace,
	parse: parsePace,
};

/**
 * A pace typed with at most one decimal, such as "2.5" before its "x",
 * reported as the exact API ratio string, "2.5000". Rejects text that is not
 * a decimal and more than one decimal.
 */
export function PaceInput(props: PaceInputProps): ReactElement {
	return (
		<ConvertedInput
			{...props}
			conversion={PACE_CONVERSION}
			inputMode="decimal"
			suffix="x"
		/>
	);
}

function parsePace(pace: string): string {
	const ratio = paceToRatio(pace);
	if (!ONE_DECIMAL_RATIO_PATTERN.test(ratio)) {
		throw new DecimalInputError("too_many_decimals", PACE_DECIMALS);
	}
	return ratio;
}
