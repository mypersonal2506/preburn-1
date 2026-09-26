import { Link, useLocation, useRouter } from "@tanstack/react-router";
import {
	CodeIcon,
	KeyRoundIcon,
	LayersIcon,
	LayoutDashboardIcon,
	RocketIcon,
	ScaleIcon,
	SettingsIcon,
	ShieldCheckIcon,
	UsersIcon,
} from "lucide-react";
import { useEffect } from "react";
import type { MemberResponse } from "@/client";
import { AppSidebarAccountMenu } from "@/components/app-sidebar-account-menu";
import { AppSidebarEnvironmentSwitch } from "@/components/app-sidebar-environment-switch";
import { AppSidebarInstallationName } from "@/components/app-sidebar-installation-name";
import {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarGroupLabel,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarRail,
	useSidebar,
} from "@/components/ui/sidebar";

/** Props of AppSidebar. */
export interface AppSidebarProps {
	/** The signed-in member, shown on the account button. */
	member: MemberResponse;
}

const NAVIGATION_GROUPS = [
	{
		label: "Monitor",
		items: [
			{ label: "Overview", to: "/", icon: LayoutDashboardIcon },
			{ label: "Customers", to: "/customers", icon: UsersIcon },
			{ label: "Decisions", to: "/decisions", icon: ScaleIcon },
		],
	},
	{
		label: "Control",
		items: [
			{ label: "Policies", to: "/policies", icon: ShieldCheckIcon },
			{ label: "Plans", to: "/plans", icon: LayersIcon },
		],
	},
	{
		label: "Developers",
		items: [
			{ label: "Get started", to: "/developers/get-started", icon: RocketIcon },
			{ label: "API keys", to: "/developers/api-keys", icon: KeyRoundIcon },
			{ label: "SDK", to: "/developers/sdk", icon: CodeIcon },
		],
	},
] as const;

const SETTINGS_SECTION = "/settings";

/**
 * The app shell sidebar, collapsible to icons. The top holds the wordmark,
 * the installation name and the environment switch. The body lists the
 * navigation groups of the shipped screens only, and the foot holds Settings
 * and the account menu. The item of the current section is marked active.
 * At phone width the sidebar is a sheet, which closes once a page opens.
 */
export function AppSidebar({ member }: AppSidebarProps) {
	const pathname = useLocation({ select: (location) => location.pathname });
	const router = useRouter();
	const { setOpenMobile } = useSidebar();

	useEffect(
		() => router.subscribe("onBeforeNavigate", () => setOpenMobile(false)),
		[router, setOpenMobile],
	);

	return (
		<Sidebar collapsible="icon">
			<SidebarHeader>
				<div className="flex flex-col gap-0.5 px-2 pt-1 group-data-[collapsible=icon]:hidden">
					<span className="text-sm font-semibold tracking-tight">Preburn</span>
					<AppSidebarInstallationName />
				</div>
				<AppSidebarEnvironmentSwitch />
			</SidebarHeader>
			<SidebarContent>
				{NAVIGATION_GROUPS.map((group) => (
					<SidebarGroup key={group.label}>
						<SidebarGroupLabel>{group.label}</SidebarGroupLabel>
						<SidebarMenu>
							{group.items.map((item) => (
								<SidebarMenuItem key={item.to}>
									<SidebarMenuButton
										asChild
										isActive={isInSection(pathname, item.to)}
										tooltip={item.label}
									>
										<Link to={item.to}>
											<item.icon />
											<span>{item.label}</span>
										</Link>
									</SidebarMenuButton>
								</SidebarMenuItem>
							))}
						</SidebarMenu>
					</SidebarGroup>
				))}
			</SidebarContent>
			<SidebarFooter>
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuButton
							asChild
							isActive={isInSection(pathname, SETTINGS_SECTION)}
							tooltip="Settings"
						>
							<Link to="/settings/installation">
								<SettingsIcon />
								<span>Settings</span>
							</Link>
						</SidebarMenuButton>
					</SidebarMenuItem>
					<SidebarMenuItem>
						<AppSidebarAccountMenu member={member} />
					</SidebarMenuItem>
				</SidebarMenu>
			</SidebarFooter>
			<SidebarRail />
		</Sidebar>
	);
}

function isInSection(pathname: string, section: string): boolean {
	if (section === "/") {
		return pathname === "/";
	}
	return pathname === section || pathname.startsWith(`${section}/`);
}
