import type { MeterDescription } from "@/client";
import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { meterLabel } from "@/lib/labels";

interface MeterLabelProps {
	meter: MeterDescription["meter"];
}

/**
 * A meter named for display, such as "input tokens", with the raw key in a
 * tooltip.
 */
export function MeterLabel({ meter }: MeterLabelProps) {
	return (
		<Tooltip>
			<TooltipTrigger asChild>
				<span>{meterLabel(meter)}</span>
			</TooltipTrigger>
			<TooltipContent className="font-mono">{meter}</TooltipContent>
		</Tooltip>
	);
}
