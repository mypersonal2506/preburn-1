import { linkOptions } from "@tanstack/react-router";
import { UsersIcon } from "lucide-react";
import type { LossCustomerResponse } from "@/client";
import { CustomerLabel } from "@/components/customer-label";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { MarginPill } from "@/components/margin-pill";
import { OutcomeBadge } from "@/components/outcome-badge";
import { SectionCard } from "@/components/section-card";
import { formatMoney } from "@/lib/format";

interface OverviewCustomersToWatchProps {
	lossCustomers: readonly LossCustomerResponse[];
}

const CUSTOMER_TO_WATCH_COLUMNS: readonly DataTableColumn<LossCustomerResponse>[] =
	[
		{
			header: "Customer",
			cell: (customer) => (
				<CustomerLabel
					displayName={customer.display_name}
					externalId={customer.external_id}
				/>
			),
		},
		{
			header: "Revenue",
			cell: (customer) => formatMoney(customer.revenue),
			numeric: true,
		},
		{
			header: "AI cost",
			cell: (customer) => formatMoney(customer.cost),
			numeric: true,
		},
		{
			header: "Margin",
			cell: (customer) => (
				<MarginPill margin={customer.margin} targetMargin={null} />
			),
		},
		{
			header: "Latest policy outcome",
			cell: (customer) =>
				customer.latest_policy_outcome === null ? (
					<span className="text-muted-foreground">None</span>
				) : (
					<OutcomeBadge outcome={customer.latest_policy_outcome.outcome} />
				),
		},
	];

/**
 * The Customers to watch card: the paying customers losing the most money
 * in the period, lowest margin first, with the outcome of the latest
 * decision a policy made for them. Rows open the customer.
 */
export function OverviewCustomersToWatch({
	lossCustomers,
}: OverviewCustomersToWatchProps) {
	return (
		<SectionCard title="Customers to watch">
			<DataTable
				label="Customers to watch"
				columns={CUSTOMER_TO_WATCH_COLUMNS}
				rows={lossCustomers}
				rowKey={(customer) => customer.id}
				rowLink={(customer) =>
					linkOptions({
						to: "/customers/$customerId",
						params: { customerId: customer.id },
					})
				}
				emptyState={
					<EmptyState icon={UsersIcon} title="No customers losing money" />
				}
			/>
		</SectionCard>
	);
}
