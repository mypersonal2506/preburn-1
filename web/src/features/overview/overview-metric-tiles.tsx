import type { OverviewResponse } from "@/client";
import { MarginBar } from "@/components/margin-bar";
import { MarginPill } from "@/components/margin-pill";
import { MetricTile } from "@/components/metric-tile";
import { formatShare } from "@/features/overview/format-share";
import {
	formatDate,
	formatMargin,
	formatMarginGap,
	formatMoney,
	ratioToNumber,
} from "@/lib/format";

interface OverviewMetricTilesProps {
	overview: OverviewResponse;
}

/**
 * The four headline tiles of the overview: Revenue recognized through the
 * last day of the period, AI cost with its share of revenue, Gross margin
 * against the revenue-weighted target, and Cost avoided by route, cap and
 * deny.
 */
export function OverviewMetricTiles({ overview }: OverviewMetricTilesProps) {
	const lastDay = overview.daily.at(-1);
	if (lastDay === undefined) {
		throw new Error(`overview daily series empty period=${overview.period}`);
	}
	const margin = overview.margin;
	const targetMargin = overview.target_margin;

	return (
		<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
			<MetricTile
				label="Revenue"
				value={formatMoney(overview.revenue)}
				foot={`Recognized through ${formatDate(lastDay.date)}`}
			/>
			<MetricTile
				label="AI cost"
				value={formatMoney(overview.cost)}
				foot={
					margin === null
						? "No revenue"
						: `${formatShare(ratioToNumber(overview.cost), ratioToNumber(overview.revenue))} of revenue`
				}
			/>
			<MetricTile
				label="Gross margin"
				value={formatMargin(margin)}
				delta={
					margin !== null && (
						<MarginPill margin={margin} targetMargin={targetMargin} />
					)
				}
				track={<MarginBar margin={margin} targetMargin={targetMargin} />}
				foot={
					margin !== null && targetMargin !== null
						? formatMarginGap(margin, targetMargin)
						: undefined
				}
			/>
			<MetricTile
				label="Cost avoided"
				helpTip="Estimated at check time"
				value={formatMoney(overview.cost_avoided.total)}
				foot="By route, cap and deny"
			/>
		</div>
	);
}
