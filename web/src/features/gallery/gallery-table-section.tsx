import { linkOptions } from "@tanstack/react-router";
import { UsersIcon } from "lucide-react";
import { useState } from "react";
import type { CustomerMarginResponse } from "@/client";
import { CustomerLabel } from "@/components/customer-label";
import {
	DataTable,
	type DataTableColumn,
	type DataTableSort,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { FilterBar } from "@/components/filter-bar";
import { FilterChip } from "@/components/filter-chip";
import { MarginPill } from "@/components/margin-pill";
import { SectionCard } from "@/components/section-card";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { useCursorPagination } from "@/components/use-cursor-pagination";
import { formatMoney, ratioToNumber } from "@/lib/format";

type SampleCustomer = Pick<
	CustomerMarginResponse,
	| "id"
	| "external_id"
	| "display_name"
	| "plan_name"
	| "revenue"
	| "margin"
	| "target_margin"
>;

type SampleSortKey = "revenue";

interface SamplePage {
	items: readonly SampleCustomer[];
	next_cursor: string | null;
}

const FIRST_PAGE_CURSOR = "first";
const NO_REVENUE = "0.000000000";
const SAMPLE_PAGES = new Map<string, SamplePage>([
	[
		FIRST_PAGE_CURSOR,
		{
			items: [
				sampleCustomer("acme", "Acme", "Creator", "120.000000000", "0.4200"),
				sampleCustomer(
					"cedar",
					"Cedar Games",
					"Creator",
					"80.000000000",
					"0.1500",
				),
			],
			next_cursor: "page-2",
		},
	],
	[
		"page-2",
		{
			items: [
				sampleCustomer(
					"lumber",
					"Lumber Co",
					"Studio",
					"40.000000000",
					"-0.0800",
				),
				sampleCustomer("tall-oak", "Tall Oak Studio", "Free", NO_REVENUE, null),
			],
			next_cursor: "page-3",
		},
	],
	[
		"page-3",
		{
			items: [
				sampleCustomer(
					"hilltop",
					"Hilltop Creative",
					"Studio",
					"310.000000000",
					"0.5100",
				),
			],
			next_cursor: null,
		},
	],
]);
const SAMPLE_COLUMNS: readonly DataTableColumn<
	SampleCustomer,
	SampleSortKey
>[] = [
	{
		header: "Customer",
		cell: (customer) => (
			<CustomerLabel
				displayName={customer.display_name}
				externalId={customer.external_id}
			/>
		),
	},
	{ header: "Plan", cell: (customer) => customer.plan_name },
	{
		header: "Revenue",
		cell: (customer) => formatMoney(customer.revenue),
		numeric: true,
		sortKey: "revenue",
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

export function GalleryTableSection() {
	const [search, setSearch] = useState("");
	const [payingOnly, setPayingOnly] = useState(false);
	const [sort, setSort] = useState<DataTableSort<SampleSortKey>>({
		key: "revenue",
		direction: "descending",
	});
	const pagination = useCursorPagination(`${payingOnly} ${search}`);
	const page = samplePage(pagination.cursor ?? FIRST_PAGE_CURSOR);
	const rows = page.items
		.filter(
			(customer) =>
				(!payingOnly || customer.revenue !== NO_REVENUE) &&
				customer.external_id.includes(search.toLowerCase()),
		)
		.toSorted((left, right) => {
			const difference =
				ratioToNumber(left.revenue) - ratioToNumber(right.revenue);
			return sort.direction === "ascending" ? difference : -difference;
		});
	const emptyCustomers = (
		<EmptyState
			icon={UsersIcon}
			title="No customers yet"
			description="Customers appear after their first check."
		/>
	);

	return (
		<SectionCard title="Tables">
			<div className="flex flex-col gap-6">
				<FilterBar
					label="Customer filters"
					search={{
						value: search,
						placeholder: "Search customers",
						onChange: setSearch,
					}}
					end={
						<FilterChip
							label="Plan"
							value="Creator"
							onClear={() => undefined}
						/>
					}
				>
					<FilterChip
						label="Paying"
						pressed={payingOnly}
						onClick={() => setPayingOnly(!payingOnly)}
					/>
				</FilterBar>
				<DataTable
					label="Sample customers"
					columns={SAMPLE_COLUMNS}
					rows={rows}
					rowKey={(customer) => customer.id}
					rowLink={(customer) =>
						linkOptions({
							to: "/customers/$customerId",
							params: { customerId: customer.id },
						})
					}
					rowMenu={() => <DropdownMenuItem>Archive</DropdownMenuItem>}
					emptyState={emptyCustomers}
					pagination={pagination}
					nextCursor={page.next_cursor}
					sort={sort}
					onSortChange={setSort}
				/>
				<DataTable
					label="Loading customers"
					columns={SAMPLE_COLUMNS}
					rows={undefined}
					rowKey={(customer) => customer.id}
					emptyState={emptyCustomers}
				/>
				<DataTable
					label="No customers"
					columns={SAMPLE_COLUMNS}
					rows={[]}
					rowKey={(customer) => customer.id}
					emptyState={emptyCustomers}
				/>
			</div>
		</SectionCard>
	);
}

function sampleCustomer(
	externalId: string,
	displayName: string,
	planName: string,
	revenue: string,
	margin: string | null,
): SampleCustomer {
	return {
		id: `cust_gallery_${externalId}`,
		external_id: externalId,
		display_name: displayName,
		plan_name: planName,
		revenue,
		margin,
		target_margin: "0.4000",
	};
}

function samplePage(cursor: string): SamplePage {
	const page = SAMPLE_PAGES.get(cursor);
	if (page === undefined) {
		throw new Error(`gallery sample page missing cursor=${cursor}`);
	}
	return page;
}
