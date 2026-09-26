import { LayersIcon } from "lucide-react";
import type { PlanMarginResponse } from "@/client";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { MarginBar } from "@/components/margin-bar";
import { MarginPill } from "@/components/margin-pill";
import { SectionCard } from "@/components/section-card";
import { formatCount, formatMoney } from "@/lib/format";

interface OverviewPlanMarginsProps {
	planMargins: readonly PlanMarginResponse[];
}

const NO_PLAN_ROW_KEY = "no-plan";

const PLAN_MARGIN_COLUMNS: readonly DataTableColumn<PlanMarginResponse>[] = [
	{
		header: "Plan",
		cell: (planMargin) =>
			planMargin.name ?? <span className="text-muted-foreground">No plan</span>,
	},
	{
		header: "Customers",
		cell: (planMargin) => formatCount(planMargin.customer_count),
		numeric: true,
	},
	{
		header: "Revenue",
		cell: (planMargin) => formatMoney(planMargin.revenue),
		numeric: true,
	},
	{
		header: "AI cost",
		cell: (planMargin) => formatMoney(planMargin.cost),
		numeric: true,
	},
	{
		header: "Margin",
		cell: (planMargin) => (
			<div className="flex items-center gap-2">
				<MarginPill
					margin={planMargin.margin}
					targetMargin={goalMargin(planMargin)}
				/>
				<div className="w-16">
					<MarginBar
						margin={planMargin.margin}
						targetMargin={goalMargin(planMargin)}
					/>
				</div>
			</div>
		),
	},
];

/**
 * The Plan margins card: revenue, AI cost and margin for each effective plan
 * with totals in the period, customers without a plan last. Margins of
 * margin target plans read against the target, the others by sign only.
 */
export function OverviewPlanMargins({ planMargins }: OverviewPlanMarginsProps) {
	return (
		<SectionCard title="Plan margins">
			<DataTable
				label="Plan margins"
				columns={PLAN_MARGIN_COLUMNS}
				rows={planMargins}
				rowKey={(planMargin) => planMargin.plan_id ?? NO_PLAN_ROW_KEY}
				emptyState={
					<EmptyState icon={LayersIcon} title="No usage in this period" />
				}
			/>
		</SectionCard>
	);
}

function goalMargin(planMargin: PlanMarginResponse): string | null {
	return planMargin.mode === "margin_target" ? planMargin.target_margin : null;
}
