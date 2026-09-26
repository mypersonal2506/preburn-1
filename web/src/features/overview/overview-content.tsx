import { useQuery } from "@tanstack/react-query";
import { lazy, Suspense } from "react";
import { getDashboardOverviewOptions } from "@/client/@tanstack/react-query.gen";
import { SectionCard } from "@/components/section-card";
import { Skeleton } from "@/components/ui/skeleton";
import { OverviewAttentionBanner } from "@/features/overview/overview-attention-banner";
import { OverviewCustomersToWatch } from "@/features/overview/overview-customers-to-watch";
import { OverviewDecisionMix } from "@/features/overview/overview-decision-mix";
import { OverviewLoadError } from "@/features/overview/overview-load-error";
import { OverviewMetricTiles } from "@/features/overview/overview-metric-tiles";
import type { OverviewPeriod } from "@/features/overview/overview-period-filter";
import { OverviewPlanMargins } from "@/features/overview/overview-plan-margins";
import { OverviewPolicyChanges } from "@/features/overview/overview-policy-changes";
import { OverviewSkeleton } from "@/features/overview/overview-skeleton";

interface OverviewContentProps {
	period: OverviewPeriod;
}

const OverviewDailyChart = lazy(() =>
	import("@/features/overview/overview-daily-chart").then((module) => ({
		default: module.OverviewDailyChart,
	})),
);

/**
 * The overview of period once the environment has a first check: the
 * attention banner, the four tiles, the daily chart, plan margins,
 * customers to watch, the decision mix and recent policy changes, with a
 * skeleton while it loads and an error state with Try again.
 */
export function OverviewContent({ period }: OverviewContentProps) {
	const overviewQuery = useQuery(
		getDashboardOverviewOptions({ query: { period } }),
	);

	if (overviewQuery.isPending) {
		return <OverviewSkeleton />;
	}
	if (overviewQuery.isError) {
		return (
			<OverviewLoadError
				error={overviewQuery.error}
				onRetry={() => void overviewQuery.refetch()}
			/>
		);
	}
	const overview = overviewQuery.data;

	return (
		<div className="flex flex-col gap-6">
			<OverviewAttentionBanner attention={overview.attention} />
			<OverviewMetricTiles overview={overview} />
			<SectionCard title="Revenue and AI cost" description="Per UTC day">
				<Suspense fallback={<Skeleton className="h-64" />}>
					<OverviewDailyChart daily={overview.daily} />
				</Suspense>
			</SectionCard>
			<OverviewPlanMargins planMargins={overview.plan_margins} />
			<OverviewCustomersToWatch lossCustomers={overview.loss_customers} />
			<div className="grid gap-6 xl:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
				<OverviewDecisionMix decisionCounts={overview.decision_counts} />
				<OverviewPolicyChanges policyChanges={overview.policy_changes} />
			</div>
		</div>
	);
}
