import type { DecisionDetailResponse } from "@/client";
import { HelpTip } from "@/components/help-tip";
import { KeyValueList } from "@/components/key-value-list";
import { SectionCard } from "@/components/section-card";
import {
	estimateBasisLabel,
	requestCostLabel,
} from "@/features/decisions/decision-labels";

interface DecisionCostCardProps {
	decision: DecisionDetailResponse;
}

/**
 * The estimated cost of a decision's request as it runs, the cost of the
 * request as asked when a route or cap changed it, and the usage whose cost
 * the check reserved.
 */
export function DecisionCostCard({ decision }: DecisionCostCardProps) {
	const changedRequest =
		decision.outcome === "route" || decision.outcome === "cap";
	return (
		<SectionCard title="Cost">
			<KeyValueList
				items={[
					{
						label: "Requested cost",
						value:
							changedRequest &&
							requestCostLabel(decision.requested_estimated_cost),
					},
					{
						label: "Estimated cost",
						value: requestCostLabel(decision.estimated_cost),
					},
					{
						label: "Estimate basis",
						value: (
							<span className="inline-flex items-center gap-1">
								{estimateBasisLabel(decision.estimate_basis)}
								<HelpTip topic="estimate basis">
									The usage whose cost the check reserved.
								</HelpTip>
							</span>
						),
					},
				]}
			/>
		</SectionCard>
	);
}
