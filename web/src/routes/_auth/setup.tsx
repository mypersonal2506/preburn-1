import { createFileRoute, redirect } from "@tanstack/react-router";
import { getSetupStatusOptions } from "@/client/@tanstack/react-query.gen";
import { SetupPage } from "@/features/auth/setup-page";

export const Route = createFileRoute("/_auth/setup")({
	staticData: { title: "Set up" },
	beforeLoad: async ({ context }) => {
		const setupStatus = await context.queryClient.fetchQuery(
			getSetupStatusOptions(),
		);
		if (!setupStatus.setup_required) {
			throw redirect({ to: "/login" });
		}
	},
	component: SetupPage,
});
