import { createFileRoute } from "@tanstack/react-router";
import { GetStartedPage } from "@/features/developers/get-started-page";

export const Route = createFileRoute("/_app/developers/get-started")({
	staticData: { title: "Get started" },
	component: GetStartedPage,
});
