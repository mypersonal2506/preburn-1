import type {
	CustomerDetailResponse,
	CustomerMarginResponse,
	PageBodyCustomerMarginResponse,
	PageBodyModelResponse,
	PageBodyPlanResponse,
	PlanResponse,
} from "@/client";

const MILLISECONDS_PER_HOUR = 3_600_000;
const MILLISECONDS_PER_DAY = 86_400_000;
const PERIOD_DAYS = 30;
const PERIOD_DAYS_LEFT = 3;
const DISTANT_PERIOD_MONTHS_AHEAD = 2;
const LOCALE = "en-US";

const today = new Date();

/** Path of the dashboard customer list. */
export const CUSTOMER_LIST_PATH = "/api/v1/dashboard/customers";

/** An active margin target plan the fake API lists. */
export const creatorPlan: PlanResponse = {
	id: "pln_01jbvagescfn78y0938nkrkay1",
	name: "Creator",
	mode: "margin_target",
	target_margin: "0.4000",
	allowance: null,
	customer_count: 2,
	hold_times: {},
	status: "active",
	created_at: "2026-09-01T10:00:00Z",
};

/** A page holding only creatorPlan. */
export const planPage: PageBodyPlanResponse = {
	items: [creatorPlan],
	next_cursor: null,
};

/**
 * End of the current period in the fixtures: 3 days and 1 hour from when the
 * test module loads, so it reads "in 3 days".
 */
export const periodEnd = new Date(
	Date.now() + PERIOD_DAYS_LEFT * MILLISECONDS_PER_DAY + MILLISECONDS_PER_HOUR,
).toISOString();

/**
 * An exclusive period end at UTC midnight on the first day of the month after
 * next, always more than 7 days away, so it shows as a date.
 */
export const distantPeriodEnd = new Date(
	Date.UTC(
		today.getUTCFullYear(),
		today.getUTCMonth() + DISTANT_PERIOD_MONTHS_AHEAD,
		1,
	),
).toISOString();

/**
 * The last day distantPeriodEnd includes, the day before it, as the
 * dashboard writes dates, such as "Nov 30, 2026".
 */
export const distantLastDay = new Intl.DateTimeFormat(LOCALE, {
	month: "short",
	day: "numeric",
	year: "numeric",
	timeZone: "UTC",
}).format(Date.parse(distantPeriodEnd) - MILLISECONDS_PER_DAY);

/** Start of the current period in the fixtures, 30 days before periodEnd. */
export const periodStart = new Date(
	Date.parse(periodEnd) - PERIOD_DAYS * MILLISECONDS_PER_DAY,
).toISOString();

/** A paying customer below the plan target. */
export const acmeMargin: CustomerMarginResponse = {
	id: "cust_01jbvagescfn78y0938nkrka01",
	external_id: "acme",
	display_name: "Acme Studio",
	plan_id: creatorPlan.id,
	plan_name: creatorPlan.name,
	target_margin: creatorPlan.target_margin,
	revenue: "120.000000000",
	cost: "84.000000000",
	margin: "0.3000",
	pace: "1.2500",
	period_start: periodStart,
	period_end: periodEnd,
};

/** A paying customer above the plan target. */
export const cedarMargin: CustomerMarginResponse = {
	...acmeMargin,
	id: "cust_01jbvagescfn78y0938nkrka02",
	external_id: "cedar",
	display_name: "Cedar Games",
	revenue: "300.000000000",
	cost: "90.000000000",
	margin: "0.7000",
	pace: "0.5000",
};

/**
 * A customer without revenue on no plan, whose cost against a zero allowance
 * makes the pace "inf".
 */
export const tallOakMargin: CustomerMarginResponse = {
	...acmeMargin,
	id: "cust_01jbvagescfn78y0938nkrka03",
	external_id: "tall-oak",
	display_name: null,
	plan_id: null,
	plan_name: null,
	target_margin: null,
	revenue: "0.000000000",
	cost: "1.500000000",
	margin: null,
	pace: "inf",
};

/** The detail of acmeMargin with one period, usage and a routed decision. */
export const acmeDetail: CustomerDetailResponse = {
	id: acmeMargin.id,
	external_id: acmeMargin.external_id,
	display_name: acmeMargin.display_name,
	status: "active",
	plan_id: creatorPlan.id,
	plan_name: creatorPlan.name,
	target_margin: creatorPlan.target_margin,
	period_start: periodStart,
	period_end: periodEnd,
	signals: {
		allowance_remaining: "-12.000000000",
		cost_allowance: "72.000000000",
		cost_to_date: "84.000000000",
		elapsed_fraction: "0.9000",
		features: {
			text_to_video: {
				cost_to_date: "84.000000000",
				period_decision_count: 14,
				reserved: "0.000000000",
			},
		},
		pace: "1.2963",
		period_decision_count: 14,
		period_revenue_net: "120.000000000",
		projected_margin: "0.2222",
		request_estimated_cost: null,
		reserved: "0.000000000",
	},
	history: [
		{
			period_start: periodStart,
			period_end: periodEnd,
			revenue: "120.000000000",
			cost: "84.000000000",
			margin: "0.3000",
			uncosted_count: 0,
			decision_counts: { allow: 10, route: 3, cap: 1, deny: 0 },
		},
	],
	usage: [
		{
			feature: "text_to_video",
			provider: "fal_ai",
			model: "fal-ai/kling-video/v2.5-turbo/pro",
			request_count: 12,
			uncosted_count: 1,
			cost: "84.000000000",
		},
	],
	recent_decisions: [
		{
			id: "dec_01jbvagescfn78y0938nkrka01",
			created_at: new Date(Date.now() - MILLISECONDS_PER_HOUR).toISOString(),
			feature: "text_to_video",
			outcome: "route",
			reason: "policy_matched",
			matched_policy_id: "pol_01jbvagescfn78y0938nkrka01",
			provider: "fal_ai",
			model: "fal-ai/kling-video/v2.5-turbo/pro",
			requested_provider: "fal_ai",
			requested_model: "fal-ai/veo3.1/fast",
			estimated_cost: "0.350000000",
			status: "settled",
		},
	],
};

/** Returns a customer list page of items followed by nextCursor. */
export function customerPage(
	items: CustomerMarginResponse[],
	nextCursor: string | null = null,
): PageBodyCustomerMarginResponse {
	return { items, next_cursor: nextCursor };
}

/**
 * Lists the query parameters of every customer list request, oldest first,
 * each as an object such as `{ sort: "margin", direction: "ascending" }`.
 */
export function customerListQueries(
	requests: readonly Request[],
): Record<string, string>[] {
	return requests
		.map((request) => new URL(request.url))
		.filter((url) => url.pathname === CUSTOMER_LIST_PATH)
		.map((url) => Object.fromEntries(url.searchParams));
}

/** A pricing model page naming the fixture decisions' fal video models. */
export const videoModelPage: PageBodyModelResponse = {
	items: [
		{
			provider: "fal_ai",
			model: "fal-ai/kling-video/v2.5-turbo/pro",
			display_name: "Kling 2.5 Turbo Pro",
			status: "active",
			key_prices: [],
		},
		{
			provider: "fal_ai",
			model: "fal-ai/veo3.1/fast",
			display_name: "Veo 3.1 Fast",
			status: "active",
			key_prices: [],
		},
	],
	next_cursor: null,
};
