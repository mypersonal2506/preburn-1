import { createFileRoute } from "@tanstack/react-router";
import { PlanNewPage } from "@/features/plans/plan-new-page";

export const Route = createFileRoute("/_app/plans/new")({
	staticData: { title: "New plan" },
	component: PlanNewPage,
});
