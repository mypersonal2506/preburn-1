import { useQuery } from "@tanstack/react-query";
import { getSettingsOptions } from "@/client/@tanstack/react-query.gen";
import { Skeleton } from "@/components/ui/skeleton";

const DEFAULT_INSTALLATION_NAME = "Preburn";

/**
 * The installation name from the settings, under the wordmark at the top of
 * the sidebar. Shows a skeleton until the settings load, and nothing while
 * the installation keeps its default name, which the wordmark already
 * shows.
 */
export function AppSidebarInstallationName() {
	const settings = useQuery(getSettingsOptions());

	if (settings.data === undefined) {
		return <Skeleton className="h-4 w-24" />;
	}
	if (settings.data.installation_name === DEFAULT_INSTALLATION_NAME) {
		return null;
	}
	return (
		<span className="truncate text-xs text-muted-foreground">
			{settings.data.installation_name}
		</span>
	);
}
