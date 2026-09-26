import "@/styles/theme.css";
import { QueryClientProvider } from "@tanstack/react-query";
import { createRouter, RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouteError } from "@/components/route-error";
import { RouteNotFound } from "@/components/route-not-found";
import { RoutePending } from "@/components/route-pending";
import { createQueryClient } from "@/lib/query-client";
import { startTheme } from "@/lib/theme";
import { routeTree } from "@/routeTree.gen";

const queryClient = createQueryClient(() => router.navigate({ to: "/login" }));
const router = createRouter({
	routeTree,
	context: { queryClient },
	defaultPendingComponent: RoutePending,
	defaultErrorComponent: RouteError,
	defaultNotFoundComponent: RouteNotFound,
});

declare module "@tanstack/react-router" {
	interface Register {
		router: typeof router;
	}
}

startTheme();

const rootElement = document.getElementById("root");
if (rootElement === null) {
	throw new Error("root element missing id=root");
}

createRoot(rootElement).render(
	<StrictMode>
		<QueryClientProvider client={queryClient}>
			<RouterProvider router={router} />
		</QueryClientProvider>
	</StrictMode>,
);
