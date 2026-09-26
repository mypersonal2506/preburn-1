import { CircleHelpIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";

interface HelpTipProps {
	topic: string;
	children: ReactNode;
}

/**
 * A help icon next to a label that explains a concept, such as pace or
 * projected margin, in a tooltip on hover and keyboard focus. Its accessible
 * name is "About {topic}".
 */
export function HelpTip({ topic, children }: HelpTipProps) {
	return (
		<Tooltip>
			<TooltipTrigger asChild>
				<Button
					variant="ghost"
					size="icon-xs"
					aria-label={`About ${topic}`}
					className="text-muted-foreground"
				>
					<CircleHelpIcon />
				</Button>
			</TooltipTrigger>
			<TooltipContent>{children}</TooltipContent>
		</Tooltip>
	);
}
