import { Link, linkOptions } from "@tanstack/react-router";
import { ShieldCheckIcon } from "lucide-react";
import type { PolicyChangeResponse } from "@/client";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { RelativeTime } from "@/components/relative-time";
import { SectionCard } from "@/components/section-card";
import { StatusDot } from "@/components/status-dot";
import { Button } from "@/components/ui/button";

interface OverviewPolicyChangesProps {
	policyChanges: readonly PolicyChangeResponse[];
}

const POLICY_CHANGE_COLUMNS: readonly DataTableColumn<PolicyChangeResponse>[] =
	[
		{ header: "Policy", cell: (policyChange) => policyChange.name },
		{
			header: "Change",
			cell: (policyChange) =>
				policyChange.change === "created"
					? "Created"
					: `Updated to version ${policyChange.version}`,
		},
		{
			header: "Status",
			cell: (policyChange) => <StatusDot status={policyChange.status} />,
		},
		{
			header: "When",
			cell: (policyChange) => (
				<RelativeTime timestamp={policyChange.updated_at} now={new Date()} />
			),
		},
	];

/**
 * The Recent policy changes card: the 5 most recently created or updated
 * policies whatever the period, newest first. Rows open the policy.
 */
export function OverviewPolicyChanges({
	policyChanges,
}: OverviewPolicyChangesProps) {
	return (
		<SectionCard title="Recent policy changes">
			<DataTable
				label="Recent policy changes"
				columns={POLICY_CHANGE_COLUMNS}
				rows={policyChanges}
				rowKey={(policyChange) => policyChange.id}
				rowLink={(policyChange) =>
					linkOptions({
						to: "/policies/$policyId",
						params: { policyId: policyChange.id },
					})
				}
				emptyState={
					<EmptyState
						icon={ShieldCheckIcon}
						title="No policies yet"
						action={
							<Button size="sm" asChild>
								<Link to="/policies/new">New policy</Link>
							</Button>
						}
					/>
				}
			/>
		</SectionCard>
	);
}
