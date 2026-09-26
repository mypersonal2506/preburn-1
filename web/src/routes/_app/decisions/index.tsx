import { createFileRoute } from "@tanstack/react-router";
import { decisionFiltersSchema } from "@/features/decisions/decision-filters";
import { DecisionsPage } from "@/features/decisions/decisions-page";

export const Route = createFileRoute("/_app/decisions/")({
	validateSearch: decisionFiltersSchema,
	component: DecisionsPage,
});
