import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import {
	getCurrentMemberOptions,
	getSetupStatusOptions,
} from "@/client/@tanstack/react-query.gen";
import { AppHeader } from "@/components/app-header";
import { AppSidebar } from "@/components/app-sidebar";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { isAuthenticationRequired } from "@/lib/api-problem";

export const Route = createFileRoute("/_app")({
	beforeLoad: async ({ context }) => {
		const setupStatus = await context.queryClient.fetchQuery({
			...getSetupStatusOptions(),
			staleTime: (query) =>
				query.state.data?.setup_required === false ? Infinity : 0,
		});
		if (setupStatus.setup_required) {
			throw redirect({ to: "/setup" });
		}
		const member = await context.queryClient
			.fetchQuery(getCurrentMemberOptions())
			.catch((error: unknown) => {
				if (isAuthenticationRequired(error)) {
					throw redirect({ to: "/login" });
				}
				throw error;
			});
		return { member };
	},
	component: AppLayout,
});

function AppLayout() {
	const { member } = Route.useRouteContext();

	return (
		<SidebarProvider>
			<AppSidebar member={member} />
			<SidebarInset>
				<AppHeader />
				<div className="flex flex-1 flex-col gap-6 p-6">
					<Outlet />
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
