import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { featureLabel } from "@/lib/labels";

interface FeatureLabelProps {
	feature: string;
}

/**
 * A feature key humanized for display, such as "text to video", with the
 * raw key in a tooltip.
 */
export function FeatureLabel({ feature }: FeatureLabelProps) {
	return (
		<Tooltip>
			<TooltipTrigger asChild>
				<span>{featureLabel(feature)}</span>
			</TooltipTrigger>
			<TooltipContent className="font-mono">{feature}</TooltipContent>
		</Tooltip>
	);
}
