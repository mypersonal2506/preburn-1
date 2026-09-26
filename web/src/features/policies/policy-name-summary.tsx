import type { PolicyResponse } from "@/client";
import { Skeleton } from "@/components/ui/skeleton";
import {
	type PolicyNames,
	policySummary,
} from "@/features/policies/policy-summary";

interface PolicyNameSummaryProps {
	policy: PolicyResponse;
	names: PolicyNames | null;
}

/**
 * A policy's name over its one-line sentence from policySummary, for the
 * first cell of a policy table. A skeleton stands in for the sentence while
 * names, the plan and customer names it reads, is null.
 */
export function PolicyNameSummary({ policy, names }: PolicyNameSummaryProps) {
	return (
		<div className="flex max-w-xl flex-col gap-0.5 whitespace-normal">
			<span className="font-medium">{policy.name}</span>
			{names === null ? (
				<Skeleton className="h-4 w-64" />
			) : (
				<span className="text-muted-foreground text-xs">
					{policySummary(policy, names)}
				</span>
			)}
		</div>
	);
}
