import { useQuery } from "@tanstack/react-query";
import { Link, linkOptions } from "@tanstack/react-router";
import { LayersIcon, PlusIcon, TriangleAlertIcon } from "lucide-react";
import type { ReactNode } from "react";
import type { PlanResponse } from "@/client";
import {
	getSettingsOptions,
	listPlansOptions,
} from "@/client/@tanstack/react-query.gen";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { StatusDot } from "@/components/status-dot";
import { Button } from "@/components/ui/button";
import { useCursorPagination } from "@/components/use-cursor-pagination";
import { DefaultPlanBadge } from "@/features/plans/default-plan-badge";
import { planRule } from "@/features/plans/plan-draft";
import { formatCount } from "@/lib/format";

export function PlanListPage() {
	const pagination = useCursorPagination("");
	const plansQuery = useQuery(
		listPlansOptions({ query: { cursor: pagination.cursor } }),
	);
	const defaultPlanIdQuery = useQuery({
		...getSettingsOptions(),
		select: (settings) => settings.default_plan_id,
	});

	function emptyState(): ReactNode {
		if (plansQuery.isError) {
			return (
				<EmptyState
					icon={TriangleAlertIcon}
					title="Something went wrong"
					action={
						<Button variant="outline" onClick={() => plansQuery.refetch()}>
							Try again
						</Button>
					}
				/>
			);
		}
		return (
			<EmptyState
				icon={LayersIcon}
				title="No plans yet"
				description="Plans set the AI allowance of customers."
				action={<NewPlanButton />}
			/>
		);
	}

	return (
		<>
			<PageHeader title="Plans" actions={<NewPlanButton />} />
			<DataTable
				label="Plans"
				columns={planColumns(defaultPlanIdQuery.data ?? null)}
				rows={plansQuery.isError ? [] : plansQuery.data?.items}
				rowKey={(plan) => plan.id}
				rowLink={(plan) =>
					linkOptions({ to: "/plans/$planId", params: { planId: plan.id } })
				}
				emptyState={emptyState()}
				pagination={pagination}
				nextCursor={plansQuery.data?.next_cursor}
			/>
		</>
	);
}

function planColumns(
	defaultPlanId: string | null,
): DataTableColumn<PlanResponse>[] {
	return [
		{
			header: "Plan",
			cell: (plan) => (
				<span className="inline-flex items-center gap-2">
					{plan.name}
					{plan.id === defaultPlanId && <DefaultPlanBadge />}
				</span>
			),
		},
		{ header: "Rule", cell: planRule },
		{
			header: "Customers",
			cell: (plan) => formatCount(plan.customer_count),
			numeric: true,
		},
		{ header: "Status", cell: (plan) => <StatusDot status={plan.status} /> },
	];
}

function NewPlanButton() {
	return (
		<Button asChild>
			<Link to="/plans/new">
				<PlusIcon />
				New plan
			</Link>
		</Button>
	);
}
