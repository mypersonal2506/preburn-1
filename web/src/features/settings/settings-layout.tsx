import { Link, useLocation } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { PageHeader } from "@/components/page-header";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

interface SettingsLayoutProps {
	children: ReactNode;
}

const SETTINGS_TABS = [
	{ label: "Installation", to: "/settings/installation" },
	{ label: "Members", to: "/settings/members" },
] as const;

/**
 * The frame of every settings page: the "Settings" header with one tab per
 * settings page, each a link to its page, the current page's tab selected,
 * and children as the selected tab's panel.
 */
export function SettingsLayout({ children }: SettingsLayoutProps) {
	const pathname = useLocation({ select: (location) => location.pathname });

	return (
		<Tabs value={pathname} className="gap-6">
			<PageHeader
				title="Settings"
				tabs={
					<TabsList variant="line">
						{SETTINGS_TABS.map((tab) => (
							<TabsTrigger key={tab.to} value={tab.to} asChild>
								<Link to={tab.to}>{tab.label}</Link>
							</TabsTrigger>
						))}
					</TabsList>
				}
			/>
			<TabsContent value={pathname}>{children}</TabsContent>
		</Tabs>
	);
}
