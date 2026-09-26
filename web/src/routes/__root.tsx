import type { QueryClient } from "@tanstack/react-query";
import {
	type AnyRouteMatch,
	createRootRouteWithContext,
	HeadContent,
	Outlet,
} from "@tanstack/react-router";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useThemeChoice } from "@/lib/theme";

interface RouterContext {
	queryClient: QueryClient;
}

declare module "@tanstack/react-router" {
	interface StaticDataRouteOption {
		/**
		 * Page name for the document title `{title} - Preburn` and the header
		 * breadcrumb. The deepest matched route with a title names the page.
		 */
		title?: string;
	}
}

const PRODUCT_NAME = "Preburn";

export const Route = createRootRouteWithContext<RouterContext>()({
	head: ({ matches }) => ({ meta: [{ title: documentTitle(matches) }] }),
	component: RootLayout,
});

function RootLayout() {
	const themeChoice = useThemeChoice();

	return (
		<TooltipProvider>
			<HeadContent />
			<Outlet />
			<Toaster theme={themeChoice} />
		</TooltipProvider>
	);
}

function documentTitle(matches: readonly AnyRouteMatch[]): string {
	const pageTitle = matches
		.map((match) => match.staticData.title)
		.findLast((title) => title !== undefined);
	return pageTitle === undefined
		? PRODUCT_NAME
		: `${pageTitle} - ${PRODUCT_NAME}`;
}
