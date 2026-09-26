import { useQuery } from "@tanstack/react-query";
import { Link, linkOptions } from "@tanstack/react-router";
import { ShieldIcon, TriangleAlertIcon } from "lucide-react";
import type { ReactNode } from "react";
import type { PolicyResponse } from "@/client";
import { listPoliciesOptions } from "@/client/@tanstack/react-query.gen";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { OutcomeBadge } from "@/components/outcome-badge";
import { SectionCard } from "@/components/section-card";
import { StatusDot } from "@/components/status-dot";
import { Button } from "@/components/ui/button";
import { useCursorPagination } from "@/components/use-cursor-pagination";
import { PolicyNameSummary } from "@/features/policies/policy-name-summary";
import type { PolicyNames } from "@/features/policies/policy-summary";
import { usePolicyNames } from "@/features/policies/use-policy-names";

interface PlanPoliciesCardProps {
	planId: string;
}

/**
 * The "Policies on this plan" card of a plan: the policies whose level is
 * the plan, in every status, newest first and a page at a time, each with
 * its one-line sentence, its outcome as an action and its status, rows
 * opening the policy.
 */
export function PlanPoliciesCard({ planId }: PlanPoliciesCardProps) {
	const pagination = useCursorPagination(planId);
	const policiesQuery = useQuery(
		listPoliciesOptions({
			query: { plan_id: planId, cursor: pagination.cursor },
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
		return (
			<EmptyState
				icon={ShieldIcon}
				title="No policies on this plan"
				action={
					<Button asChild size="sm">
						<Link to="/policies/new">New policy</Link>
					</Button>
				}
			/>
		);
	}

	return (
		<SectionCard title="Policies on this plan">
			<DataTable
				label="Policies on this plan"
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
		</SectionCard>
	);
}

function policyColumns(
	names: PolicyNames | null,
): DataTableColumn<PolicyResponse>[] {
	return [
		{
			header: "Policy",
			cell: (policy) => <PolicyNameSummary policy={policy} names={names} />,
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
	];
}
