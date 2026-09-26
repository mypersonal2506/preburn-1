import type { ReactNode } from "react";
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts";
import type { DailyTotalsResponse } from "@/client";
import {
	type ChartConfig,
	ChartContainer,
	ChartTooltip,
	ChartTooltipContent,
} from "@/components/ui/chart";
import { formatDate, formatMoney, ratioToNumber } from "@/lib/format";

interface OverviewDailyChartProps {
	daily: readonly DailyTotalsResponse[];
}

interface DailyChartPoint {
	date: string;
	revenue: number;
	cost: number;
}

const AMOUNT_DECIMALS = 9;

const DAILY_CHART_CONFIG = {
	revenue: { label: "Revenue", color: "var(--revenue)" },
	cost: { label: "AI cost", color: "var(--cost)" },
} satisfies ChartConfig;

/**
 * Revenue and AI cost per UTC day as overlapping areas in the revenue and
 * cost tokens, with the day and both amounts in the tooltip. The overview
 * loads it lazily, so the chart library stays out of the initial bundle.
 */
export function OverviewDailyChart({ daily }: OverviewDailyChartProps) {
	const points: DailyChartPoint[] = daily.map((day) => ({
		date: day.date,
		revenue: ratioToNumber(day.revenue),
		cost: ratioToNumber(day.cost),
	}));

	return (
		<ChartContainer
			config={DAILY_CHART_CONFIG}
			className="aspect-auto h-64 w-full"
		>
			<AreaChart data={points} margin={{ left: 12, right: 12 }}>
				<CartesianGrid vertical={false} />
				<XAxis
					dataKey="date"
					tickLine={false}
					axisLine={false}
					tickMargin={8}
					minTickGap={32}
					tickFormatter={formatChartDate}
				/>
				<ChartTooltip
					cursor={false}
					content={
						<ChartTooltipContent
							labelFormatter={formatChartDate}
							formatter={(amount, name, item) => (
								<div className="flex w-full items-center gap-2">
									<span
										aria-hidden
										className="size-2.5 shrink-0 rounded-[2px]"
										style={{ backgroundColor: item.color }}
									/>
									<span className="text-muted-foreground">{name}</span>
									<span className="numeric ml-auto">
										{chartAmountToMoney(amount)}
									</span>
								</div>
							)}
						/>
					}
				/>
				<Area
					dataKey="revenue"
					name={DAILY_CHART_CONFIG.revenue.label}
					type="monotone"
					fill="var(--color-revenue)"
					fillOpacity={0.2}
					stroke="var(--color-revenue)"
				/>
				<Area
					dataKey="cost"
					name={DAILY_CHART_CONFIG.cost.label}
					type="monotone"
					fill="var(--color-cost)"
					fillOpacity={0.3}
					stroke="var(--color-cost)"
				/>
			</AreaChart>
		</ChartContainer>
	);
}

function formatChartDate(date: ReactNode): string {
	if (typeof date !== "string") {
		throw new Error(`chart date invalid value=${String(date)}`);
	}
	return formatDate(date);
}

function chartAmountToMoney(amount: unknown): string {
	if (typeof amount !== "number") {
		throw new Error(`chart amount invalid value=${String(amount)}`);
	}
	return formatMoney(amount.toFixed(AMOUNT_DECIMALS));
}
