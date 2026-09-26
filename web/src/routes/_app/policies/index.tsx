import { createFileRoute, stripSearchParams } from "@tanstack/react-router";
import { PolicyListPage } from "@/features/policies/policy-list-page";
import {
	policyListSearchDefaults,
	policyListSearchSchema,
} from "@/features/policies/policy-search";

export const Route = createFileRoute("/_app/policies/")({
	validateSearch: policyListSearchSchema,
	search: { middlewares: [stripSearchParams(policyListSearchDefaults)] },
	component: PolicyListPage,
});
