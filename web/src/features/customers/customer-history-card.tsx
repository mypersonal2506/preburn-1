import { lazy, Suspense } from "react";
import type { CustomerPeriodResponse } from "@/client";
import { SectionCard } from "@/components/section-card";
import { Skeleton } from "@/components/ui/skeleton";

interface CustomerHistoryCardProps {
	history: readonly CustomerPeriodResponse[];
}

const CustomerHistoryChart = lazy(() =>
	import("@/features/customers/customer-history-chart").then((module) => ({
		default: module.CustomerHistoryChart,
	})),
);

/**
 * The Period history card of a customer: revenue and AI cost per billing
 * period in a chart that loads on demand, with a skeleton until it does.
 */
export function CustomerHistoryCard({ history }: CustomerHistoryCardProps) {
	return (
		<SectionCard title="Period history" description="Per billing period">
			<Suspense fallback={<Skeleton className="h-56 w-full" />}>
				<CustomerHistoryChart history={history} />
			</Suspense>
		</SectionCard>
	);
}
