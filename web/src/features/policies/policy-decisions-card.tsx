import { useQuery } from "@tanstack/react-query";
import { Link, linkOptions } from "@tanstack/react-router";
import { ListChecksIcon, TriangleAlertIcon } from "lucide-react";
import type { ReactElement } from "react";
import type { DecisionResponse } from "@/client";
import { listDashboardDecisionsOptions } from "@/client/@tanstack/react-query.gen";
import { CustomerLabel } from "@/components/customer-label";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { FeatureLabel } from "@/components/feature-label";
import { ModelLabel } from "@/components/model-label";
import { OutcomeBadge } from "@/components/outcome-badge";
import type { ModelReference } from "@/components/pickers/model-picker";
import { RelativeTime } from "@/components/relative-time";
import { SectionCard } from "@/components/section-card";
import { Button } from "@/components/ui/button";
import { useModelDisplayNames } from "@/components/use-model-display-names";
import { formatMoney } from "@/lib/format";

interface PolicyDecisionsCardProps {
	policyId: string;
}

const RECENT_POLICY_DECISIONS_LIMIT = 5;

/**
 * The Recent decisions card of a policy: the 5 latest decisions it decided,
 * newest first, each row opening the decision, and View all opening the
 * decisions list filtered to the policy.
 */
export function PolicyDecisionsCard({
	policyId,
}: PolicyDecisionsCardProps): ReactElement {
	const decisionsQuery = useQuery(
		listDashboardDecisionsOptions({
			query: { policy_id: policyId, limit: RECENT_POLICY_DECISIONS_LIMIT },
		}),
	);
	const decisions = decisionsQuery.data?.items ?? [];
	const modelDisplayName = useModelDisplayNames(decisions);

	return (
		<SectionCard
			title="Recent decisions"
			action={
				<Button asChild variant="outline" size="sm">
					<Link to="/decisions" search={{ policy_id: policyId }}>
						View all
					</Link>
				</Button>
			}
		>
			<DataTable
				label="Recent decisions"
				columns={decisionColumns(modelDisplayName)}
				rows={decisionsQuery.isError ? [] : decisionsQuery.data?.items}
				rowKey={(decision) => decision.id}
				rowLink={(decision) =>
					linkOptions({
						to: "/decisions/$decisionId",
						params: { decisionId: decision.id },
					})
				}
				emptyState={
					decisionsQuery.isError ? (
						<EmptyState
							icon={TriangleAlertIcon}
							title="Something went wrong"
							action={
								<Button
									variant="outline"
									onClick={() => decisionsQuery.refetch()}
								>
									Try again
								</Button>
							}
						/>
					) : (
						<EmptyState icon={ListChecksIcon} title="No decisions yet" />
					)
				}
			/>
		</SectionCard>
	);
}

function decisionColumns(
	modelDisplayName: (model: ModelReference) => string | null,
): DataTableColumn<DecisionResponse>[] {
	const now = new Date();
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
				<ModelLabel
					provider={decision.provider}
					model={decision.model}
					displayName={modelDisplayName(decision)}
				/>
			),
		},
		{
			header: "Outcome",
			cell: (decision) => <OutcomeBadge outcome={decision.outcome} />,
		},
		{
			header: "Cost",
			cell: (decision) =>
				decision.estimated_cost === null
					? "Unpriced"
					: formatMoney(decision.estimated_cost),
			numeric: true,
		},
	];
}
