import { Link } from "@tanstack/react-router";
import type { DecisionDetailResponse, PolicyResponse } from "@/client";
import { FeatureLabel } from "@/components/feature-label";
import { KeyValueList } from "@/components/key-value-list";
import { ModelLabel } from "@/components/model-label";
import type { ModelReference } from "@/components/pickers/model-picker";
import { SectionCard } from "@/components/section-card";
import { attributesLabel } from "@/features/decisions/decision-labels";

interface DecisionRequestCardProps {
	decision: DecisionDetailResponse;
	policy: PolicyResponse | undefined;
	modelDisplayName: (model: ModelReference) => string | null;
}

const LOADING_TEXT = "Loading";

/**
 * What the check was asked and what it decided to run: the customer's user,
 * the feature, the requested model, the route target, the request's
 * attributes, the overrides the decision set, and the matched policy with
 * its version at the check, linking to the policy. policy is the matched
 * policy once it has loaded.
 */
export function DecisionRequestCard({
	decision,
	policy,
	modelDisplayName,
}: DecisionRequestCardProps) {
	const requestedModel = {
		provider: decision.requested_provider,
		model: decision.requested_model,
	};
	return (
		<SectionCard title="Request">
			<KeyValueList
				items={[
					{
						label: "Customer user",
						value: decision.customer_user_external_id,
					},
					{
						label: "Feature",
						value: <FeatureLabel feature={decision.feature} />,
					},
					{
						label: "Requested model",
						value: (
							<ModelLabel
								provider={requestedModel.provider}
								model={requestedModel.model}
								displayName={modelDisplayName(requestedModel)}
							/>
						),
					},
					{
						label: "Routed to",
						value: decision.outcome === "route" && (
							<ModelLabel
								provider={decision.provider}
								model={decision.model}
								displayName={modelDisplayName(decision)}
							/>
						),
					},
					{ label: "Attributes", value: attributesLabel(decision.attributes) },
					{ label: "Overrides", value: attributesLabel(decision.overrides) },
					{
						label: "Policy",
						value: decision.matched_policy_id !== null && (
							<Link
								to="/policies/$policyId"
								params={{ policyId: decision.matched_policy_id }}
								className="underline-offset-4 hover:underline"
							>
								{policy?.name ?? LOADING_TEXT}, version{" "}
								{decision.matched_policy_version}
							</Link>
						),
					},
				]}
			/>
		</SectionCard>
	);
}
