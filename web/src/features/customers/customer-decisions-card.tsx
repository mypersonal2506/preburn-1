import { linkOptions } from "@tanstack/react-router";
import { ListChecksIcon } from "lucide-react";
import type { CustomerDecisionResponse } from "@/client";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { FeatureLabel } from "@/components/feature-label";
import { OutcomeBadge } from "@/components/outcome-badge";
import type { ModelReference } from "@/components/pickers/model-picker";
import { RelativeTime } from "@/components/relative-time";
import { SectionCard } from "@/components/section-card";
import { StatusDot } from "@/components/status-dot";
import { useModelDisplayNames } from "@/components/use-model-display-names";
import { DecisionModel } from "@/features/decisions/decision-model";
import { formatMoney } from "@/lib/format";

interface CustomerDecisionsCardProps {
	decisions: readonly CustomerDecisionResponse[];
}

/**
 * The Recent decisions card of a customer: its 10 latest decisions, newest
 * first, each row opening the decision. Times read relative within 7 days,
 * with the local date and time in a tooltip, and models by display name.
 */
export function CustomerDecisionsCard({
	decisions,
}: CustomerDecisionsCardProps) {
	const modelDisplayName = useModelDisplayNames(
		decisions.flatMap((decision) => [
			decision,
			{
				provider: decision.requested_provider,
				model: decision.requested_model,
			},
		]),
	);
	return (
		<SectionCard title="Recent decisions">
			<DataTable
				label="Recent decisions"
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
					<EmptyState icon={ListChecksIcon} title="No decisions yet" />
				}
			/>
		</SectionCard>
	);
}

function decisionColumns(
	modelDisplayName: (model: ModelReference) => string | null,
	now: Date,
): DataTableColumn<CustomerDecisionResponse>[] {
	return [
		{
			header: "Time",
			cell: (decision) => (
				<RelativeTime timestamp={decision.created_at} now={now} />
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
		{
			header: "Status",
			cell: (decision) => <StatusDot status={decision.status} />,
		},
	];
}
