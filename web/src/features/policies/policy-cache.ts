import type { QueryClient } from "@tanstack/react-query";
import type { PolicyResponse } from "@/client";
import {
	getDashboardOnboardingQueryKey,
	getPolicyQueryKey,
	listPoliciesQueryKey,
} from "@/client/@tanstack/react-query.gen";
import { type Environment, getEnvironment } from "@/lib/environment-store";

/**
 * Puts a policy the API just returned from a save sent in savedEnvironment
 * into the query cache of its detail page, and marks stale every policy
 * list, the plan pages' policy sections included, and the onboarding
 * status, whose "Create a policy" step counts active policies. Query keys
 * hold no environment, so after a switch away from savedEnvironment the
 * policy stays out of the cache. Resolves once the active lists have
 * refetched.
 */
export async function storeSavedPolicy(
	queryClient: QueryClient,
	policy: PolicyResponse,
	savedEnvironment: Environment,
): Promise<void> {
	if (savedEnvironment === getEnvironment()) {
		queryClient.setQueryData(
			getPolicyQueryKey({ path: { policy_id: policy.id } }),
			policy,
		);
	}
	await Promise.all([
		queryClient.invalidateQueries({ queryKey: listPoliciesQueryKey() }),
		queryClient.invalidateQueries({
			queryKey: getDashboardOnboardingQueryKey(),
		}),
	]);
}
