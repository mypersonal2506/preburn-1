import type { ReactElement } from "react";
import { CustomerPicker } from "@/components/pickers/customer-picker";
import { PlanPicker } from "@/components/pickers/plan-picker";
import { Blank } from "@/components/sentence/blank";
import {
	type BlankChoice,
	BlankChoices,
} from "@/features/policies/blank-choices";
import {
	customerChoiceName,
	type PolicyDraft,
} from "@/features/policies/policy-draft";
import type { PolicyLevel } from "@/features/policies/policy-phrases";

interface PolicyWhoBlankProps {
	draft: PolicyDraft;
	planName: string | null;
	invalidMessage: string | undefined;
	onChange: (draft: PolicyDraft) => void;
}

interface WhoWords {
	placeholder: string;
	phrase: string | null;
}

const LEVEL_CHOICES: readonly BlankChoice<PolicyLevel>[] = [
	{ value: "everyone", label: "All customers" },
	{ value: "plan", label: "Customers on a plan" },
	{ value: "customer", label: "One customer" },
];

/**
 * The who blank of a policy sentence: "all customers", a plan's name such
 * as "Creator" before the word customers, or one customer's name. Its
 * popover chooses the level, then the plan or the customer with a picker.
 * Changing the level clears the plan and customer of the other levels.
 */
export function PolicyWhoBlank({
	draft,
	planName,
	invalidMessage,
	onChange,
}: PolicyWhoBlankProps): ReactElement {
	const words = whoWords(draft, planName);
	return (
		<Blank
			placeholder={words.placeholder}
			phrase={words.phrase}
			invalidMessage={invalidMessage}
		>
			<div className="flex flex-col gap-3">
				<BlankChoices
					label="Applies to"
					choices={LEVEL_CHOICES}
					value={draft.level}
					onChoose={(level) => {
						if (level !== draft.level) {
							onChange({ ...draft, level, plan_id: null, customer: null });
						}
					}}
				/>
				{draft.level === "plan" && (
					<PlanPicker
						value={draft.plan_id}
						onChange={(plan) => onChange({ ...draft, plan_id: plan.id })}
					/>
				)}
				{draft.level === "customer" && (
					<CustomerPicker
						value={draft.customer}
						onChange={(customer) =>
							onChange({
								...draft,
								customer: {
									id: customer.id,
									external_id: customer.external_id,
									display_name: customer.display_name,
								},
							})
						}
					/>
				)}
			</div>
		</Blank>
	);
}

function whoWords(draft: PolicyDraft, planName: string | null): WhoWords {
	switch (draft.level) {
		case "everyone":
			return { placeholder: "all customers", phrase: "all customers" };
		case "plan":
			return { placeholder: "which plan", phrase: planName };
		case "customer":
			return {
				placeholder: "which customer",
				phrase:
					draft.customer === null ? null : customerChoiceName(draft.customer),
			};
	}
}
