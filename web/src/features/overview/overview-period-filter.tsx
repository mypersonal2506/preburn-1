import type { OverviewResponse } from "@/client";
import { FilterBar } from "@/components/filter-bar";
import { FilterChip } from "@/components/filter-chip";

/**
 * What the overview sums: the current period, the previous one or the last
 * 30 days.
 */
export type OverviewPeriod = OverviewResponse["period"];

interface OverviewPeriodFilterProps {
	period: OverviewPeriod;
	onPeriodChange: (period: OverviewPeriod) => void;
}

const OVERVIEW_PERIOD_LABELS: Record<OverviewPeriod, string> = {
	current: "Current period",
	previous: "Previous period",
	last_30_days: "Last 30 days",
};
const OVERVIEW_PERIODS: readonly OverviewPeriod[] = [
	"current",
	"previous",
	"last_30_days",
];

/** The overview's period selector: one pressed chip per period option. */
export function OverviewPeriodFilter({
	period,
	onPeriodChange,
}: OverviewPeriodFilterProps) {
	return (
		<FilterBar label="Period">
			{OVERVIEW_PERIODS.map((option) => (
				<FilterChip
					key={option}
					label={OVERVIEW_PERIOD_LABELS[option]}
					pressed={option === period}
					onClick={() => onPeriodChange(option)}
				/>
			))}
		</FilterBar>
	);
}
