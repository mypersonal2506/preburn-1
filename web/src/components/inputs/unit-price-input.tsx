import type { ReactElement } from "react";
import type { MeterDescription } from "@/client";
import {
	ConvertedInput,
	type TextConversion,
	type TypedInputProps,
} from "@/components/inputs/converted-input";
import {
	displayedPriceToUnitPrice,
	type UnitPrice,
	unitPriceToDisplayedPrice,
} from "@/lib/decimal";
import { meterPriceUnitLabel } from "@/lib/labels";

/** Props of UnitPriceInput. */
export interface UnitPriceInputProps extends TypedInputProps<UnitPrice> {
	meter: MeterDescription["meter"];
}

/**
 * A USD price typed per displayed unit of the meter, per 1M tokens for token
 * meters and per unit for the others, such as "3.00" per 1M input tokens,
 * reported as the exact API unit_price and unit_quantity,
 * `{unit_price: "3", unit_quantity: 1000000}`. Rejects text that is not a
 * decimal, a price below zero, more than 9 decimals and more than 9 integer
 * digits. A new meter shows the value again in that meter's displayed unit,
 * or empties the field and reports null when the price needs more than 9
 * decimals or 9 integer digits there.
 */
export function UnitPriceInput({
	meter,
	...props
}: UnitPriceInputProps): ReactElement {
	const conversion: TextConversion<UnitPrice> = {
		format: (price) =>
			unitPriceToDisplayedPrice(price.unit_price, price.unit_quantity, meter),
		parse: (price) => displayedPriceToUnitPrice(price, meter),
	};
	return (
		<ConvertedInput
			key={meter}
			{...props}
			conversion={conversion}
			inputMode="decimal"
			prefix="$"
			suffix={`per ${meterPriceUnitLabel(meter)}`}
		/>
	);
}
