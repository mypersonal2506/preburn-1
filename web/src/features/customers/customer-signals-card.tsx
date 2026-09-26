import type { ReactNode } from "react";
import type { SignalsResponse } from "@/client";
import { HelpTip } from "@/components/help-tip";
import { KeyValueList } from "@/components/key-value-list";
import { SectionCard } from "@/components/section-card";
import {
	formatCount,
	formatMargin,
	formatMoney,
	formatPace,
	formatPercent,
} from "@/lib/format";

interface CustomerSignalsCardProps {
	signals: SignalsResponse;
}

interface SignalValueProps {
	topic: string;
	help: string;
	children: ReactNode;
}

/**
 * The Signals card of a customer: every signal of the current period as a
 * policy condition reads it, named by the policy sentence phrases, such as
 * "Allowance left" and "Spend this period".
 */
export function CustomerSignalsCard({ signals }: CustomerSignalsCardProps) {
	return (
		<SectionCard title="Signals">
			<KeyValueList
				items={[
					{
						label: "Allowance left",
						value: (
							<SignalValue
								topic="Allowance left"
								help="Allowance minus settled and reserved cost."
							>
								{formatMoney(signals.allowance_remaining)}
							</SignalValue>
						),
					},
					{ label: "Pace", value: formatPace(signals.pace) },
					{
						label: "Projected margin",
						value: formatMargin(signals.projected_margin),
					},
					{
						label: "Spend this period",
						value: formatMoney(signals.cost_to_date),
					},
					{
						label: "Revenue this period",
						value: formatMoney(signals.period_revenue_net),
					},
					{
						label: "Period elapsed",
						value: formatPercent(signals.elapsed_fraction),
					},
					{
						label: "Requests this period",
						value: formatCount(signals.period_decision_count),
					},
					{ label: "Allowance", value: formatMoney(signals.cost_allowance) },
					{
						label: "Reserved",
						value: (
							<SignalValue
								topic="Reserved"
								help="Cost held for checked requests until usage arrives."
							>
								{formatMoney(signals.reserved)}
							</SignalValue>
						),
					},
				]}
			/>
		</SectionCard>
	);
}

function SignalValue({ topic, help, children }: SignalValueProps) {
	return (
		<span className="inline-flex items-center gap-1">
			{children}
			<HelpTip topic={topic}>{help}</HelpTip>
		</span>
	);
}
