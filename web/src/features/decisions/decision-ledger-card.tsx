import { ReceiptIcon } from "lucide-react";
import type { DecisionLedgerEntryResponse } from "@/client";
import { zMeterDescription } from "@/client/zod.gen";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { ModelLabel } from "@/components/model-label";
import type { ModelReference } from "@/components/pickers/model-picker";
import { SectionCard } from "@/components/section-card";
import { requestCostLabel } from "@/features/decisions/decision-labels";
import { formatDateTime } from "@/lib/format";
import { usageLabel } from "@/lib/labels";

interface DecisionLedgerCardProps {
	entries: readonly DecisionLedgerEntryResponse[];
	modelDisplayName: (model: ModelReference) => string | null;
}

/**
 * The usage reported for a decision and the corrections that priced it
 * later, oldest first: when the request ran, its model, usage and cost, and
 * whether the entry is a report or a correction.
 */
export function DecisionLedgerCard({
	entries,
	modelDisplayName,
}: DecisionLedgerCardProps) {
	const columns: DataTableColumn<DecisionLedgerEntryResponse>[] = [
		{
			header: "Occurred",
			cell: (entry) => formatDateTime(entry.occurred_at),
		},
		{
			header: "Model",
			cell: (entry) => (
				<ModelLabel
					provider={entry.provider}
					model={entry.model}
					displayName={modelDisplayName(entry)}
				/>
			),
		},
		{ header: "Usage", cell: ledgerUsageLabel },
		{
			header: "Cost",
			cell: (entry) => requestCostLabel(entry.cost),
			numeric: true,
		},
		{
			header: "Type",
			cell: (entry) => (entry.correction_of === null ? "Report" : "Correction"),
		},
	];
	return (
		<SectionCard title="Ledger entries">
			<DataTable
				label="Ledger entries"
				columns={columns}
				rows={entries}
				rowKey={(entry) => entry.id}
				emptyState={<EmptyState icon={ReceiptIcon} title="No usage reported" />}
			/>
		</SectionCard>
	);
}

function ledgerUsageLabel(entry: DecisionLedgerEntryResponse): string {
	return Object.entries(entry.usage)
		.map(([meter, quantity]) =>
			usageLabel(zMeterDescription.shape.meter.parse(meter), quantity),
		)
		.join(", ");
}
