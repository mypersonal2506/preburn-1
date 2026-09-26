import { useQuery } from "@tanstack/react-query";
import type { ReactElement } from "react";
import type { MeterDescription } from "@/client";
import { listPricingMetersOptions } from "@/client/@tanstack/react-query.gen";
import {
	type PickerFieldProps,
	type PickerOption,
	PickerShell,
} from "@/components/pickers/picker-shell";
import { meterLabel } from "@/lib/labels";

/** A meter key, such as input_tokens. */
export type Meter = MeterDescription["meter"];

/** Props of MeterPicker. */
export interface MeterPickerProps extends PickerFieldProps {
	value: Meter | null;
	onChange: (meter: Meter) => void;
}

/**
 * Picks one of the meters the pricing API lists. Options show the meter's
 * name, such as "input tokens", and the search also matches the key.
 */
export function MeterPicker({
	value,
	onChange,
	...fieldProps
}: MeterPickerProps): ReactElement {
	const metersQuery = useQuery(listPricingMetersOptions());

	return (
		<PickerShell
			{...fieldProps}
			placeholder="Select a meter"
			searchPlaceholder="Search meters"
			emptyText="No meters found"
			selectedValue={value}
			selectedLabel={value === null ? null : meterLabel(value)}
			groups={[{ options: metersQuery.data?.items.map(meterOption) ?? [] }]}
			loading={metersQuery.isPending}
			error={metersQuery.error}
			onSelect={onChange}
		/>
	);
}

function meterOption({ meter }: MeterDescription): PickerOption<Meter> {
	return {
		value: meter,
		label: meterLabel(meter),
		detail: null,
		choice: meter,
	};
}
