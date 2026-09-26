import { createFileRoute } from "@tanstack/react-router";
import { LinkPage } from "@/features/auth/link-page";

export const Route = createFileRoute("/_auth/link")({
	staticData: { title: "Set password" },
	component: LinkPage,
});
