import { useQueries } from "@tanstack/react-query";
import type { CustomerDetailResponse, PolicyResponse } from "@/client";
import {
	getDashboardCustomerOptions,
	getPlanOptions,
} from "@/client/@tanstack/react-query.gen";
import type { CustomerChoice } from "@/components/pickers/customer-picker";
import { useModelDisplayNames } from "@/components/use-model-display-names";
import { customerChoiceName } from "@/features/policies/policy-draft";
import type { PolicyNames } from "@/features/policies/policy-summary";

/**
 * Looks up the names of plans by id, each plan once. Returns null while any
 * loads. A failed lookup throws to the route's error component.
 */
export function usePlanNames(
	planIds: readonly string[],
): ReadonlyMap<string, string> | null {
	const planQueries = useQueries({
		queries: [...new Set(planIds)].map((planId) => ({
			...getPlanOptions({ path: { plan_id: planId } }),
			throwOnError: true,
		})),
	});
	const plans = planQueries.flatMap((query) =>
		query.data === undefined ? [] : [query.data],
	);
	if (plans.length < planQueries.length) {
		return null;
	}
	return new Map(plans.map((plan) => [plan.id, plan.name]));
}

/**
 * Looks up the customers of customer policies by customer id, each customer
 * once, for their names. Each choice holds only the id, external id and
 * display name, so a refetch that changes the customer's signals leaves
 * the choice equal. Returns null while any loads. A failed lookup throws to
 * the route's error component.
 */
export function useCustomerChoices(
	customerIds: readonly string[],
): ReadonlyMap<string, CustomerChoice> | null {
	const customerQueries = useQueries({
		queries: [...new Set(customerIds)].map((customerId) => ({
			...getDashboardCustomerOptions({ path: { customer_id: customerId } }),
			select: customerChoice,
			throwOnError: true,
		})),
	});
	const customers = customerQueries.flatMap((query) =>
		query.data === undefined ? [] : [query.data],
	);
	if (customers.length < customerQueries.length) {
		return null;
	}
	return new Map(customers.map((customer) => [customer.id, customer]));
}

/**
 * The names the summaries of policies read: plan and customer names, null
 * while any loads, and model display names, which fall back to the model
 * name while they load and for models the catalog has no display name for.
 */
export function usePolicyNames(
	policies: readonly PolicyResponse[],
): PolicyNames | null {
	const planNames = usePlanNames(
		policies.flatMap((policy) =>
			policy.plan_id === null ? [] : [policy.plan_id],
		),
	);
	const customers = useCustomerChoices(
		policies.flatMap((policy) =>
			policy.customer_id === null ? [] : [policy.customer_id],
		),
	);
	const modelDisplayName = useModelDisplayNames(
		policies.flatMap((policy) => policy.action.route_chain ?? []),
	);
	if (planNames === null || customers === null) {
		return null;
	}
	return {
		plan: (planId) => lookUp(planNames, planId, "plan"),
		customer: (customerId) =>
			customerChoiceName(lookUp(customers, customerId, "customer")),
		model: (target) => modelDisplayName(target) ?? target.model,
	};
}

function customerChoice(customer: CustomerDetailResponse): CustomerChoice {
	return {
		id: customer.id,
		external_id: customer.external_id,
		display_name: customer.display_name,
	};
}

function lookUp<Value>(
	values: ReadonlyMap<string, Value>,
	id: string,
	kind: string,
): Value {
	const value = values.get(id);
	if (value === undefined) {
		throw new Error(`policy name missing kind=${kind} id=${id}`);
	}
	return value;
}
