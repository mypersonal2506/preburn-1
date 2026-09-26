import type {
	LossCustomerResponse,
	OnboardingResponse,
	OverviewResponse,
	PlanMarginResponse,
} from "@/client";

/** The onboarding state of an environment that has sent its first check. */
export const onboardingWithFirstCheck: OnboardingResponse = {
	first_check_at: "2026-09-20T08:00:00Z",
	has_api_key: true,
	has_plan: true,
	has_policy: true,
	has_revenue: true,
};

/** The onboarding state of an environment before its first check. */
export const onboardingWithoutFirstCheck: OnboardingResponse = {
	...onboardingWithFirstCheck,
	first_check_at: null,
	has_policy: false,
	has_revenue: false,
};

/** A plan below its target margin. */
export const studioPlanMargin: PlanMarginResponse = {
	plan_id: "pln_01jbvagescfn78y0938nkrkayd",
	name: "Studio",
	mode: "margin_target",
	target_margin: "0.5000",
	customer_count: 3,
	revenue: "240.500000000",
	cost: "170.000000000",
	margin: "0.2931",
	below_target: true,
};

/** A customer losing money whose latest policy decision denied a request. */
export const lumberLossCustomer: LossCustomerResponse = {
	id: "cust_01jbvagescfn78y0938nkrkaye",
	external_id: "lumber",
	display_name: "Lumber Co",
	plan_id: studioPlanMargin.plan_id,
	revenue: "30.000000000",
	cost: "54.000000000",
	margin: "-0.8000",
	latest_policy_outcome: {
		outcome: "deny",
		policy_id: "pol_01jbvagescfn78y0938nkrkayf",
		decided_at: "2026-09-26T09:12:00Z",
	},
};

/** An overview of the current period with every section filled. */
export const overviewFixture: OverviewResponse = {
	period: "current",
	revenue: "1240.500000000",
	cost: "480.250000000",
	margin: "0.6129",
	target_margin: "0.4000",
	daily: [
		{ date: "2026-09-24", revenue: "400.000000000", cost: "150.000000000" },
		{ date: "2026-09-25", revenue: "440.500000000", cost: "170.250000000" },
		{ date: "2026-09-26", revenue: "400.000000000", cost: "160.000000000" },
	],
	plan_margins: [
		{
			plan_id: "pln_01jbvagescfn78y0938nkrkayg",
			name: "Creator",
			mode: "margin_target",
			target_margin: "0.4000",
			customer_count: 12,
			revenue: "1000.000000000",
			cost: "310.250000000",
			margin: "0.6898",
			below_target: false,
		},
		studioPlanMargin,
	],
	loss_customers: [lumberLossCustomer],
	cost_avoided: {
		total: "95.400000000",
		denied: "40.000000000",
		routed: "50.400000000",
		capped: "5.000000000",
		changed_decision_count: 202,
	},
	decision_counts: { allow: 812, route: 120, cap: 50, deny: 32 },
	uncosted_count: 2,
	attention: {
		plans_below_target: 1,
		customers_above_pace: 3,
		dropped_reports: 0,
		uncosted_requests: 2,
	},
	policy_changes: [
		{
			id: "pol_01jbvagescfn78y0938nkrkayf",
			name: "Heavy video users",
			status: "active",
			version: 3,
			change: "updated",
			created_at: "2026-09-10T12:00:00Z",
			updated_at: "2026-09-25T16:30:00Z",
		},
		{
			id: "pol_01jbvagescfn78y0938nkrkayh",
			name: "Stop at allowance",
			status: "disabled",
			version: 1,
			change: "created",
			created_at: "2026-09-12T08:00:00Z",
			updated_at: "2026-09-12T08:00:00Z",
		},
	],
};

/**
 * An overview with a customer without a plan and a customer to watch that no
 * policy decided.
 */
export const overviewWithNullRows: OverviewResponse = {
	...overviewFixture,
	plan_margins: [
		...overviewFixture.plan_margins,
		{
			plan_id: null,
			name: null,
			mode: null,
			target_margin: null,
			customer_count: 4,
			revenue: "0.500000000",
			cost: "0.120000000",
			margin: "0.7600",
			below_target: false,
		},
	],
	loss_customers: [
		...overviewFixture.loss_customers,
		{
			...lumberLossCustomer,
			id: "cust_01jbvagescfn78y0938nkrkayj",
			external_id: "cedar",
			display_name: null,
			margin: "-0.2000",
			latest_policy_outcome: null,
		},
	],
};

/** An overview of an environment with checks but nothing in the period. */
export const emptyOverviewFixture: OverviewResponse = {
	...overviewFixture,
	revenue: "0.000000000",
	cost: "0.000000000",
	margin: null,
	target_margin: null,
	daily: [{ date: "2026-09-26", revenue: "0.000000000", cost: "0.000000000" }],
	plan_margins: [],
	loss_customers: [],
	cost_avoided: {
		total: "0.000000000",
		denied: "0.000000000",
		routed: "0.000000000",
		capped: "0.000000000",
		changed_decision_count: 0,
	},
	decision_counts: { allow: 0, route: 0, cap: 0, deny: 0 },
	uncosted_count: 0,
	attention: {
		plans_below_target: 0,
		customers_above_pace: 0,
		dropped_reports: 0,
		uncosted_requests: 0,
	},
	policy_changes: [],
};
