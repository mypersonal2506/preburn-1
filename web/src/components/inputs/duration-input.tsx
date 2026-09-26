import type { ReactElement } from "react";
import {
	ConvertedInput,
	type TextConversion,
	type TypedInputProps,
} from "@/components/inputs/converted-input";
import { Button } from "@/components/ui/button";
import { DecimalInputError } from "@/lib/decimal";

/** Props of DurationInput. */
export type DurationInputProps = TypedInputProps<number>;

interface DurationPreset {
	label: string;
	seconds: number;
}

const DURATION_PRESETS: readonly DurationPreset[] = [
	{ label: "30 sec", seconds: 30 },
	{ label: "1 min", seconds: 60 },
	{ label: "5 min", seconds: 300 },
	{ label: "10 min", seconds: 600 },
	{ label: "30 min", seconds: 1_800 },
	{ label: "1 hour", seconds: 3_600 },
];
const SECONDS_DIGITS_MAXIMUM = 9;
const SECONDS_TEXT_PATTERN = /^(\d+)(\.\d+)?$/;
const LEADING_ZEROS_PATTERN = /^0+/;

const SECONDS_CONVERSION: TextConversion<number> = {
	format: String,
	parse: parseSeconds,
};

/**
 * A duration in whole seconds, picked from presets between 30 seconds and 1
 * hour or typed as custom seconds. The preset matching the value shows as
 * pressed. Rejects text that is not a whole number and more than 9 digits.
 */
export function DurationInput(props: DurationInputProps): ReactElement {
	return (
		<div className="flex flex-col gap-2">
			<div className="flex flex-wrap gap-1">
				{DURATION_PRESETS.map((preset) => {
					const pressed = preset.seconds === props.value;
					return (
						<Button
							key={preset.seconds}
							type="button"
							size="sm"
							variant={pressed ? "secondary" : "outline"}
							aria-pressed={pressed}
							onClick={() => props.onChange(preset.seconds)}
						>
							{preset.label}
						</Button>
					);
				})}
			</div>
			<ConvertedInput
				{...props}
				conversion={SECONDS_CONVERSION}
				inputMode="numeric"
				suffix="seconds"
			/>
		</div>
	);
}

function parseSeconds(text: string): number {
	const match = SECONDS_TEXT_PATTERN.exec(text.trim());
	if (match === null) {
		throw new DecimalInputError("invalid_format", null);
	}
	const [, digits = "", fraction] = match;
	if (fraction !== undefined) {
		throw new DecimalInputError("too_many_decimals", 0);
	}
	if (
		digits.replace(LEADING_ZEROS_PATTERN, "").length > SECONDS_DIGITS_MAXIMUM
	) {
		throw new DecimalInputError(
			"too_many_integer_digits",
			SECONDS_DIGITS_MAXIMUM,
		);
	}
	return Number(digits);
}
