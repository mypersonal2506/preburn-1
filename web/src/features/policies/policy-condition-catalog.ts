import type { ComponentType } from "react";
import { AmountInput } from "@/components/inputs/amount-input";
import type { TypedInputProps } from "@/components/inputs/converted-input";
import { PaceInput } from "@/components/inputs/pace-input";
import { PercentInput } from "@/components/inputs/percent-input";
import type {
	ConditionCatalog,
	ConditionSignal,
} from "@/components/sentence/condition-panel";
import { CountInput } from "@/features/policies/count-input";
import {
	POLICY_SIGNALS,
	type PolicySignal,
	type SignalUnit,
	signalPhrase,
	signalUnit,
} from "@/features/policies/policy-phrases";

const [FIRST_SIGNAL, ...OTHER_SIGNALS] = POLICY_SIGNALS;

const UNIT_INPUTS: Record<
	SignalUnit,
	ComponentType<TypedInputProps<string>>
> = {
	amount: AmountInput,
	pace: PaceInput,
	percent: PercentInput,
	count: CountInput,
};

/**
 * Every policy signal with its phrase and the typed input for its values,
 * such as AmountInput for allowance left, in the order the sentence builder
 * offers them.
 */
export const POLICY_CONDITION_CATALOG: ConditionCatalog<PolicySignal> = [
	conditionSignal(FIRST_SIGNAL),
	...OTHER_SIGNALS.map(conditionSignal),
];

/** The typed input for the condition values of signal. */
export function signalInput(
	signal: PolicySignal,
): ComponentType<TypedInputProps<string>> {
	return UNIT_INPUTS[signalUnit(signal)];
}

function conditionSignal(signal: PolicySignal): ConditionSignal<PolicySignal> {
	return { signal, phrase: signalPhrase(signal), input: signalInput(signal) };
}
