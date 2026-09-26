import type { DecisionResponse } from "@/client";
import { ModelLabel } from "@/components/model-label";
import type { ModelReference } from "@/components/pickers/model-picker";

interface DecisionModelProps {
	decision: Pick<
		DecisionResponse,
		"outcome" | "provider" | "model" | "requested_provider" | "requested_model"
	>;
	modelDisplayName: (model: ModelReference) => string | null;
}

/**
 * The model a decision runs by display name, and under it "from {requested
 * model}" when the decision routed the request to another model.
 */
export function DecisionModel({
	decision,
	modelDisplayName,
}: DecisionModelProps) {
	const requestedModel = {
		provider: decision.requested_provider,
		model: decision.requested_model,
	};
	return (
		<div className="flex flex-col">
			<ModelLabel
				provider={decision.provider}
				model={decision.model}
				displayName={modelDisplayName(decision)}
			/>
			{decision.outcome === "route" && (
				<span className="text-muted-foreground text-xs">
					from{" "}
					<ModelLabel
						provider={requestedModel.provider}
						model={requestedModel.model}
						displayName={modelDisplayName(requestedModel)}
					/>
				</span>
			)}
		</div>
	);
}
