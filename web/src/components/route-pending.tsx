import { Skeleton } from "@/components/ui/skeleton";

const PENDING_ROW_COUNT = 6;
const PENDING_ROWS = Array.from(
	{ length: PENDING_ROW_COUNT },
	(_row, position) => position,
);

/**
 * Pending state of a dashboard route, shaped like a list page: a title
 * skeleton above skeleton table rows. The router shows it while a route
 * loads, inside the app shell for every page of the shell.
 */
export function RoutePending() {
	return (
		<div aria-busy="true" className="flex flex-col gap-6">
			<Skeleton className="h-7 w-48" />
			<div className="flex flex-col gap-2">
				{PENDING_ROWS.map((row) => (
					<Skeleton key={row} className="h-9 w-full" />
				))}
			</div>
		</div>
	);
}
