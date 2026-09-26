import { evaluate, useStore } from "@tanstack/react-form";
import { type ReactElement, type ReactNode, useId, useState } from "react";
import type { PlanResponse } from "@/client";
import { zPlanResponse } from "@/client/zod.gen";
import { useAppForm } from "@/components/form/app-form";
import { AmountInput } from "@/components/inputs/amount-input";
import { PercentInput } from "@/components/inputs/percent-input";
import { Blank } from "@/components/sentence/blank";
import { SentenceCard } from "@/components/sentence/sentence-card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
	draftFromPlan,
	holdTimesSummary,
	type PlanDraft,
	type PlanMode,
} from "@/features/plans/plan-draft";
import { PlanHoldTimes } from "@/features/plans/plan-hold-times";
import {
	type PlanField,
	planDraftProblems,
	planRequestProblem,
} from "@/features/plans/plan-problems";
import { formatMoney, formatPercent } from "@/lib/format";

/**
 * How the plan sentence saves a draft without problems: `save` sends it and
 * calls onSaved with the plan the API returns, `pending` is true while a
 * save runs, and `error` is the error of the last save, null when there is
 * none.
 */
export interface PlanSave {
	pending: boolean;
	error: unknown;
	save: (draft: PlanDraft, onSaved: (plan: PlanResponse) => void) => void;
}

/** Props of PlanSentence. */
export interface PlanSentenceProps {
	savedDraft: PlanDraft;
	action: "create" | "edit";
	facts: ReactNode;
	planSave: PlanSave;
}

interface PlanModeChoiceProps {
	mode: PlanMode;
	onModeChange: (mode: PlanMode) => void;
}

interface ModeWords {
	label: string;
	verb: string;
	placeholder: string;
	ending: string;
}

type PlanMessages = Partial<Record<PlanField, string>>;

const PLAN_MODES: readonly PlanMode[] = ["margin_target", "fixed_allowance"];
const MODE_WORDS: Record<PlanMode, ModeWords> = {
	margin_target: {
		label: "Margin target",
		verb: "keeps",
		placeholder: "what share",
		ending: "of revenue as margin.",
	},
	fixed_allowance: {
		label: "Fixed allowance",
		verb: "customers get",
		placeholder: "a fixed amount",
		ending: "of AI usage each period.",
	},
};
const PLAN_FIELDS: readonly PlanField[] = ["name", "rule", "hold_times"];
const NAME_PLACEHOLDER = "This plan";

/**
 * The plan sentence on a SentenceCard: "[Creator] keeps [40.0%] of revenue
 * as margin." or "[Free] customers get [a fixed $2.00] of AI usage each
 * period." The name blank edits the name, so the card has no name input.
 * The rule blank picks the mode and types its target margin or allowance, and
 * the draft keeps both, so switching mode back shows the earlier value.
 * Advanced holds the hold times. Saving checks the draft with
 * planDraftProblems first, and from the first attempt on the problems show
 * at their blanks and in the error list, updating as the draft changes. A
 * failed save shows at the blanks until the draft changes. A successful save
 * resets the sentence to the saved plan. `action` edit shows Save and
 * Discard only while the draft differs from savedDraft.
 */
export function PlanSentence({
	savedDraft,
	action,
	facts,
	planSave,
}: PlanSentenceProps): ReactElement {
	const [sentDraft, setSentDraft] = useState<PlanDraft | null>(null);
	const form = useAppForm({
		defaultValues: savedDraft,
		onSubmit: ({ value, formApi }) => {
			if (Object.keys(planDraftProblems(value).fields).length > 0) {
				return;
			}
			setSentDraft(value);
			planSave.save(value, (plan) => formApi.reset(draftFromPlan(plan)));
		},
	});
	const draft = useStore(form.store, (state) => state.values);
	const submitted = useStore(
		form.store,
		(state) => state.submissionAttempts > 0,
	);

	const draftProblems = planDraftProblems(draft);
	const requestProblem = planRequestProblem(
		sentDraft !== null && evaluate(draft, sentDraft) ? planSave.error : null,
	);
	const messages: PlanMessages = {};
	for (const field of PLAN_FIELDS) {
		const message =
			(submitted ? draftProblems.fields[field] : undefined) ??
			requestProblem.fields[field]?.[0];
		if (message !== undefined) {
			messages[field] = message;
		}
	}
	const errors = [
		...PLAN_FIELDS.flatMap((field) => messages[field] ?? []),
		...(requestProblem.form ?? []),
	];
	const words = MODE_WORDS[draft.mode];

	return (
		<SentenceCard
			sentence={
				<>
					<form.Field name="name">
						{(field) => (
							<Blank
								placeholder={NAME_PLACEHOLDER}
								phrase={
									field.state.value.trim() === "" ? null : field.state.value
								}
								invalidMessage={messages.name}
							>
								<Input
									aria-label="Plan name"
									value={field.state.value}
									onChange={(event) => field.handleChange(event.target.value)}
								/>
							</Blank>
						)}
					</form.Field>{" "}
					{words.verb}{" "}
					<form.Field name="mode">
						{(field) => (
							<Blank
								placeholder={words.placeholder}
								phrase={rulePhrase(draft)}
								invalidMessage={messages.rule}
							>
								<div className="flex flex-col gap-3">
									<PlanModeChoice
										mode={field.state.value}
										onModeChange={field.handleChange}
									/>
									{field.state.value === "margin_target" ? (
										<form.Field name="target_margin">
											{(targetMarginField) => (
												<PercentInput
													aria-label="Target margin"
													value={targetMarginField.state.value}
													onChange={targetMarginField.handleChange}
												/>
											)}
										</form.Field>
									) : (
										<form.Field name="allowance">
											{(allowanceField) => (
												<AmountInput
													aria-label="Allowance"
													value={allowanceField.state.value}
													onChange={allowanceField.handleChange}
												/>
											)}
										</form.Field>
									)}
								</div>
							</Blank>
						)}
					</form.Field>{" "}
					{words.ending}
				</>
			}
			facts={facts}
			errors={errors}
			advanced={{
				summary: holdTimesSummary(draft.hold_times),
				content: (
					<form.Field name="hold_times">
						{(field) => (
							<PlanHoldTimes
								rows={field.state.value}
								problems={submitted ? draftProblems.holdTimes : []}
								onChange={field.handleChange}
							/>
						)}
					</form.Field>
				),
			}}
			action={
				action === "create"
					? { kind: "create", label: "Create plan" }
					: {
							kind: "edit",
							dirty: !evaluate(draft, savedDraft),
							onDiscard: () => form.reset(),
						}
			}
			pending={planSave.pending}
			onSubmit={() => void form.handleSubmit()}
		/>
	);
}

function PlanModeChoice({
	mode,
	onModeChange,
}: PlanModeChoiceProps): ReactElement {
	const idPrefix = useId();
	return (
		<RadioGroup
			aria-label="Rule"
			value={mode}
			onValueChange={(value) =>
				onModeChange(zPlanResponse.shape.mode.parse(value))
			}
		>
			{PLAN_MODES.map((planMode) => {
				const itemId = `${idPrefix}-${planMode}`;
				return (
					<div key={planMode} className="flex items-center gap-2">
						<RadioGroupItem id={itemId} value={planMode} />
						<Label htmlFor={itemId}>{MODE_WORDS[planMode].label}</Label>
					</div>
				);
			})}
		</RadioGroup>
	);
}

function rulePhrase(draft: PlanDraft): string | null {
	if (draft.mode === "margin_target") {
		return draft.target_margin === null
			? null
			: formatPercent(draft.target_margin);
	}
	return draft.allowance === null
		? null
		: `a fixed ${formatMoney(draft.allowance)}`;
}
