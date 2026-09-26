import type { QueryClient } from "@tanstack/react-query";
import type { PlanResponse } from "@/client";
import {
	getDashboardOnboardingQueryKey,
	getPlanQueryKey,
	listPlansQueryKey,
} from "@/client/@tanstack/react-query.gen";
import { type Environment, getEnvironment } from "@/lib/environment-store";

/**
 * Puts a plan the API just returned from a save sent in savedEnvironment
 * into the query cache of its detail page, and marks stale every plan list,
 * the plans page and the plan pickers included, so they load it, and the
 * onboarding status, whose "Create a plan" step counts active plans. Query
 * keys hold no environment, so after a switch away from savedEnvironment the
 * plan stays out of the cache.
 */
export function storeSavedPlan(
	queryClient: QueryClient,
	plan: PlanResponse,
	savedEnvironment: Environment,
): void {
	if (savedEnvironment === getEnvironment()) {
		queryClient.setQueryData(
			getPlanQueryKey({ path: { plan_id: plan.id } }),
			plan,
		);
	}
	void queryClient.invalidateQueries({ queryKey: listPlansQueryKey() });
	void queryClient.invalidateQueries({
		queryKey: getDashboardOnboardingQueryKey(),
	});
}
