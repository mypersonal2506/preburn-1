import type { DecisionDetailResponse } from "@/client";
import { HelpTip } from "@/components/help-tip";
import { KeyValueList } from "@/components/key-value-list";
import { SectionCard } from "@/components/section-card";
import {
	formatCount,
	formatMargin,
	formatMoney,
	formatPace,
	formatPercent,
	formatPeriod,
	formatUnitCost,
} from "@/lib/format";

interface DecisionSignalsCardProps {
	decision: DecisionDetailResponse;
}

/**
 * The customer's signals at the check, which the policies compared, under
 * the UTC billing period the decision counts in.
 */
export function DecisionSignalsCard({ decision }: DecisionSignalsCardProps) {
	const signals = decision.signals;
	return (
		<SectionCard
			title="Signals"
			description={formatPeriod(decision.period_start, decision.period_end)}
		>
			<KeyValueList
				items={[
					{
						label: "Revenue this period",
						value: formatMoney(signals.period_revenue_net),
					},
					{
						label: "Spend this period",
						value: formatMoney(signals.cost_to_date),
					},
					{ label: "Reserved", value: formatMoney(signals.reserved) },
					{ label: "Allowance", value: formatMoney(signals.cost_allowance) },
					{
						label: "Allowance left",
						value: formatMoney(signals.allowance_remaining),
					},
					{
						label: "Period elapsed",
						value: formatPercent(signals.elapsed_fraction),
					},
					{
						label: "Pace",
						value: (
							<span className="inline-flex items-center gap-1">
								{formatPace(signals.pace)}
								<HelpTip topic="pace">
									Allowance spent divided by the period elapsed.
								</HelpTip>
							</span>
						),
					},
					{
						label: "Projected margin",
						value: (
							<span className="inline-flex items-center gap-1">
								{formatMargin(signals.projected_margin)}
								<HelpTip topic="projected margin">
									Margin at the end of the period at the current pace.
								</HelpTip>
							</span>
						),
					},
					{
						label: "Requests this period",
						value: formatCount(signals.period_decision_count),
					},
					{
						label: "Request cost",
						value:
							signals.request_estimated_cost !== null &&
							formatUnitCost(signals.request_estimated_cost),
					},
				]}
			/>
		</SectionCard>
	);
}
