import type { ReactNode } from "react";

interface PageHeaderProps {
	title: string;
	meta?: ReactNode;
	actions?: ReactNode;
	menu?: ReactNode;
	tabs?: ReactNode;
}

/**
 * The top of every page: the title as the page heading, a meta line only
 * when it carries data ("Version 3, updated 2 hours ago"), actions and a
 * PageMenu on the right, and optional tabs underneath.
 */
export function PageHeader({
	title,
	meta,
	actions,
	menu,
	tabs,
}: PageHeaderProps) {
	return (
		<div className="flex flex-col gap-3">
			<div className="flex flex-wrap items-start justify-between gap-3">
				<div className="flex min-w-0 flex-col gap-1">
					<h1 className="truncate font-heading text-xl font-semibold">
						{title}
					</h1>
					{meta !== undefined && (
						<div className="text-sm text-muted-foreground">{meta}</div>
					)}
				</div>
				{(actions !== undefined || menu !== undefined) && (
					<div className="flex items-center gap-2">
						{actions}
						{menu}
					</div>
				)}
			</div>
			{tabs}
		</div>
	);
}
