import { useQuery } from "@tanstack/react-query";
import { getRouteApi, Link, linkOptions } from "@tanstack/react-router";
import { SearchXIcon, TriangleAlertIcon, UsersIcon } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import type { CustomerMarginResponse } from "@/client";
import { listDashboardCustomersOptions } from "@/client/@tanstack/react-query.gen";
import { CustomerLabel } from "@/components/customer-label";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { FilterBar } from "@/components/filter-bar";
import { FilterChip } from "@/components/filter-chip";
import { MarginBar } from "@/components/margin-bar";
import { MarginPill } from "@/components/margin-pill";
import { PageHeader } from "@/components/page-header";
import { PlanPicker } from "@/components/pickers/plan-picker";
import { Button } from "@/components/ui/button";
import { useCursorPagination } from "@/components/use-cursor-pagination";
import type { CustomerListSearch } from "@/features/customers/customer-list-search";
import { toApiProblem } from "@/lib/api-problem";
import { formatMoney, formatPace, formatPeriodEnd } from "@/lib/format";

type CustomerSort = CustomerListSearch["sort"];

type RevenueFilter = CustomerListSearch["revenue_filter"];

type RevenueFilterChip = {
	revenueFilter: RevenueFilter;
	label: string;
};

const SEARCH_DEBOUNCE_MILLISECONDS = 250;
const ENVIRONMENT_TOO_LARGE_CODE = "environment_too_large";

const REVENUE_FILTER_CHIPS: readonly RevenueFilterChip[] = [
	{ revenueFilter: "all", label: "All" },
	{ revenueFilter: "paying", label: "Paying" },
	{ revenueFilter: "free", label: "Free" },
];

const CUSTOMER_COLUMNS: readonly DataTableColumn<
	CustomerMarginResponse,
	CustomerSort
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
	{ header: "Plan", cell: (customer) => customer.plan_name ?? "No plan" },
	{
		header: "Revenue",
		cell: (customer) => formatMoney(customer.revenue),
		numeric: true,
		sortKey: "revenue",
	},
	{
		header: "AI cost",
		cell: (customer) => formatMoney(customer.cost),
		numeric: true,
		sortKey: "cost",
	},
	{
		header: "Margin",
		cell: (customer) => (
			<div className="flex w-24 flex-col items-start gap-1.5">
				<MarginPill
					margin={customer.margin}
					targetMargin={customer.target_margin}
				/>
				<MarginBar
					margin={customer.margin}
					targetMargin={customer.target_margin}
				/>
			</div>
		),
		sortKey: "margin",
	},
	{
		header: "Pace",
		cell: (customer) => formatPace(customer.pace),
		numeric: true,
		sortKey: "pace",
	},
	{
		header: "Period ends",
		cell: (customer) => formatPeriodEnd(customer.period_end),
	},
];

const customerListRoute = getRouteApi("/_app/customers/");

export function CustomerListPage() {
	const search = customerListRoute.useSearch();
	const navigate = customerListRoute.useNavigate();
	const [requestedSearch, setRequestedSearch] = useState(search.search);

	useEffect(() => {
		const timer = window.setTimeout(
			() => setRequestedSearch(search.search),
			SEARCH_DEBOUNCE_MILLISECONDS,
		);
		return () => window.clearTimeout(timer);
	}, [search.search]);

	const listFilters = {
		search: requestedSearch,
		plan_id: search.plan_id,
		revenue_filter: search.revenue_filter,
		sort: search.sort,
		direction: search.direction,
	};
	const pagination = useCursorPagination(JSON.stringify(listFilters));
	const customersQuery = useQuery(
		listDashboardCustomersOptions({
			query: { ...listFilters, cursor: pagination.cursor },
		}),
	);
	const filtered =
		search.search !== undefined ||
		search.plan_id !== undefined ||
		search.revenue_filter !== "all";

	function changeSearch(
		changes: Partial<CustomerListSearch>,
		replace = false,
	): void {
		void navigate({
			search: (previous) => ({ ...previous, ...changes }),
			replace,
		});
	}

	function emptyState(): ReactNode {
		if (customersQuery.isError) {
			if (
				toApiProblem(customersQuery.error).code === ENVIRONMENT_TOO_LARGE_CODE
			) {
				return (
					<EmptyState
						icon={TriangleAlertIcon}
						title="Too many customers to list"
						description="The list covers up to 50,000 active customers."
					/>
				);
			}
			return (
				<EmptyState
					icon={TriangleAlertIcon}
					title="Something went wrong"
					action={
						<Button variant="outline" onClick={() => customersQuery.refetch()}>
							Try again
						</Button>
					}
				/>
			);
		}
		if (filtered) {
			return (
				<EmptyState
					icon={SearchXIcon}
					title="No customers match"
					action={
						<Button
							variant="outline"
							onClick={() =>
								changeSearch({
									search: undefined,
									plan_id: undefined,
									revenue_filter: "all",
								})
							}
						>
							Clear filters
						</Button>
					}
				/>
			);
		}
		return (
			<EmptyState
				icon={UsersIcon}
				title="No customers yet"
				description="Customers appear after their first check."
				action={
					<Button asChild>
						<Link to="/developers/get-started">Get started</Link>
					</Button>
				}
			/>
		);
	}

	return (
		<>
			<PageHeader title="Customers" />
			<FilterBar
				label="Customer filters"
				search={{
					value: search.search ?? "",
					placeholder: "Search customers",
					onChange: (text) =>
						changeSearch({ search: text === "" ? undefined : text }, true),
				}}
			>
				<PlanPicker
					value={search.plan_id ?? null}
					onChange={(plan) => changeSearch({ plan_id: plan.id })}
					trigger={(planName) => (
						<FilterChip
							label="Plan"
							value={planName ?? undefined}
							onClear={() => changeSearch({ plan_id: undefined })}
						/>
					)}
				/>
				{REVENUE_FILTER_CHIPS.map((chip) => (
					<FilterChip
						key={chip.revenueFilter}
						label={chip.label}
						pressed={search.revenue_filter === chip.revenueFilter}
						onClick={() => changeSearch({ revenue_filter: chip.revenueFilter })}
					/>
				))}
			</FilterBar>
			<DataTable
				label="Customers"
				columns={CUSTOMER_COLUMNS}
				rows={customersQuery.isError ? [] : customersQuery.data?.items}
				rowKey={(customer) => customer.id}
				rowLink={(customer) =>
					linkOptions({
						to: "/customers/$customerId",
						params: { customerId: customer.id },
					})
				}
				emptyState={emptyState()}
				pagination={pagination}
				nextCursor={customersQuery.data?.next_cursor}
				sort={{ key: search.sort, direction: search.direction }}
				onSortChange={(sort) =>
					changeSearch({ sort: sort.key, direction: sort.direction })
				}
			/>
		</>
	);
}
