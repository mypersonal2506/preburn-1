import { useInfiniteQuery } from "@tanstack/react-query";
import { type ReactElement, useEffect } from "react";
import type { PlanResponse } from "@/client";
import { listPlansInfiniteOptions } from "@/client/@tanstack/react-query.gen";
import {
	type PickerFieldProps,
	type PickerOption,
	PickerShell,
} from "@/components/pickers/picker-shell";
import { formatCount, formatMoney, formatPercent } from "@/lib/format";

/** Props of PlanPicker. */
export interface PlanPickerProps extends PickerFieldProps {
	value: string | null;
	onChange: (plan: PlanResponse) => void;
}

const PLAN_PAGE_SIZE = 100;

/**
 * Picks an active plan of the environment by id. It loads every page of
 * plans, so the search covers them all and the trigger can name an archived
 * plan that is already selected. Options show the name, the rule, such as
 * "40.0% margin target" or "$2.00 allowance", and the customer count.
 */
export function PlanPicker({
	value,
	onChange,
	...fieldProps
}: PlanPickerProps): ReactElement {
	const plansQuery = useInfiniteQuery({
		...listPlansInfiniteOptions({ query: { limit: PLAN_PAGE_SIZE } }),
		initialPageParam: {},
		getNextPageParam: (page) => page.next_cursor,
	});
	const {
		hasNextPage,
		isFetchingNextPage,
		isFetchNextPageError,
		fetchNextPage,
	} = plansQuery;

	useEffect(() => {
		if (hasNextPage && !isFetchingNextPage && !isFetchNextPageError) {
			void fetchNextPage();
		}
	}, [hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage]);

	const plans = plansQuery.data?.pages.flatMap((page) => page.items) ?? [];
	const selectedPlan = plans.find((plan) => plan.id === value);

	return (
		<PickerShell
			{...fieldProps}
			placeholder="Select a plan"
			searchPlaceholder="Search plans"
			emptyText="No plans found"
			selectedValue={value}
			selectedLabel={selectedPlan?.name ?? null}
			groups={[
				{
					options: plans
						.filter((plan) => plan.status === "active")
						.map(planOption),
				},
			]}
			loading={plansQuery.isPending || (hasNextPage && !isFetchNextPageError)}
			error={plansQuery.error}
			onSelect={onChange}
		/>
	);
}

function planOption(plan: PlanResponse): PickerOption<PlanResponse> {
	return {
		value: plan.id,
		label: plan.name,
		detail: `${planRule(plan)}, ${customerCount(plan.customer_count)}`,
		choice: plan,
	};
}

function planRule(plan: PlanResponse): string {
	if (plan.mode === "margin_target") {
		return `${formatPercent(plan.target_margin)} margin target`;
	}
	if (plan.allowance === null) {
		throw new Error(`plan allowance missing plan_id=${plan.id}`);
	}
	return `${formatMoney(plan.allowance)} allowance`;
}

function customerCount(count: number): string {
	return `${formatCount(count)} ${count === 1 ? "customer" : "customers"}`;
}
