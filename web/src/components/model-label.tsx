import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";

interface ModelLabelProps {
	provider: string;
	model: string;
	displayName: string | null;
}

/**
 * A model by its catalog display name, or by its model name when the
 * catalog has none, with "{provider}/{model}" in a tooltip.
 */
export function ModelLabel({ provider, model, displayName }: ModelLabelProps) {
	return (
		<Tooltip>
			<TooltipTrigger asChild>
				<span>{displayName ?? model}</span>
			</TooltipTrigger>
			<TooltipContent className="font-mono">
				{provider}/{model}
			</TooltipContent>
		</Tooltip>
	);
}
