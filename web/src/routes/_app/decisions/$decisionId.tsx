import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { getDashboardDecisionOptions } from "@/client/@tanstack/react-query.gen";
import type { RecordTitle } from "@/components/app-header";
import { decisionTitle } from "@/features/decisions/decision-labels";
import { DecisionPage } from "@/features/decisions/decision-page";

export const Route = createFileRoute("/_app/decisions/$decisionId")({
	staticData: { title: "Decision" },
	context: ({ params }): { recordTitle: RecordTitle } => ({
		recordTitle: { id: params.decisionId, useName: useDecisionTitle },
	}),
	component: DecisionPage,
});

function useDecisionTitle(decisionId: string): string | undefined {
	return useQuery({
		...getDashboardDecisionOptions({ path: { decision_id: decisionId } }),
		select: decisionTitle,
	}).data;
}
