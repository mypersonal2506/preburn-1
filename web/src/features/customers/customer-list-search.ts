import type * as z from "zod";
import { zListDashboardCustomersQuery } from "@/client/zod.gen";

/**
 * The customer list's filters and sort as URL search params, named and
 * defaulted like the query of `GET /api/v1/dashboard/customers`: `search`,
 * `plan_id`, `revenue_filter`, `sort` and `direction`.
 */
export const customerListSearchSchema = zListDashboardCustomersQuery.pick({
	search: true,
	plan_id: true,
	revenue_filter: true,
	sort: true,
	direction: true,
});

/** The customer list's filters and sort as its route reads them. */
export type CustomerListSearch = z.output<typeof customerListSearchSchema>;

/** The search params of the unfiltered list, left out of its URL. */
export const customerListSearchDefaults = customerListSearchSchema.parse({});
