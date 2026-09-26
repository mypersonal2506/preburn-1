import { createFileRoute } from "@tanstack/react-router";
import { PlanListPage } from "@/features/plans/plan-list-page";

export const Route = createFileRoute("/_app/plans/")({
	component: PlanListPage,
});
