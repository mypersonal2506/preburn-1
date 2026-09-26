import type { ReactNode } from "react";
import { HelpTip } from "@/components/help-tip";
import { Card, CardContent } from "@/components/ui/card";

interface MetricTileProps {
	label: string;
	helpTip?: string;
	value: string;
	delta?: ReactNode;
	track?: ReactNode;
	foot?: ReactNode;
}

/**
 * One headline number: its label with an optional HelpTip, the formatted
 * value in numeric type, an optional pill next to it (delta, such as a
 * MarginPill), an optional track under it (track, such as a MarginBar) and
 * a muted foot line.
 */
export function MetricTile({
	label,
	helpTip,
	value,
	delta,
	track,
	foot,
}: MetricTileProps) {
	return (
		<Card size="sm">
			<CardContent className="flex flex-col gap-2">
				<div className="flex items-center gap-1 text-sm text-muted-foreground">
					{label}
					{helpTip !== undefined && <HelpTip topic={label}>{helpTip}</HelpTip>}
				</div>
				<div className="flex items-center gap-2">
					<span className="numeric text-2xl font-medium">{value}</span>
					{delta}
				</div>
				{track}
				{foot !== undefined && (
					<div className="text-xs text-muted-foreground">{foot}</div>
				)}
			</CardContent>
		</Card>
	);
}
