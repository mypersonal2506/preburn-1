import { useQuery } from "@tanstack/react-query";
import { getRouteApi, Link, linkOptions } from "@tanstack/react-router";
import { FunnelXIcon, ScaleIcon } from "lucide-react";
import type { DecisionResponse } from "@/client";
import { listDashboardDecisionsOptions } from "@/client/@tanstack/react-query.gen";
import { CustomerLabel } from "@/components/customer-label";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { FeatureLabel } from "@/components/feature-label";
import { LiveIndicator } from "@/components/live-indicator";
import { OutcomeBadge } from "@/components/outcome-badge";
import { PageHeader } from "@/components/page-header";
import type { ModelReference } from "@/components/pickers/model-picker";
import { RelativeTime } from "@/components/relative-time";
import { StatusDot } from "@/components/status-dot";
import { Button } from "@/components/ui/button";
import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { useCursorPagination } from "@/components/use-cursor-pagination";
import { useModelDisplayNames } from "@/components/use-model-display-names";
import { DecisionFilterBar } from "@/features/decisions/decision-filter-bar";
import type { DecisionFilters } from "@/features/decisions/decision-filters";
import {
	decisionReasonLabel,
	requestCostLabel,
} from "@/features/decisions/decision-labels";
import { DecisionModel } from "@/features/decisions/decision-model";
import { useLiveDecisions } from "@/features/decisions/use-live-decisions";

const decisionsRoute = getRouteApi("/_app/decisions/");

export function DecisionsPage() {
	const filters = decisionsRoute.useSearch();
	const navigate = decisionsRoute.useNavigate();
	const pagination = useCursorPagination(JSON.stringify(filters));
	const listOptions = listDashboardDecisionsOptions({
		query: { ...filters, cursor: pagination.cursor },
	});
	const stream = useLiveDecisions(
		listOptions.queryKey,
		filters,
		pagination.cursor === undefined,
	);
	const decisionsQuery = useQuery({
		...listOptions,
		throwOnError: true,
		refetchOnWindowFocus: stream.status !== "paused",
	});
	const decisions = decisionsQuery.data?.items;
	const modelDisplayName = useModelDisplayNames(
		decisions?.flatMap(decisionModels) ?? [],
	);
	const filtered = Object.values(filters).some((value) => value !== undefined);

	function changeFilters(changed: DecisionFilters): void {
		void navigate({ search: changed });
	}

	return (
		<>
			<PageHeader
				title="Decisions"
				actions={<LiveIndicator stream={stream} />}
			/>
			<div className="flex flex-col gap-3">
				<DecisionFilterBar filters={filters} onChange={changeFilters} />
				<DataTable
					label="Decisions"
					columns={decisionColumns(modelDisplayName, new Date())}
					rows={decisions}
					rowKey={(decision) => decision.id}
					rowLink={(decision) =>
						linkOptions({
							to: "/decisions/$decisionId",
							params: { decisionId: decision.id },
						})
					}
					emptyState={
						filtered ? (
							<EmptyState
								icon={FunnelXIcon}
								title="No matching decisions"
								action={
									<Button variant="outline" onClick={() => changeFilters({})}>
										Clear filters
									</Button>
								}
							/>
						) : (
							<EmptyState
								icon={ScaleIcon}
								title="No decisions yet"
								description="Send a check to see decisions here."
								action={
									<Button asChild>
										<Link to="/developers/get-started">Get started</Link>
									</Button>
								}
							/>
						)
					}
					pagination={pagination}
					nextCursor={decisionsQuery.data?.next_cursor}
				/>
			</div>
		</>
	);
}

function decisionColumns(
	modelDisplayName: (model: ModelReference) => string | null,
	now: Date,
): DataTableColumn<DecisionResponse>[] {
	return [
		{
			header: "Time",
			cell: (decision) => (
				<RelativeTime timestamp={decision.created_at} now={now} />
			),
		},
		{
			header: "Customer",
			cell: (decision) => (
				<CustomerLabel
					displayName={decision.customer_display_name}
					externalId={decision.customer_external_id}
				/>
			),
		},
		{
			header: "Feature",
			cell: (decision) => <FeatureLabel feature={decision.feature} />,
		},
		{
			header: "Model",
			cell: (decision) => (
				<DecisionModel
					decision={decision}
					modelDisplayName={modelDisplayName}
				/>
			),
		},
		{
			header: "Outcome",
			cell: (decision) => (
				<Tooltip>
					<TooltipTrigger asChild>
						<span>
							<OutcomeBadge outcome={decision.outcome} />
						</span>
					</TooltipTrigger>
					<TooltipContent>
						{decisionReasonLabel(decision.reason)}
					</TooltipContent>
				</Tooltip>
			),
		},
		{
			header: "Cost",
			cell: (decision) => requestCostLabel(decision.estimated_cost),
			numeric: true,
		},
		{
			header: "Status",
			cell: (decision) => <StatusDot status={decision.status} />,
		},
	];
}

function decisionModels(decision: DecisionResponse): ModelReference[] {
	return [
		{ provider: decision.provider, model: decision.model },
		{ provider: decision.requested_provider, model: decision.requested_model },
	];
}
