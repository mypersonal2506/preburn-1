import { XIcon } from "lucide-react";
import type { ReactElement } from "react";
import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { CountInput } from "@/features/policies/count-input";
import type { OverrideParameter } from "@/features/policies/override-parameters";
import {
	type OverrideValue,
	parameterLabel,
} from "@/features/policies/policy-phrases";
import { attributeValueLabel } from "@/lib/labels";

interface OverrideEditorProps {
	parameters: readonly OverrideParameter[];
	parameterKey: string;
	value: OverrideValue | null;
	onChange: (parameterKey: string, value: OverrideValue | null) => void;
	onRemove: () => void;
}

/**
 * One override of a policy: the parameter, chosen from parameters, and its
 * value, from the values the parameter takes, or as a whole number within
 * the range of an integer parameter without listed values. A parameter that
 * parameters does not list, such as one set before the route's models
 * changed, keeps only its current value. Choosing another parameter empties
 * the value. The remove button drops the override.
 */
export function OverrideEditor({
	parameters,
	parameterKey,
	value,
	onChange,
	onRemove,
}: OverrideEditorProps): ReactElement {
	const listedParameter = parameters.find(({ key }) => key === parameterKey);
	const parameter = listedParameter ?? {
		key: parameterKey,
		values: value === null ? [] : [value],
		minimum: null,
		maximum: null,
	};
	const choices =
		listedParameter === undefined ? [parameter, ...parameters] : parameters;

	return (
		<div className="flex flex-wrap items-start gap-1.5 text-sm">
			<Select
				value={parameterKey}
				onValueChange={(nextKey) => onChange(nextKey, null)}
			>
				<SelectTrigger size="sm" aria-label="Setting">
					<SelectValue>{parameterLabel(parameterKey)}</SelectValue>
				</SelectTrigger>
				<SelectContent>
					{choices.map(({ key }) => (
						<SelectItem key={key} value={key}>
							{parameterLabel(key)}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			{parameter.values.length > 0 ? (
				<Select
					value={value === null ? "" : String(value)}
					onValueChange={(text) =>
						onChange(parameterKey, listedValue(parameter, text))
					}
				>
					<SelectTrigger size="sm" aria-label="Value">
						<SelectValue placeholder="Choose a value">
							{value === null ? null : attributeValueLabel(parameterKey, value)}
						</SelectValue>
					</SelectTrigger>
					<SelectContent>
						{parameter.values.map((parameterValue) => (
							<SelectItem
								key={String(parameterValue)}
								value={String(parameterValue)}
							>
								{attributeValueLabel(parameterKey, parameterValue)}
							</SelectItem>
						))}
					</SelectContent>
				</Select>
			) : (
				<div className="flex w-32 flex-col gap-1">
					<CountInput
						aria-label="Value"
						value={value === null ? null : String(value)}
						onChange={(count) =>
							onChange(parameterKey, count === null ? null : Number(count))
						}
					/>
					{parameter.minimum !== null && parameter.maximum !== null && (
						<p className="text-muted-foreground text-xs">
							{parameter.minimum} to {parameter.maximum}
						</p>
					)}
				</div>
			)}
			<Button
				type="button"
				variant="ghost"
				size="icon-xs"
				aria-label="Remove setting"
				onClick={onRemove}
			>
				<XIcon />
			</Button>
		</div>
	);
}

function listedValue(
	parameter: OverrideParameter,
	text: string,
): OverrideValue {
	const value = parameter.values.find(
		(parameterValue) => String(parameterValue) === text,
	);
	if (value === undefined) {
		throw new Error(`override value unknown parameter=${parameter.key}`);
	}
	return value;
}
