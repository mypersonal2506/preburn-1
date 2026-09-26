import { ActivityIcon } from "lucide-react";
import type { CustomerUsageResponse } from "@/client";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { FeatureLabel } from "@/components/feature-label";
import { ModelLabel } from "@/components/model-label";
import type { ModelReference } from "@/components/pickers/model-picker";
import { SectionCard } from "@/components/section-card";
import { useModelDisplayNames } from "@/components/use-model-display-names";
import { formatCount, formatMoney } from "@/lib/format";

interface CustomerUsageCardProps {
	usage: readonly CustomerUsageResponse[];
}

/**
 * The usage card of a customer: the current period's requests by feature,
 * provider and model, highest cost first, models by display name, with the
 * requests that have no price counted as Unpriced.
 */
export function CustomerUsageCard({ usage }: CustomerUsageCardProps) {
	const modelDisplayName = useModelDisplayNames(usage);
	return (
		<SectionCard title="Usage by feature and model">
			<DataTable
				label="Usage by feature and model"
				columns={usageColumns(modelDisplayName)}
				rows={usage}
				rowKey={(modelUsage) =>
					`${modelUsage.feature} ${modelUsage.provider} ${modelUsage.model}`
				}
				emptyState={
					<EmptyState icon={ActivityIcon} title="No usage this period" />
				}
			/>
		</SectionCard>
	);
}

function usageColumns(
	modelDisplayName: (model: ModelReference) => string | null,
): DataTableColumn<CustomerUsageResponse>[] {
	return [
		{
			header: "Feature",
			cell: (modelUsage) => <FeatureLabel feature={modelUsage.feature} />,
		},
		{
			header: "Model",
			cell: (modelUsage) => (
				<ModelLabel
					provider={modelUsage.provider}
					model={modelUsage.model}
					displayName={modelDisplayName(modelUsage)}
				/>
			),
		},
		{
			header: "Requests",
			cell: (modelUsage) => formatCount(modelUsage.request_count),
			numeric: true,
		},
		{
			header: "Unpriced",
			cell: (modelUsage) => formatCount(modelUsage.uncosted_count),
			numeric: true,
		},
		{
			header: "AI cost",
			cell: (modelUsage) => formatMoney(modelUsage.cost),
			numeric: true,
		},
	];
}
