import { useQuery } from "@tanstack/react-query";
import { type ReactElement, useState } from "react";
import type { ModelParametersResponse } from "@/client";
import { getParameterMappingsOptions } from "@/client/@tanstack/react-query.gen";
import { AddClause } from "@/components/sentence/add-clause";
import { Button } from "@/components/ui/button";
import {
	capOverrideParameters,
	type OverrideParameter,
	routeOverrideParameters,
} from "@/features/policies/override-parameters";
import { FEATURE_BLANK, WHO_BLANK } from "@/features/policies/policy-blanks";
import { policyClauses } from "@/features/policies/policy-clauses";
import { PolicyConditions } from "@/features/policies/policy-conditions";
import type {
	PolicyDraft,
	PolicyDraftAction,
} from "@/features/policies/policy-draft";
import { PolicyFeatureBlank } from "@/features/policies/policy-feature-blank";
import { PolicyOutcome } from "@/features/policies/policy-outcome";
import type { PolicyProblemView } from "@/features/policies/policy-problems";
import { PolicyWhoBlank } from "@/features/policies/policy-who-blank";

interface PolicySentenceProps {
	draft: PolicyDraft;
	planName: string | null;
	problems: PolicyProblemView;
	initialOpenedBlank: string | null;
	onChange: (draft: PolicyDraft) => void;
}

interface SettingsLoadErrorProps {
	onRetry: () => void;
}

/**
 * The policy grammar on the sentence builder: "For {who} using {feature},
 * {when} {then}." followed by the (+) menu of optional clauses, rendered
 * from draft. A clause just added opens its first blank, as does
 * initialOpenedBlank when the sentence mounts. The settings a route or cap
 * can override come from the parameter mappings. When they fail to load,
 * the sentence and the cap's settings blank say so with Try again.
 */
export function PolicySentence({
	draft,
	planName,
	problems,
	initialOpenedBlank,
	onChange,
}: PolicySentenceProps): ReactElement {
	const [openedBlank, setOpenedBlank] = useState(initialOpenedBlank);
	const overridable =
		draft.action.outcome === "route" || draft.action.outcome === "cap";
	const mappingsQuery = useQuery({
		...getParameterMappingsOptions(),
		enabled: overridable,
	});
	const parameters = overrideParameters(
		draft.action,
		mappingsQuery.data?.models ?? [],
	);
	const settingsLoadError = mappingsQuery.isError ? (
		<SettingsLoadError onRetry={() => void mappingsQuery.refetch()} />
	) : null;

	function change(nextDraft: PolicyDraft): void {
		setOpenedBlank(null);
		onChange(nextDraft);
	}

	function add(nextDraft: PolicyDraft, blank: string): void {
		setOpenedBlank(blank);
		onChange(nextDraft);
	}

	return (
		<>
			For{" "}
			<PolicyWhoBlank
				draft={draft}
				planName={planName}
				invalidMessage={problems.blankMessage(WHO_BLANK)}
				onChange={change}
			/>
			{draft.level === "plan" && " customers"} using{" "}
			<PolicyFeatureBlank
				feature={draft.feature}
				invalidMessage={problems.blankMessage(FEATURE_BLANK)}
				onChange={(feature) => change({ ...draft, feature })}
			/>
			,{" "}
			<PolicyConditions
				when={draft.when}
				problems={problems}
				openedBlank={openedBlank}
				onChange={(when) => change({ ...draft, when })}
				onAdd={(when, blank) => add({ ...draft, when }, blank)}
			/>{" "}
			<PolicyOutcome
				action={draft.action}
				parameters={parameters}
				problems={problems}
				openedBlank={openedBlank}
				settingsLoadError={settingsLoadError}
				onChange={(action) => change({ ...draft, action })}
				onAdd={(action, blank) => add({ ...draft, action }, blank)}
			/>
			.{" "}
			<AddClause
				clauses={policyClauses(draft, parameters).map((clause) => ({
					label: clause.label,
					onAdd: () => add(clause.draft, clause.openedBlank),
				}))}
			/>
			{settingsLoadError}
		</>
	);
}

function SettingsLoadError({ onRetry }: SettingsLoadErrorProps): ReactElement {
	return (
		<span className="inline-flex items-center gap-1 text-sm">
			<span role="alert" className="text-destructive">
				Settings did not load
			</span>
			<Button type="button" variant="link" size="sm" onClick={onRetry}>
				Try again
			</Button>
		</span>
	);
}

function overrideParameters(
	action: PolicyDraftAction,
	models: readonly ModelParametersResponse[],
): OverrideParameter[] {
	switch (action.outcome) {
		case "route":
			return routeOverrideParameters(models, action.route_chain);
		case "cap":
			return capOverrideParameters(models);
		case "allow":
		case "deny":
			return [];
	}
}
