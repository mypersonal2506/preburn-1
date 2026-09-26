import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";

interface CustomerLabelProps {
	displayName: string | null;
	externalId: string;
}

/**
 * A customer by display name with the external id in a tooltip. A customer
 * without a display name shows its external id and no tooltip.
 */
export function CustomerLabel({ displayName, externalId }: CustomerLabelProps) {
	if (displayName === null) {
		return <span>{externalId}</span>;
	}
	return (
		<Tooltip>
			<TooltipTrigger asChild>
				<span>{displayName}</span>
			</TooltipTrigger>
			<TooltipContent className="font-mono">{externalId}</TooltipContent>
		</Tooltip>
	);
}
