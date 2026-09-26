import { useQuery } from "@tanstack/react-query";
import type { ReactElement } from "react";
import type { ParameterResponse } from "@/client";
import { getParameterMappingsOptions } from "@/client/@tanstack/react-query.gen";
import {
	type PickerFieldProps,
	type PickerOption,
	PickerShell,
} from "@/components/pickers/picker-shell";
import { type AttributeValue, attributeValueLabel } from "@/lib/labels";

/** One parameter value, keyed by the name policy overrides use. */
export interface AttributeSelection {
	key: string;
	value: AttributeValue;
}

/** Props of AttributePicker. */
export interface AttributePickerProps extends PickerFieldProps {
	provider: string;
	model: string;
	value: AttributeSelection | null;
	onChange: (selection: AttributeSelection) => void;
}

/**
 * Picks a value of one of the overridable parameters of a provider model,
 * from the parameter mappings: both values of a boolean parameter and the
 * allowed values of the others. An integer parameter with only a minimum and
 * maximum has no values to list. Options read like "without audio" or
 * "720p", and the search also matches the key.
 */
export function AttributePicker({
	provider,
	model,
	value,
	onChange,
	...fieldProps
}: AttributePickerProps): ReactElement {
	const mappingsQuery = useQuery(getParameterMappingsOptions());

	const modelParameters = mappingsQuery.data?.models.find(
		(mapping) => mapping.provider === provider && mapping.model === model,
	);

	return (
		<PickerShell
			{...fieldProps}
			placeholder="Select a value"
			searchPlaceholder="Search values"
			emptyText="No values found"
			selectedValue={value === null ? null : selectionValue(value)}
			selectedLabel={
				value === null ? null : attributeValueLabel(value.key, value.value)
			}
			groups={[
				{
					options: Object.entries(modelParameters?.parameters ?? {}).flatMap(
						([key, parameter]) =>
							parameterValues(parameter).map((parameterValue) =>
								selectionOption({ key, value: parameterValue }),
							),
					),
				},
			]}
			loading={mappingsQuery.isPending}
			error={mappingsQuery.error}
			onSelect={onChange}
		/>
	);
}

function parameterValues(parameter: ParameterResponse): AttributeValue[] {
	return parameter.value_type === "boolean"
		? [true, false]
		: parameter.allowed_values;
}

function selectionOption(
	selection: AttributeSelection,
): PickerOption<AttributeSelection> {
	return {
		value: selectionValue(selection),
		label: attributeValueLabel(selection.key, selection.value),
		detail: null,
		choice: selection,
	};
}

function selectionValue(selection: AttributeSelection): string {
	return `${selection.key}:${String(selection.value)}`;
}
