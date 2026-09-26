import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { getPlanOptions } from "@/client/@tanstack/react-query.gen";
import type { RecordTitle } from "@/components/app-header";
import { PlanDetailPage } from "@/features/plans/plan-detail-page";

export const Route = createFileRoute("/_app/plans/$planId")({
	staticData: { title: "Plan" },
	context: ({ params }): { recordTitle: RecordTitle } => ({
		recordTitle: { id: params.planId, useName: usePlanName },
	}),
	component: PlanDetailPage,
});

function usePlanName(planId: string): string | undefined {
	return useQuery({
		...getPlanOptions({ path: { plan_id: planId } }),
		select: (plan) => plan.name,
	}).data;
}
