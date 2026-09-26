import { Fragment, type ReactElement } from "react";
import type { PolicyCondition, PolicyConditionGroup } from "@/client";
import { zPolicyCondition } from "@/client/zod.gen";
import { Blank } from "@/components/sentence/blank";
import { BlankPhrase } from "@/components/sentence/blank-phrase";
import { ConditionPanel } from "@/components/sentence/condition-panel";
import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import {
	type BlankChoice,
	BlankChoices,
} from "@/features/policies/blank-choices";
import {
	conditionComparisonBlank,
	conditionSignalBlank,
	WHEN_BLANK,
} from "@/features/policies/policy-blanks";
import {
	NEW_CONDITION_OPERATOR,
	writeConditionGroup,
} from "@/features/policies/policy-clauses";
import {
	POLICY_CONDITION_CATALOG,
	signalInput,
} from "@/features/policies/policy-condition-catalog";
import {
	comparisonPhrase,
	conditionGroupPhrase,
	type InlineConditions,
	inlineConditions,
	isAlways,
	matchWord,
	type PolicySignal,
	signalPhrase,
} from "@/features/policies/policy-phrases";
import type { PolicyProblemView } from "@/features/policies/policy-problems";
import { useItemKeys } from "@/features/policies/use-item-keys";
import { conditionOperatorLabel } from "@/lib/labels";

interface PolicyConditionsProps {
	when: PolicyConditionGroup;
	problems: PolicyProblemView;
	openedBlank: string | null;
	onChange: (when: PolicyConditionGroup) => void;
	onAdd: (when: PolicyConditionGroup, openedBlank: string) => void;
}

interface InlineConditionProps {
	condition: PolicyCondition;
	index: number;
	problems: PolicyProblemView;
	openedBlank: string | null;
	onChange: (condition: PolicyCondition) => void;
	onRemove: () => void;
}

type ConditionOperator = PolicyCondition["operator"];

const CONDITION_OPERATORS: readonly ConditionOperator[] = [
	"gt",
	"gte",
	"lt",
	"lte",
	"eq",
	"ne",
];

const SIGNAL_CHOICES: readonly BlankChoice<PolicySignal>[] =
	POLICY_CONDITION_CATALOG.map(({ signal, phrase }) => ({
		value: signal,
		label: phrase,
	}));

/**
 * The when part of a policy sentence. A group that always holds reads
 * "[always]", whose popover adds a first condition on a chosen signal and
 * opens its comparison. One to three plain conditions read inline, "when
 * [pace] is [above 2.0x] [and] ...", where the and or blank toggles the
 * group between all and any and each signal's popover can remove its
 * condition. Removing the last one goes back to always. Any other group
 * reads "when [all of 5 conditions] match", whose wide popover holds the
 * ConditionPanel.
 */
export function PolicyConditions({
	when,
	problems,
	openedBlank,
	onChange,
	onAdd,
}: PolicyConditionsProps): ReactElement {
	const conditionKeys = useItemKeys<PolicyCondition>();
	const whenMessage = problems.blankMessage(WHEN_BLANK);
	if (isAlways(when)) {
		return (
			<Blank
				placeholder="always"
				phrase="always"
				invalidMessage={whenMessage}
				defaultOpen={openedBlank === WHEN_BLANK}
			>
				<BlankChoices
					label="Only when"
					choices={SIGNAL_CHOICES}
					value={null}
					onChoose={(signal) =>
						onAdd(
							{
								all: [{ signal, operator: NEW_CONDITION_OPERATOR, value: "" }],
							},
							conditionComparisonBlank(0),
						)
					}
				/>
			</Blank>
		);
	}
	const inline = inlineConditions(when);
	if (inline === null) {
		return (
			<>
				when{" "}
				<Blank
					placeholder="which conditions"
					phrase={conditionGroupPhrase(when)}
					invalidMessage={whenMessage}
					defaultOpen={openedBlank === WHEN_BLANK}
					wide
				>
					<ConditionPanel
						catalog={POLICY_CONDITION_CATALOG}
						value={when}
						onChange={onChange}
					/>
				</Blank>{" "}
				match,
			</>
		);
	}
	return (
		<>
			when{" "}
			{inline.conditions.map((condition, index) => (
				<Fragment key={conditionKeys.keyOf(condition)}>
					{index > 0 && (
						<>
							{" "}
							<BlankPhrase
								placeholder={matchWord(inline.match)}
								phrase={matchWord(inline.match)}
								aria-invalid={whenMessage !== undefined}
								onClick={() =>
									onChange(
										writeConditionGroup(
											inline.match === "all" ? "any" : "all",
											inline.conditions,
										),
									)
								}
							/>{" "}
						</>
					)}
					<InlineCondition
						condition={condition}
						index={index}
						problems={problems}
						openedBlank={openedBlank}
						onChange={(nextCondition) => {
							conditionKeys.carry(condition, nextCondition);
							onChange(
								writeConditionGroup(
									inline.match,
									inline.conditions.with(index, nextCondition),
								),
							);
						}}
						onRemove={() => onChange(withoutCondition(inline, index))}
					/>
				</Fragment>
			))}
			,
		</>
	);
}

function InlineCondition({
	condition,
	index,
	problems,
	openedBlank,
	onChange,
	onRemove,
}: InlineConditionProps): ReactElement {
	const ValueInput = signalInput(condition.signal);
	const signalBlank = conditionSignalBlank(index);
	const comparisonBlank = conditionComparisonBlank(index);
	return (
		<>
			<Blank
				placeholder="which signal"
				phrase={signalPhrase(condition.signal)}
				invalidMessage={problems.blankMessage(signalBlank)}
				defaultOpen={openedBlank === signalBlank}
			>
				<div className="flex flex-col gap-2">
					<BlankChoices
						label="Signal"
						choices={SIGNAL_CHOICES}
						value={condition.signal}
						onChoose={(signal) => {
							if (signal !== condition.signal) {
								onChange({ ...condition, signal, value: "" });
							}
						}}
					/>
					<Button type="button" variant="outline" size="sm" onClick={onRemove}>
						Remove condition
					</Button>
				</div>
			</Blank>{" "}
			is{" "}
			<Blank
				placeholder="what value"
				phrase={condition.value === "" ? null : comparisonPhrase(condition)}
				invalidMessage={problems.blankMessage(comparisonBlank)}
				defaultOpen={openedBlank === comparisonBlank}
			>
				<div className="flex flex-col gap-2">
					<Select
						value={condition.operator}
						onValueChange={(operator) =>
							onChange({
								...condition,
								operator: zPolicyCondition.shape.operator.parse(operator),
							})
						}
					>
						<SelectTrigger size="sm" aria-label="Comparison">
							<SelectValue>
								{conditionOperatorLabel(condition.operator)}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{CONDITION_OPERATORS.map((operator) => (
								<SelectItem key={operator} value={operator}>
									{conditionOperatorLabel(operator)}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
					<ValueInput
						aria-label="Value"
						value={condition.value === "" ? null : condition.value}
						onChange={(value) => onChange({ ...condition, value: value ?? "" })}
					/>
				</div>
			</Blank>
		</>
	);
}

function withoutCondition(
	inline: InlineConditions,
	index: number,
): PolicyConditionGroup {
	const conditions = inline.conditions.toSpliced(index, 1);
	return conditions.length === 0
		? { all: [] }
		: writeConditionGroup(inline.match, conditions);
}
