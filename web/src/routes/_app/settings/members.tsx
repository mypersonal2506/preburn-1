import { createFileRoute } from "@tanstack/react-router";
import { MembersPage } from "@/features/settings/members-page";

export const Route = createFileRoute("/_app/settings/members")({
	staticData: { title: "Members" },
	component: MembersPage,
});
