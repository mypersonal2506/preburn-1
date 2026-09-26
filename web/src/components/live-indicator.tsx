import { cn } from "cn";
import { PauseIcon, PlayIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { formatCount } from "@/lib/format";
import type { EventStream, EventStreamStatus } from "@/lib/use-event-stream";

interface LiveIndicatorProps {
	stream: EventStream;
}

type StreamStatusDisplay = {
	label: string;
	dotClass: string;
};

const STREAM_STATUS_DISPLAYS: Record<EventStreamStatus, StreamStatusDisplay> = {
	connecting: { label: "Connecting", dotClass: "bg-muted-foreground" },
	live: { label: "Live", dotClass: "bg-success" },
	reconnecting: { label: "Reconnecting", dotClass: "bg-warning" },
	paused: { label: "Paused", dotClass: "bg-muted-foreground" },
};

/**
 * The status of a useEventStream stream, "Connecting", "Live",
 * "Reconnecting" or "Paused" with the count of decisions that arrived since
 * the pause, next to a Pause or Resume button.
 */
export function LiveIndicator({ stream }: LiveIndicatorProps) {
	const display = STREAM_STATUS_DISPLAYS[stream.status];
	const paused = stream.status === "paused";
	return (
		<div className="flex items-center gap-2">
			<span role="status" className="inline-flex items-center gap-1.5 text-sm">
				<span
					aria-hidden
					className={cn("size-1.5 rounded-full", display.dotClass)}
				/>
				{paused && stream.newCount > 0
					? `${display.label}, ${formatCount(stream.newCount)} new`
					: display.label}
			</span>
			{paused ? (
				<Button variant="outline" size="sm" onClick={stream.resume}>
					<PlayIcon />
					Resume
				</Button>
			) : (
				<Button variant="outline" size="sm" onClick={stream.pause}>
					<PauseIcon />
					Pause
				</Button>
			)}
		</div>
	);
}
