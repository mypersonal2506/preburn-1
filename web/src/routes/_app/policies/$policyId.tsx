import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { getPolicyOptions } from "@/client/@tanstack/react-query.gen";
import type { RecordTitle } from "@/components/app-header";
import { PolicyDetailPage } from "@/features/policies/policy-detail-page";

export const Route = createFileRoute("/_app/policies/$policyId")({
	staticData: { title: "Policy" },
	context: ({ params }): { recordTitle: RecordTitle } => ({
		recordTitle: { id: params.policyId, useName: usePolicyName },
	}),
	component: PolicyDetailPage,
});

function usePolicyName(policyId: string): string | undefined {
	return useQuery({
		...getPolicyOptions({ path: { policy_id: policyId } }),
		select: (policy) => policy.name,
	}).data;
}
