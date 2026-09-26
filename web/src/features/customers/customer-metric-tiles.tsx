import { useQuery } from "@tanstack/react-query";
import type { CustomerDetailResponse } from "@/client";
import { getPlanOptions } from "@/client/@tanstack/react-query.gen";
import { MarginBar } from "@/components/margin-bar";
import { MetricTile } from "@/components/metric-tile";
import { AllowanceTrack } from "@/features/customers/allowance-track";
import {
	formatMargin,
	formatMarginGap,
	formatMoney,
	formatPace,
	formatPercent,
	formatPeriod,
	ratioToNumber,
} from "@/lib/format";

interface CustomerMetricTilesProps {
	customer: CustomerDetailResponse;
}

const NO_REVENUE_PROJECTED_MARGIN = "-inf";

/**
 * The headline tiles of a customer's current period: Revenue with the
 * period, AI cost with its allowance track, Projected margin against the
 * plan target and Pace with the elapsed share of the period. Only a margin
 * target plan's target counts, with its gap in points, since a fixed
 * allowance plan keeps a stored target that is not its goal.
 */
export function CustomerMetricTiles({ customer }: CustomerMetricTilesProps) {
	const { signals } = customer;
	const hasAllowance = ratioToNumber(signals.cost_allowance) > 0;
	const planModeQuery = useQuery({
		...getPlanOptions({ path: { plan_id: customer.plan_id ?? "" } }),
		enabled: customer.plan_id !== null,
		select: (plan) => plan.mode,
	});
	const targetMargin =
		planModeQuery.data === "margin_target" ? customer.target_margin : null;
	const marginGap =
		targetMargin !== null &&
		signals.projected_margin !== NO_REVENUE_PROJECTED_MARGIN
			? formatMarginGap(signals.projected_margin, targetMargin)
			: undefined;
	return (
		<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
			<MetricTile
				label="Revenue"
				value={formatMoney(signals.period_revenue_net)}
				foot={formatPeriod(customer.period_start, customer.period_end)}
			/>
			<MetricTile
				label="AI cost"
				value={formatMoney(signals.cost_to_date)}
				track={
					hasAllowance && (
						<AllowanceTrack
							costAllowance={signals.cost_allowance}
							allowanceRemaining={signals.allowance_remaining}
						/>
					)
				}
				foot={
					hasAllowance
						? `${formatMoney(signals.cost_allowance)} allowance`
						: "No allowance"
				}
			/>
			<MetricTile
				label="Projected margin"
				helpTip="Margin at period end if spending keeps its pace."
				value={formatMargin(signals.projected_margin)}
				track={
					<MarginBar
						margin={signals.projected_margin}
						targetMargin={targetMargin}
					/>
				}
				foot={marginGap}
			/>
			<MetricTile
				label="Pace"
				helpTip="Allowance used over period elapsed. 1.0x is on track."
				value={formatPace(signals.pace)}
				foot={`${formatPercent(signals.elapsed_fraction)} of period elapsed`}
			/>
		</div>
	);
}
