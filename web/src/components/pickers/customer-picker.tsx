import { keepPreviousData, useInfiniteQuery } from "@tanstack/react-query";
import { type ReactElement, useState } from "react";
import type { CustomerMarginResponse } from "@/client";
import { listDashboardCustomersInfiniteOptions } from "@/client/@tanstack/react-query.gen";
import {
	type PickerFieldProps,
	type PickerOption,
	PickerShell,
} from "@/components/pickers/picker-shell";

/** The fields of a customer that CustomerPicker shows for its value. */
export type CustomerChoice = Pick<
	CustomerMarginResponse,
	"id" | "external_id" | "display_name"
>;

/** Props of CustomerPicker. */
export interface CustomerPickerProps extends PickerFieldProps {
	value: CustomerChoice | null;
	onChange: (customer: CustomerMarginResponse) => void;
}

/**
 * Picks a customer of the environment, searching external ids and display
 * names on the server. Options show the display name, or the external id
 * when the customer has none, with the plan name. The value carries its own
 * display name and external id because the API has no light lookup by
 * customer id.
 */
export function CustomerPicker({
	value,
	onChange,
	...fieldProps
}: CustomerPickerProps): ReactElement {
	const [search, setSearch] = useState("");
	const customersQuery = useInfiniteQuery({
		...listDashboardCustomersInfiniteOptions({ query: { search } }),
		initialPageParam: {},
		getNextPageParam: (page) => page.next_cursor,
		placeholderData: keepPreviousData,
	});

	const customers =
		customersQuery.data?.pages.flatMap((page) => page.items) ?? [];

	return (
		<PickerShell
			{...fieldProps}
			placeholder="Select a customer"
			searchPlaceholder="Search customers"
			emptyText="No customers found"
			selectedValue={value?.id ?? null}
			selectedLabel={value === null ? null : customerName(value)}
			groups={[{ options: customers.map(customerOption) }]}
			loading={customersQuery.isPending}
			error={customersQuery.error}
			onSelect={onChange}
			onSearchChange={setSearch}
			nextPage={
				customersQuery.hasNextPage
					? {
							loading: customersQuery.isFetchingNextPage,
							load: () => void customersQuery.fetchNextPage(),
						}
					: undefined
			}
		/>
	);
}

function customerOption(
	customer: CustomerMarginResponse,
): PickerOption<CustomerMarginResponse> {
	return {
		value: customer.id,
		label: customerName(customer),
		detail: customer.plan_name,
		choice: customer,
	};
}

function customerName(customer: CustomerChoice): string {
	return customer.display_name ?? customer.external_id;
}
