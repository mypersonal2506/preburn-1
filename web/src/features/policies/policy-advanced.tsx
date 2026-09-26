import { type ReactElement, useId } from "react";
import { HelpTip } from "@/components/help-tip";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import type { EnforcementSettings } from "@/features/policies/policy-phrases";

interface PolicyAdvancedProps {
	settings: EnforcementSettings;
	onChange: (settings: EnforcementSettings) => void;
}

interface SettingSelectProps<Value extends string> {
	label: string;
	help?: string;
	options: readonly SettingOption<Value>[];
	value: Value;
	onChange: (value: Value) => void;
}

interface SettingOption<Value extends string> {
	value: Value;
	label: string;
}

type FallbackOutcome = EnforcementSettings["on_unreachable"];

const ENFORCEMENT_OPTIONS: readonly SettingOption<
	EnforcementSettings["enforcement"]
>[] = [
	{ value: "soft", label: "Soft" },
	{ value: "hard", label: "Hard" },
];

const FALLBACK_OPTIONS: readonly SettingOption<FallbackOutcome>[] = [
	{ value: "allow", label: "Allow" },
	{ value: "deny", label: "Deny" },
];

/**
 * The Advanced settings of a policy: enforcement, what the SDK does when
 * Preburn is unreachable, and what a request that has no price gets.
 */
export function PolicyAdvanced({
	settings,
	onChange,
}: PolicyAdvancedProps): ReactElement {
	return (
		<FieldGroup>
			<SettingSelect
				label="Enforcement"
				help="Hard reserves the ceiling estimate and rechecks limits."
				options={ENFORCEMENT_OPTIONS}
				value={settings.enforcement}
				onChange={(enforcement) => onChange({ ...settings, enforcement })}
			/>
			<SettingSelect
				label="When Preburn is unreachable"
				options={FALLBACK_OPTIONS}
				value={settings.on_unreachable}
				onChange={(onUnreachable) =>
					onChange({ ...settings, on_unreachable: onUnreachable })
				}
			/>
			<SettingSelect
				label="When a request has no price"
				options={FALLBACK_OPTIONS}
				value={settings.on_uncosted}
				onChange={(onUncosted) =>
					onChange({ ...settings, on_uncosted: onUncosted })
				}
			/>
		</FieldGroup>
	);
}

function SettingSelect<Value extends string>({
	label,
	help,
	options,
	value,
	onChange,
}: SettingSelectProps<Value>): ReactElement {
	const triggerId = useId();
	return (
		<Field>
			<div className="flex items-center gap-1">
				<FieldLabel htmlFor={triggerId}>{label}</FieldLabel>
				{help !== undefined && <HelpTip topic={label}>{help}</HelpTip>}
			</div>
			<Select
				value={value}
				onValueChange={(nextValue) => onChange(optionValue(options, nextValue))}
			>
				<SelectTrigger id={triggerId} size="sm">
					<SelectValue />
				</SelectTrigger>
				<SelectContent>
					{options.map((option) => (
						<SelectItem key={option.value} value={option.value}>
							{option.label}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
		</Field>
	);
}

function optionValue<Value extends string>(
	options: readonly SettingOption<Value>[],
	value: string,
): Value {
	const option = options.find((candidate) => candidate.value === value);
	if (option === undefined) {
		throw new Error(`setting option unknown value=${value}`);
	}
	return option.value;
}
