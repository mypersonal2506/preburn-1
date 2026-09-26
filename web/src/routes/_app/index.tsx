import { createFileRoute } from "@tanstack/react-router";
import { zGetDashboardOverviewQuery } from "@/client/zod.gen";
import { OverviewPage } from "@/features/overview/overview-page";

export const Route = createFileRoute("/_app/")({
	staticData: { title: "Overview" },
	validateSearch: zGetDashboardOverviewQuery,
	component: OverviewPage,
});
