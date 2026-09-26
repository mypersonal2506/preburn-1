import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { createPlanMutation } from "@/client/@tanstack/react-query.gen";
import { PageHeader } from "@/components/page-header";
import { storeSavedPlan } from "@/features/plans/plan-cache";
import { createPlanBody, newPlanDraft } from "@/features/plans/plan-draft";
import { PlanSentence } from "@/features/plans/plan-sentence";
import { getEnvironment } from "@/lib/environment-store";

export function PlanNewPage() {
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const [newDraft] = useState(newPlanDraft);
	const createPlan = useMutation({
		...createPlanMutation(),
		onMutate: getEnvironment,
		onSuccess: async (plan, _variables, createdEnvironment) => {
			storeSavedPlan(queryClient, plan, createdEnvironment);
			toast.success("Plan created");
			if (createdEnvironment === getEnvironment()) {
				await navigate({ to: "/plans/$planId", params: { planId: plan.id } });
			}
		},
	});

	return (
		<>
			<PageHeader title="New plan" />
			<PlanSentence
				savedDraft={newDraft}
				action="create"
				facts={null}
				planSave={{
					pending: createPlan.isPending,
					error: createPlan.error,
					save: (draft, onSaved) =>
						createPlan.mutate(
							{ body: createPlanBody(draft) },
							{ onSuccess: onSaved },
						),
				}}
			/>
		</>
	);
}
