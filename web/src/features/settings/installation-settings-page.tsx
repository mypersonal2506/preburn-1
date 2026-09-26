import { useQuery } from "@tanstack/react-query";
import { TriangleAlertIcon } from "lucide-react";
import type { ReactNode } from "react";
import { getSettingsOptions } from "@/client/@tanstack/react-query.gen";
import { EmptyState } from "@/components/empty-state";
import { SectionCard } from "@/components/section-card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { InstallationSettingsForm } from "@/features/settings/installation-settings-form";
import { SettingsLayout } from "@/features/settings/settings-layout";
import { useEnvironment } from "@/lib/environment-store";

export function InstallationSettingsPage() {
	const environment = useEnvironment();
	const settingsQuery = useQuery(getSettingsOptions());

	function cardContent(): ReactNode {
		if (settingsQuery.isError) {
			return (
				<EmptyState
					icon={TriangleAlertIcon}
					title="Something went wrong"
					action={
						<Button variant="outline" onClick={() => settingsQuery.refetch()}>
							Try again
						</Button>
					}
				/>
			);
		}
		if (settingsQuery.isPending) {
			return (
				<div className="flex flex-col gap-6">
					<Skeleton className="h-9 w-full" />
					<Skeleton className="h-9 w-full" />
				</div>
			);
		}
		// Without the key a touched form keeps the other environment's values after a switch.
		return (
			<InstallationSettingsForm
				key={environment}
				settings={settingsQuery.data}
			/>
		);
	}

	return (
		<SettingsLayout>
			<SectionCard title="Installation" className="max-w-xl">
				{cardContent()}
			</SectionCard>
		</SettingsLayout>
	);
}
