import { useQuery } from "@tanstack/react-query";
import {
	getDashboardCustomerOptions,
	getPolicyOptions,
} from "@/client/@tanstack/react-query.gen";
import { FilterBar } from "@/components/filter-bar";
import { FilterChip } from "@/components/filter-chip";
import {
	type CustomerChoice,
	CustomerPicker,
} from "@/components/pickers/customer-picker";
import { FeaturePicker } from "@/components/pickers/feature-picker";
import type { DecisionFilters } from "@/features/decisions/decision-filters";
import { toApiProblem } from "@/lib/api-problem";
import { decisionOutcomes, outcomeStyles } from "@/lib/outcomes";

interface DecisionFilterBarProps {
	filters: DecisionFilters;
	onChange: (filters: DecisionFilters) => void;
}

interface CustomerFilterProps {
	customer: CustomerChoice | null;
	placeholderValue: string | undefined;
	onChange: (customerId: string | undefined) => void;
}

interface SelectedCustomerFilterProps {
	customerId: string;
	onChange: (customerId: string | undefined) => void;
}

interface PolicyFilterChipProps {
	policyId: string;
	onClear: () => void;
}

const LOADING_TEXT = "Loading";
const UNKNOWN_TEXT = "Unknown";
const NOT_FOUND_CODE = "not_found";

/**
 * The filters of the decisions list in one row: All and one chip per
 * outcome, a customer and a feature picker on filter chips, and, when the
 * list is filtered by a policy, a policy chip that clears the filter. The
 * chosen customer and policy show by name, or as "Unknown" when the
 * environment has no such record, such as after an environment switch, and
 * their chips still clear.
 */
export function DecisionFilterBar({
	filters,
	onChange,
}: DecisionFilterBarProps) {
	function changeCustomer(customerId: string | undefined): void {
		onChange({ ...filters, customer_id: customerId });
	}

	return (
		<FilterBar label="Decision filters">
			<FilterChip
				label="All"
				pressed={filters.outcome === undefined}
				onClick={() => onChange({ ...filters, outcome: undefined })}
			/>
			{decisionOutcomes.map((outcome) => (
				<FilterChip
					key={outcome}
					label={outcomeStyles[outcome].pastTenseLabel}
					pressed={filters.outcome === outcome}
					onClick={() => onChange({ ...filters, outcome })}
				/>
			))}
			{filters.customer_id === undefined ? (
				<CustomerFilter
					customer={null}
					placeholderValue={undefined}
					onChange={changeCustomer}
				/>
			) : (
				<SelectedCustomerFilter
					customerId={filters.customer_id}
					onChange={changeCustomer}
				/>
			)}
			<FeaturePicker
				value={filters.feature ?? null}
				onChange={(feature) => onChange({ ...filters, feature })}
				trigger={(label) => (
					<FilterChip
						label="Feature"
						value={label ?? undefined}
						onClear={() => onChange({ ...filters, feature: undefined })}
					/>
				)}
			/>
			{filters.policy_id !== undefined && (
				<PolicyFilterChip
					policyId={filters.policy_id}
					onClear={() => onChange({ ...filters, policy_id: undefined })}
				/>
			)}
		</FilterBar>
	);
}

function CustomerFilter({
	customer,
	placeholderValue,
	onChange,
}: CustomerFilterProps) {
	return (
		<CustomerPicker
			value={customer}
			onChange={(chosen) => onChange(chosen.id)}
			trigger={(label) => (
				<FilterChip
					label="Customer"
					value={label ?? placeholderValue}
					onClear={() => onChange(undefined)}
				/>
			)}
		/>
	);
}

function SelectedCustomerFilter({
	customerId,
	onChange,
}: SelectedCustomerFilterProps) {
	const customerQuery = useQuery({
		...getDashboardCustomerOptions({ path: { customer_id: customerId } }),
		throwOnError: (error) => toApiProblem(error).code !== NOT_FOUND_CODE,
	});
	return (
		<CustomerFilter
			customer={customerQuery.data ?? null}
			placeholderValue={customerQuery.isError ? UNKNOWN_TEXT : LOADING_TEXT}
			onChange={onChange}
		/>
	);
}

function PolicyFilterChip({ policyId, onClear }: PolicyFilterChipProps) {
	const policyQuery = useQuery({
		...getPolicyOptions({ path: { policy_id: policyId } }),
		throwOnError: (error) => toApiProblem(error).code !== NOT_FOUND_CODE,
	});
	return (
		<FilterChip
			label="Policy"
			value={
				policyQuery.data?.name ??
				(policyQuery.isError ? UNKNOWN_TEXT : LOADING_TEXT)
			}
			onClear={onClear}
		/>
	);
}
