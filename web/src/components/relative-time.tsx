import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { formatDateTime, formatRelativeTime } from "@/lib/format";

interface RelativeTimeProps {
	timestamp: string;
	now: Date;
}

/**
 * An event time relative to now within 7 days, such as "2 hours ago", with
 * the date and time in the viewer's time zone in a tooltip.
 */
export function RelativeTime({ timestamp, now }: RelativeTimeProps) {
	return (
		<Tooltip>
			<TooltipTrigger asChild>
				<span>{formatRelativeTime(timestamp, now)}</span>
			</TooltipTrigger>
			<TooltipContent>{formatDateTime(timestamp)}</TooltipContent>
		</Tooltip>
	);
}
