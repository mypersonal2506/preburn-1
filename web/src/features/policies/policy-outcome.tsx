import { XIcon } from "lucide-react";
import { Fragment, type ReactElement, type ReactNode } from "react";
import { AmountInput } from "@/components/inputs/amount-input";
import { ModelPicker } from "@/components/pickers/model-picker";
import { Blank } from "@/components/sentence/blank";
import { BlankPhrase } from "@/components/sentence/blank-phrase";
import { Button } from "@/components/ui/button";
import {
	type BlankChoice,
	BlankChoices,
} from "@/features/policies/blank-choices";
import { CountInput } from "@/features/policies/count-input";
import { OverrideEditor } from "@/features/policies/override-editor";
import type { OverrideParameter } from "@/features/policies/override-parameters";
import {
	CAP_SETTINGS_BLANK,
	LIMIT_BLANK,
	OUTCOME_BLANK,
	routeOverrideBlank,
	routeTargetBlank,
} from "@/features/policies/policy-blanks";
import {
	type PolicyDraftAction,
	unpickedRouteTarget,
} from "@/features/policies/policy-draft";
import {
	capOverridePhrase,
	limitPhrase,
	type OverrideValue,
	type PolicyLimit,
	type PolicyOutcome as PolicyOutcomeValue,
	type RouteTarget,
} from "@/features/policies/policy-phrases";
import type { PolicyProblemView } from "@/features/policies/policy-problems";
import { useItemKeys } from "@/features/policies/use-item-keys";
import { attributeValueLabel } from "@/lib/labels";

interface PolicyOutcomeProps {
	action: PolicyDraftAction;
	parameters: readonly OverrideParameter[];
	problems: PolicyProblemView;
	openedBlank: string | null;
	settingsLoadError: ReactNode;
	onChange: (action: PolicyDraftAction) => void;
	onAdd: (action: PolicyDraftAction, openedBlank: string) => void;
}

interface CapSettingsBlankProps {
	action: PolicyDraftAction;
	parameters: readonly OverrideParameter[];
	invalidMessage: string | undefined;
	defaultOpen: boolean;
	settingsLoadError: ReactNode;
	onChange: (action: PolicyDraftAction) => void;
}

interface LimitBlankProps {
	action: PolicyDraftAction;
	limit: PolicyLimit;
	invalidMessage: string | undefined;
	defaultOpen: boolean;
	onChange: (action: PolicyDraftAction) => void;
}

type OverrideEntry = [string, OverrideValue | null];

const OUTCOME_CHOICES: readonly BlankChoice<PolicyOutcomeValue>[] = [
	{ value: "allow", label: "Allow", description: "Run" },
	{ value: "route", label: "Route", description: "Switch" },
	{ value: "cap", label: "Cap", description: "Limit" },
	{ value: "deny", label: "Deny", description: "Block" },
];

const LIMIT_CHOICES: readonly BlankChoice<PolicyLimit["kind"]>[] = [
	{ value: "count", label: "Requests" },
	{ value: "amount", label: "AI cost" },
];

/**
 * The then part of a policy sentence: "[allow] the request", "[deny] the
 * request", "[route] to [model]" with ", or [model] if that has no price"
 * per fallback and a phrase per setting (", at [5s]", ", [without audio]"),
 * or "[cap] to [4s, no audio] and allow at most [20 requests] this period".
 * Choosing route opens the first model, and choosing cap opens the
 * settings. Route and cap keep the settings, allow and deny drop them, and
 * only cap keeps the limit. parameters are the settings the outcome can
 * override, and settingsLoadError, when they failed to load, shows in the
 * cap's settings blank. Route model pickers list the cheapest models first.
 * Choosing another parameter for a route setting reopens its blank under
 * the new parameter.
 */
export function PolicyOutcome({
	action,
	parameters,
	problems,
	openedBlank,
	settingsLoadError,
	onChange,
	onAdd,
}: PolicyOutcomeProps): ReactElement {
	const targetKeys = useItemKeys<RouteTarget>();

	function chooseOutcome(outcome: PolicyOutcomeValue): void {
		if (outcome === action.outcome) {
			return;
		}
		switch (outcome) {
			case "allow":
			case "deny":
				onChange({ outcome, route_chain: [], overrides: {}, limit: null });
				return;
			case "route":
				onAdd(
					{
						outcome,
						route_chain: [unpickedRouteTarget()],
						overrides: action.overrides,
						limit: null,
					},
					routeTargetBlank(0),
				);
				return;
			case "cap":
				onAdd(
					{
						outcome,
						route_chain: [],
						overrides: action.overrides,
						limit: action.limit,
					},
					CAP_SETTINGS_BLANK,
				);
				return;
		}
	}

	const outcomeBlank = (
		<Blank
			placeholder="which outcome"
			phrase={action.outcome}
			invalidMessage={problems.blankMessage(OUTCOME_BLANK)}
			defaultOpen={openedBlank === OUTCOME_BLANK}
		>
			<BlankChoices
				label="Outcome"
				choices={OUTCOME_CHOICES}
				value={action.outcome}
				onChoose={chooseOutcome}
			/>
		</Blank>
	);

	switch (action.outcome) {
		case "allow":
		case "deny":
			return <>{outcomeBlank} the request</>;
		case "route":
			return (
				<>
					{outcomeBlank} to{" "}
					{action.route_chain.map((target, index) => (
						<Fragment key={targetKeys.keyOf(target)}>
							{index > 0 && ", or "}
							<ModelPicker
								value={target.model === "" ? null : target}
								cheapestFirst
								onChange={(model) => {
									const pickedTarget = {
										provider: model.provider,
										model: model.model,
									};
									targetKeys.carry(target, pickedTarget);
									onChange({
										...action,
										route_chain: action.route_chain.with(index, pickedTarget),
									});
								}}
								invalidMessage={problems.blankMessage(routeTargetBlank(index))}
								defaultOpen={openedBlank === routeTargetBlank(index)}
								trigger={(label) => (
									<BlankPhrase placeholder="which model" phrase={label} />
								)}
							/>
							{index > 0 && (
								<>
									{" "}
									if that has no price
									<Button
										type="button"
										variant="ghost"
										size="icon-xs"
										aria-label="Remove fallback"
										onClick={() =>
											onChange({
												...action,
												route_chain: action.route_chain.toSpliced(index, 1),
											})
										}
									>
										<XIcon />
									</Button>
								</>
							)}
						</Fragment>
					))}
					{overrideEntries(action).map(([key, value], index) => (
						<Fragment key={key}>
							{value === null || typeof value === "boolean" ? ", " : ", at "}
							<Blank
								placeholder="which setting"
								phrase={value === null ? null : attributeValueLabel(key, value)}
								invalidMessage={problems.blankMessage(routeOverrideBlank(key))}
								defaultOpen={openedBlank === routeOverrideBlank(key)}
							>
								<OverrideEditor
									parameters={unsetParameters(parameters, action, key)}
									parameterKey={key}
									value={value}
									onChange={(nextKey, nextValue) =>
										onAdd(
											{
												...action,
												overrides: replaceOverride(
													action,
													index,
													nextKey,
													nextValue,
												),
											},
											routeOverrideBlank(nextKey),
										)
									}
									onRemove={() =>
										onChange({
											...action,
											overrides: withoutOverride(action, key),
										})
									}
								/>
							</Blank>
						</Fragment>
					))}
				</>
			);
		case "cap":
			return (
				<>
					{outcomeBlank}
					{(overrideEntries(action).length > 0 || action.limit === null) && (
						<>
							{" "}
							to{" "}
							<CapSettingsBlank
								action={action}
								parameters={parameters}
								invalidMessage={problems.blankMessage(CAP_SETTINGS_BLANK)}
								defaultOpen={openedBlank === CAP_SETTINGS_BLANK}
								settingsLoadError={settingsLoadError}
								onChange={onChange}
							/>
						</>
					)}
					{action.limit !== null && (
						<>
							{" "}
							and allow at most{" "}
							<LimitBlank
								action={action}
								limit={action.limit}
								invalidMessage={problems.blankMessage(LIMIT_BLANK)}
								defaultOpen={openedBlank === LIMIT_BLANK}
								onChange={onChange}
							/>{" "}
							this period
						</>
					)}
				</>
			);
	}
}

function CapSettingsBlank({
	action,
	parameters,
	invalidMessage,
	defaultOpen,
	settingsLoadError,
	onChange,
}: CapSettingsBlankProps): ReactElement {
	const entries = overrideEntries(action);
	const [unsetParameter] = unsetParameters(parameters, action, null);
	return (
		<Blank
			placeholder="which settings"
			phrase={capSettingsPhrase(entries)}
			invalidMessage={invalidMessage}
			defaultOpen={defaultOpen}
		>
			<div className="flex flex-col gap-2">
				{entries.map(([key, value], index) => (
					<OverrideEditor
						key={key}
						parameters={unsetParameters(parameters, action, key)}
						parameterKey={key}
						value={value}
						onChange={(nextKey, nextValue) =>
							onChange({
								...action,
								overrides: replaceOverride(action, index, nextKey, nextValue),
							})
						}
						onRemove={() =>
							onChange({ ...action, overrides: withoutOverride(action, key) })
						}
					/>
				))}
				{unsetParameter !== undefined && (
					<Button
						type="button"
						variant="ghost"
						size="sm"
						onClick={() =>
							onChange({
								...action,
								overrides: { ...action.overrides, [unsetParameter.key]: null },
							})
						}
					>
						Add setting
					</Button>
				)}
				{settingsLoadError}
			</div>
		</Blank>
	);
}

function LimitBlank({
	action,
	limit,
	invalidMessage,
	defaultOpen,
	onChange,
}: LimitBlankProps): ReactElement {
	const LimitInput = limit.kind === "count" ? CountInput : AmountInput;
	return (
		<Blank
			placeholder="how much"
			phrase={limit.value === "" ? null : limitPhrase(limit)}
			invalidMessage={invalidMessage}
			defaultOpen={defaultOpen}
		>
			<div className="flex flex-col gap-2">
				<BlankChoices
					label="Limit"
					choices={LIMIT_CHOICES}
					value={limit.kind}
					onChoose={(kind) => {
						if (kind !== limit.kind) {
							onChange({ ...action, limit: { kind, value: "" } });
						}
					}}
				/>
				<LimitInput
					key={limit.kind}
					aria-label="Most per period"
					value={limit.value === "" ? null : limit.value}
					onChange={(value) =>
						onChange({ ...action, limit: { ...limit, value: value ?? "" } })
					}
				/>
				<Button
					type="button"
					variant="outline"
					size="sm"
					onClick={() => onChange({ ...action, limit: null })}
				>
					Remove limit
				</Button>
			</div>
		</Blank>
	);
}

function overrideEntries(action: PolicyDraftAction): OverrideEntry[] {
	return Object.entries(action.overrides);
}

function unsetParameters(
	parameters: readonly OverrideParameter[],
	action: PolicyDraftAction,
	currentKey: string | null,
): OverrideParameter[] {
	return parameters.filter(
		({ key }) => key === currentKey || !Object.hasOwn(action.overrides, key),
	);
}

function replaceOverride(
	action: PolicyDraftAction,
	index: number,
	key: string,
	value: OverrideValue | null,
): Record<string, OverrideValue | null> {
	return Object.fromEntries(overrideEntries(action).with(index, [key, value]));
}

function withoutOverride(
	action: PolicyDraftAction,
	key: string,
): Record<string, OverrideValue | null> {
	return Object.fromEntries(
		overrideEntries(action).filter(([entryKey]) => entryKey !== key),
	);
}

function capSettingsPhrase(entries: readonly OverrideEntry[]): string | null {
	const phrases = entries.flatMap(([key, value]) =>
		value === null ? [] : [capOverridePhrase(key, value)],
	);
	return phrases.length === 0 || phrases.length < entries.length
		? null
		: phrases.join(", ");
}
