import { createFileRoute, stripSearchParams } from "@tanstack/react-router";
import { CustomerListPage } from "@/features/customers/customer-list-page";
import {
	customerListSearchDefaults,
	customerListSearchSchema,
} from "@/features/customers/customer-list-search";

export const Route = createFileRoute("/_app/customers/")({
	validateSearch: customerListSearchSchema,
	search: { middlewares: [stripSearchParams(customerListSearchDefaults)] },
	component: CustomerListPage,
});
