import { createFileRoute } from "@tanstack/react-router";
import { InstallationSettingsPage } from "@/features/settings/installation-settings-page";

export const Route = createFileRoute("/_app/settings/installation")({
	staticData: { title: "Installation" },
	component: InstallationSettingsPage,
});
