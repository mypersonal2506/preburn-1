import { ToggleGroup } from "radix-ui";
import {
	type Environment,
	setEnvironment,
	useEnvironment,
} from "@/lib/environment-store";

interface EnvironmentOption {
	environment: Environment;
	label: string;
}

const ENVIRONMENT_OPTIONS: readonly EnvironmentOption[] = [
	{ environment: "test", label: "Test" },
	{ environment: "live", label: "Live" },
];

/**
 * The Test and Live segmented switch at the top of the sidebar. Switching
 * stores the environment, which every later API request sends as
 * `X-Preburn-Environment`. Hidden while the sidebar is collapsed to icons.
 */
export function AppSidebarEnvironmentSwitch() {
	const environment = useEnvironment();

	return (
		<ToggleGroup.Root
			type="single"
			value={environment}
			aria-label="Environment"
			className="grid grid-cols-2 gap-0.5 rounded-md bg-sidebar-accent p-0.5 group-data-[collapsible=icon]:hidden"
		>
			{ENVIRONMENT_OPTIONS.map((option) => (
				<ToggleGroup.Item
					key={option.environment}
					value={option.environment}
					onClick={() => setEnvironment(option.environment)}
					className="rounded-sm px-2 py-1 text-xs font-medium text-sidebar-foreground outline-hidden transition-colors hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring data-[state=on]:bg-background data-[state=on]:text-foreground"
				>
					{option.label}
				</ToggleGroup.Item>
			))}
		</ToggleGroup.Root>
	);
}
