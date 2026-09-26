import { useQuery } from "@tanstack/react-query";
import { getRouteApi, Link, linkOptions } from "@tanstack/react-router";
import { SearchXIcon, ShieldIcon, TriangleAlertIcon } from "lucide-react";
import type { ReactNode } from "react";
import type { PolicyResponse } from "@/client";
import { listPoliciesOptions } from "@/client/@tanstack/react-query.gen";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { FilterBar } from "@/components/filter-bar";
import { FilterChip } from "@/components/filter-chip";
import { OutcomeBadge } from "@/components/outcome-badge";
import { PageHeader } from "@/components/page-header";
import { RelativeTime } from "@/components/relative-time";
import { StatusDot } from "@/components/status-dot";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useCursorPagination } from "@/components/use-cursor-pagination";
import { PolicyNameSummary } from "@/features/policies/policy-name-summary";
import type { PolicyListSearch } from "@/features/policies/policy-search";
import {
	type PolicyNames,
	whoPhrase,
} from "@/features/policies/policy-summary";
import { usePolicyNames } from "@/features/policies/use-policy-names";

type PolicyStatusFilter = PolicyListSearch["status"];

interface StatusChip {
	status: PolicyStatusFilter;
	label: string;
}

const STATUS_CHIPS: readonly StatusChip[] = [
	{ status: "active", label: "Active" },
	{ status: "disabled", label: "Disabled" },
	{ status: "archived", label: "Archived" },
	{ status: "all", label: "All" },
];

const policyListRoute = getRouteApi("/_app/policies/");

export function PolicyListPage() {
	const search = policyListRoute.useSearch();
	const navigate = policyListRoute.useNavigate();
	const pagination = useCursorPagination(search.status);
	const policiesQuery = useQuery(
		listPoliciesOptions({
			query: {
				status: search.status === "all" ? undefined : search.status,
				cursor: pagination.cursor,
			},
		}),
	);
	const names = usePolicyNames(policiesQuery.data?.items ?? []);

	function emptyState(): ReactNode {
		if (policiesQuery.isError) {
			return (
				<EmptyState
					icon={TriangleAlertIcon}
					title="Something went wrong"
					action={
						<Button variant="outline" onClick={() => policiesQuery.refetch()}>
							Try again
						</Button>
					}
				/>
			);
		}
		if (search.status === "disabled" || search.status === "archived") {
			return (
				<EmptyState icon={SearchXIcon} title={`No ${search.status} policies`} />
			);
		}
		return (
			<EmptyState
				icon={ShieldIcon}
				title={
					search.status === "active" ? "No active policies" : "No policies yet"
				}
				description="Policies allow, route, cap or deny requests."
				action={
					<Button asChild>
						<Link to="/policies/new">New policy</Link>
					</Button>
				}
			/>
		);
	}

	return (
		<>
			<PageHeader
				title="Policies"
				actions={
					<Button asChild>
						<Link to="/policies/new">New policy</Link>
					</Button>
				}
			/>
			<FilterBar label="Policy filters">
				{STATUS_CHIPS.map((chip) => (
					<FilterChip
						key={chip.status}
						label={chip.label}
						pressed={search.status === chip.status}
						onClick={() => void navigate({ search: { status: chip.status } })}
					/>
				))}
			</FilterBar>
			<DataTable
				label="Policies"
				columns={policyColumns(names)}
				rows={policiesQuery.isError ? [] : policiesQuery.data?.items}
				rowKey={(policy) => policy.id}
				rowLink={(policy) =>
					linkOptions({
						to: "/policies/$policyId",
						params: { policyId: policy.id },
					})
				}
				emptyState={emptyState()}
				pagination={pagination}
				nextCursor={policiesQuery.data?.next_cursor}
			/>
		</>
	);
}

function policyColumns(
	names: PolicyNames | null,
): DataTableColumn<PolicyResponse>[] {
	const now = new Date();
	return [
		{
			header: "Policy",
			cell: (policy) => <PolicyNameSummary policy={policy} names={names} />,
		},
		{
			header: "Applies to",
			cell: (policy) =>
				names === null ? (
					<Skeleton className="h-4 w-24" />
				) : (
					appliesTo(policy, names)
				),
		},
		{
			header: "Outcome",
			cell: (policy) => (
				<OutcomeBadge outcome={policy.action.outcome} kind="policy" />
			),
		},
		{
			header: "Status",
			cell: (policy) => <StatusDot status={policy.status} />,
		},
		{
			header: "Updated",
			cell: (policy) => (
				<RelativeTime timestamp={policy.updated_at} now={now} />
			),
		},
	];
}

function appliesTo(policy: PolicyResponse, names: PolicyNames): string {
	return policy.level === "everyone"
		? "All customers"
		: whoPhrase(policy, names);
}
