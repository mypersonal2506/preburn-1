import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
	ChevronsUpDownIcon,
	CircleUserRoundIcon,
	LogOutIcon,
} from "lucide-react";
import { toast } from "sonner";
import type { MemberResponse } from "@/client";
import { logoutMutation } from "@/client/@tanstack/react-query.gen";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuRadioGroup,
	DropdownMenuRadioItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarMenuButton } from "@/components/ui/sidebar";
import { isAuthenticationRequired } from "@/lib/api-problem";
import { setThemeChoice, type ThemeChoice, useThemeChoice } from "@/lib/theme";

/** Props of AppSidebarAccountMenu. */
export interface AppSidebarAccountMenuProps {
	/** The signed-in member. */
	member: MemberResponse;
}

interface ThemeOption {
	choice: ThemeChoice;
	label: string;
}

const THEME_OPTIONS: readonly ThemeOption[] = [
	{ choice: "light", label: "Light" },
	{ choice: "dark", label: "Dark" },
	{ choice: "system", label: "System" },
];

/**
 * The account button at the foot of the sidebar, labelled with the member's
 * display name. Its menu holds the member's email, which opens the account
 * page, the theme choice, and Log out. Logging out opens the login page, then
 * drops every cached query so the next member starts clean. A session that
 * already ended counts as logged out: the query client's
 * `authentication_required` handler opens the login page and drops the cache.
 */
export function AppSidebarAccountMenu({ member }: AppSidebarAccountMenuProps) {
	const themeChoice = useThemeChoice();
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const logout = useMutation({
		...logoutMutation(),
		onSuccess: async () => {
			await navigate({ to: "/login" });
			queryClient.clear();
		},
		onError: (error) => {
			if (!isAuthenticationRequired(error)) {
				toast.error("Log out failed");
			}
		},
	});

	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<SidebarMenuButton size="lg">
					<CircleUserRoundIcon />
					<span className="truncate font-medium">{member.display_name}</span>
					<ChevronsUpDownIcon className="ml-auto" />
				</SidebarMenuButton>
			</DropdownMenuTrigger>
			<DropdownMenuContent
				side="top"
				align="start"
				className="w-(--radix-dropdown-menu-trigger-width) min-w-56"
			>
				<DropdownMenuItem asChild>
					<Link to="/account" className="truncate text-muted-foreground">
						{member.email}
					</Link>
				</DropdownMenuItem>
				<DropdownMenuSeparator />
				<DropdownMenuLabel>Theme</DropdownMenuLabel>
				<DropdownMenuRadioGroup value={themeChoice}>
					{THEME_OPTIONS.map((option) => (
						<DropdownMenuRadioItem
							key={option.choice}
							value={option.choice}
							onSelect={() => setThemeChoice(option.choice)}
						>
							{option.label}
						</DropdownMenuRadioItem>
					))}
				</DropdownMenuRadioGroup>
				<DropdownMenuSeparator />
				<DropdownMenuItem onSelect={() => logout.mutate({})}>
					<LogOutIcon />
					Log out
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
