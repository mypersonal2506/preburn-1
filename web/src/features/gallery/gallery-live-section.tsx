import { useQuery } from "@tanstack/react-query";
import { linkOptions } from "@tanstack/react-router";
import { ScaleIcon, UsersIcon } from "lucide-react";
import { useState } from "react";
import type { CustomerMarginResponse } from "@/client";
import { listDashboardCustomersOptions } from "@/client/@tanstack/react-query.gen";
import { CustomerLabel } from "@/components/customer-label";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { LiveIndicator } from "@/components/live-indicator";
import { MarginPill } from "@/components/margin-pill";
import { OutcomeBadge } from "@/components/outcome-badge";
import { SectionCard } from "@/components/section-card";
import { useCursorPagination } from "@/components/use-cursor-pagination";
import { useEnvironment } from "@/lib/environment-store";
import { formatDateTime, formatMoney } from "@/lib/format";
import { type DecisionEvent, useEventStream } from "@/lib/use-event-stream";

const CUSTOMER_PAGE_SIZE = 2;
const SHOWN_DECISIONS_MAXIMUM = 5;
const CUSTOMER_COLUMNS: readonly DataTableColumn<CustomerMarginResponse>[] = [
	{
		header: "Customer",
		cell: (customer) => (
			<CustomerLabel
				displayName={customer.display_name}
				externalId={customer.external_id}
			/>
		),
	},
	{ header: "Plan", cell: (customer) => customer.plan_name ?? "No plan" },
	{
		header: "Revenue",
		cell: (customer) => formatMoney(customer.revenue),
		numeric: true,
	},
	{
		header: "Margin",
		cell: (customer) => (
			<MarginPill
				margin={customer.margin}
				targetMargin={customer.target_margin}
			/>
		),
	},
];

export function GalleryLiveSection() {
	const environment = useEnvironment();
	const pagination = useCursorPagination("newest");
	const customers = useQuery(
		listDashboardCustomersOptions({
			query: { cursor: pagination.cursor, limit: CUSTOMER_PAGE_SIZE },
		}),
	);

	return (
		<SectionCard title="Live data">
			<div className="flex flex-col gap-6">
				<DataTable
					label="Customers"
					columns={CUSTOMER_COLUMNS}
					rows={customers.data?.items}
					rowKey={(customer) => customer.id}
					rowLink={(customer) =>
						linkOptions({
							to: "/customers/$customerId",
							params: { customerId: customer.id },
						})
					}
					emptyState={
						<EmptyState
							icon={UsersIcon}
							title="No customers yet"
							description="Customers appear after their first check."
						/>
					}
					pagination={pagination}
					nextCursor={customers.data?.next_cursor}
				/>
				<GalleryDecisionStream key={environment} />
			</div>
		</SectionCard>
	);
}

function GalleryDecisionStream() {
	const [decisions, setDecisions] = useState<DecisionEvent[]>([]);
	const stream = useEventStream((decision) => {
		setDecisions((shown) =>
			[decision, ...shown].slice(0, SHOWN_DECISIONS_MAXIMUM),
		);
	});

	return (
		<div className="flex flex-col gap-3">
			<LiveIndicator stream={stream} />
			{decisions.length === 0 ? (
				<EmptyState
					icon={ScaleIcon}
					title="No decisions yet"
					description="Decisions appear here as checks arrive."
				/>
			) : (
				<ul aria-label="Latest decisions" className="flex flex-col gap-2">
					{decisions.map((decision) => (
						<li
							key={decision.id}
							className="flex flex-wrap items-center gap-3 text-sm"
						>
							<OutcomeBadge outcome={decision.outcome} />
							<CustomerLabel
								displayName={decision.customer_display_name}
								externalId={decision.customer_external_id}
							/>
							<span className="font-mono text-xs">{decision.model}</span>
							<span className="text-muted-foreground">
								{formatDateTime(decision.created_at)}
							</span>
						</li>
					))}
				</ul>
			)}
		</div>
	);
}
