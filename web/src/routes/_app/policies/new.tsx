import { createFileRoute } from "@tanstack/react-router";
import { PolicyNewPage } from "@/features/policies/policy-new-page";
import { newPolicySearchSchema } from "@/features/policies/policy-search";

export const Route = createFileRoute("/_app/policies/new")({
	staticData: { title: "New policy" },
	validateSearch: newPolicySearchSchema,
	component: PolicyNewPage,
});
