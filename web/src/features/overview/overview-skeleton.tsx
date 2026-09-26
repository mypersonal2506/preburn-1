import { Skeleton } from "@/components/ui/skeleton";

const TILE_SKELETON_KEYS = ["revenue", "cost", "margin", "cost-avoided"];

/**
 * Loading state of the overview, shaped like its layout: four tile
 * skeletons above a chart skeleton.
 */
export function OverviewSkeleton() {
	return (
		<div aria-busy="true" className="flex flex-col gap-6">
			<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
				{TILE_SKELETON_KEYS.map((tileKey) => (
					<Skeleton key={tileKey} className="h-28" />
				))}
			</div>
			<Skeleton className="h-80" />
		</div>
	);
}
