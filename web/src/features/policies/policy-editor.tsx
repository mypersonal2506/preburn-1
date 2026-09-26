import { type ReactElement, useState } from "react";
import { SentenceCard } from "@/components/sentence/sentence-card";
import { PolicyAdvanced } from "@/features/policies/policy-advanced";
import { NAME_BLANK } from "@/features/policies/policy-blanks";
import {
	draftProblems,
	type PolicyDraft,
	sameDocument,
	suggestPolicyName,
} from "@/features/policies/policy-draft";
import { PolicyFacts } from "@/features/policies/policy-facts";
import { enforcementSummary } from "@/features/policies/policy-phrases";
import {
	policyProblemView,
	serverProblems,
} from "@/features/policies/policy-problems";
import { PolicySentence } from "@/features/policies/policy-sentence";
import { usePlanNames } from "@/features/policies/use-policy-names";

interface PolicyEditorProps {
	initialDraft: PolicyDraft;
	initialOpenedBlank: string | null;
	policyId: string | null;
	saving: boolean;
	saveError: unknown;
	onSave: (draft: PolicyDraft, name: string) => void;
}

/**
 * The SentenceCard of a policy: the sentence, its facts and error list, the
 * name, suggested from the sentence until the member types one, the
 * Advanced settings and the primary action. Without policyId it creates a
 * policy and shows the status switch. With policyId it edits that policy
 * and shows Save and Discard while the document differs from initialDraft,
 * leaving the status to the page menu. Saving with an empty blank lists
 * the empty blanks instead of calling onSave, and they stay listed while
 * edited. saveError is the failed save's error, null otherwise, and its
 * problems show until the next edit. Remount it with a new key to start
 * from a new initialDraft.
 */
export function PolicyEditor({
	initialDraft,
	initialOpenedBlank,
	policyId,
	saving,
	saveError,
	onSave,
}: PolicyEditorProps): ReactElement {
	const [draft, setDraft] = useState(initialDraft);
	const [submittedDraft, setSubmittedDraft] = useState<PolicyDraft | null>(
		null,
	);
	const planNames = usePlanNames(draft.plan_id === null ? [] : [draft.plan_id]);
	const planName =
		draft.plan_id === null ? null : (planNames?.get(draft.plan_id) ?? null);
	const name = draft.name ?? suggestPolicyName(draft, planName);
	const problems =
		submittedDraft === null
			? []
			: [
					...draftProblems(draft),
					...(saveError !== null && draft === submittedDraft
						? serverProblems(saveError)
						: []),
				];
	const problemView = policyProblemView(problems, draft);

	function submit(): void {
		setSubmittedDraft(draft);
		if (draftProblems(draft).length === 0) {
			onSave(draft, name);
		}
	}

	return (
		<SentenceCard
			sentence={
				<PolicySentence
					draft={draft}
					planName={planName}
					problems={problemView}
					initialOpenedBlank={initialOpenedBlank}
					onChange={setDraft}
				/>
			}
			facts={
				<PolicyFacts draft={draft} planName={planName} policyId={policyId} />
			}
			errors={problemView.messages}
			name={{
				value: name,
				onChange: (nextName) => setDraft({ ...draft, name: nextName }),
				invalid: problemView.blankMessage(NAME_BLANK) !== undefined,
			}}
			status={
				policyId === null
					? {
							active: draft.status === "active",
							onActiveChange: (active) =>
								setDraft({ ...draft, status: active ? "active" : "disabled" }),
						}
					: undefined
			}
			advanced={{
				summary: enforcementSummary(draft),
				content: (
					<PolicyAdvanced
						settings={draft}
						onChange={(settings) => setDraft({ ...draft, ...settings })}
					/>
				),
			}}
			action={
				policyId === null
					? { kind: "create", label: "Create policy" }
					: {
							kind: "edit",
							dirty: !sameDocument(draft, initialDraft),
							onDiscard: () => {
								setDraft(initialDraft);
								setSubmittedDraft(null);
							},
						}
			}
			pending={saving}
			onSubmit={submit}
		/>
	);
}
