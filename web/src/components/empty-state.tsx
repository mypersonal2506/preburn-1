import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

interface EmptyStateProps {
	icon: LucideIcon;
	title: string;
	description?: string;
	action?: ReactNode;
}

/**
 * Shows that a list or section has nothing yet: an icon, a title, at most
 * one short line and one primary action.
 */
export function EmptyState({
	icon: Icon,
	title,
	description,
	action,
}: EmptyStateProps) {
	return (
		<div className="flex flex-col items-center gap-2 py-10 text-center">
			<Icon aria-hidden className="size-5 text-muted-foreground" />
			<p className="font-medium">{title}</p>
			{description !== undefined && (
				<p className="text-sm text-muted-foreground">{description}</p>
			)}
			{action !== undefined && <div className="mt-2">{action}</div>}
		</div>
	);
}
