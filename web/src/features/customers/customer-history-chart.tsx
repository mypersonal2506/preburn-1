import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import type { CustomerPeriodResponse } from "@/client";
import {
	type ChartConfig,
	ChartContainer,
	ChartLegend,
	ChartLegendContent,
	ChartTooltip,
	ChartTooltipContent,
} from "@/components/ui/chart";
import { formatDate, formatMoney, ratioToNumber } from "@/lib/format";

interface CustomerHistoryChartProps {
	history: readonly CustomerPeriodResponse[];
}

const AMOUNT_DECIMALS = 9;
const TICK_DECIMALS = 2;
const BAR_MAXIMUM_WIDTH = 24;
const BAR_GAP = 2;
const BAR_RADIUS = 4;
const Y_AXIS_WIDTH = 72;

const HISTORY_CHART_CONFIG = {
	revenue: { label: "Revenue", color: "var(--revenue)" },
	cost: { label: "AI cost", color: "var(--cost)" },
} satisfies ChartConfig;

/**
 * Revenue and AI cost of each billing period as grouped bars, oldest period
 * first, labelled by the UTC date the period starts. history comes newest
 * first, as the API sends it. Load it lazily, it carries the chart library.
 */
export function CustomerHistoryChart({ history }: CustomerHistoryChartProps) {
	const points = history.toReversed().map((period) => ({
		periodStart: formatDate(period.period_start),
		revenue: ratioToNumber(period.revenue),
		cost: ratioToNumber(period.cost),
	}));
	return (
		<ChartContainer config={HISTORY_CHART_CONFIG} className="aspect-auto h-56">
			<BarChart
				data={points}
				barGap={BAR_GAP}
				maxBarSize={BAR_MAXIMUM_WIDTH}
				accessibilityLayer
			>
				<CartesianGrid vertical={false} />
				<XAxis dataKey="periodStart" tickLine={false} axisLine={false} />
				<YAxis
					width={Y_AXIS_WIDTH}
					tickLine={false}
					axisLine={false}
					tickFormatter={(tick: number) =>
						formatMoney(tick.toFixed(TICK_DECIMALS))
					}
				/>
				<ChartTooltip
					content={
						<ChartTooltipContent
							formatter={(value, name) => (
								<div className="flex w-full justify-between gap-4">
									<span className="text-muted-foreground">{name}</span>
									<span className="numeric font-medium text-foreground">
										{chartAmount(value)}
									</span>
								</div>
							)}
						/>
					}
				/>
				<ChartLegend content={<ChartLegendContent />} />
				<Bar
					dataKey="revenue"
					name={HISTORY_CHART_CONFIG.revenue.label}
					fill="var(--color-revenue)"
					radius={[BAR_RADIUS, BAR_RADIUS, 0, 0]}
				/>
				<Bar
					dataKey="cost"
					name={HISTORY_CHART_CONFIG.cost.label}
					fill="var(--color-cost)"
					radius={[BAR_RADIUS, BAR_RADIUS, 0, 0]}
				/>
			</BarChart>
		</ChartContainer>
	);
}

function chartAmount(value: unknown): string {
	if (typeof value !== "number") {
		throw new Error(`chart amount invalid value=${String(value)}`);
	}
	return formatMoney(value.toFixed(AMOUNT_DECIMALS));
}
