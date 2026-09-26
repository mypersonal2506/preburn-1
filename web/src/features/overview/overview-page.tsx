import { useQuery } from "@tanstack/react-query";
import { getRouteApi, Link } from "@tanstack/react-router";
import { RocketIcon } from "lucide-react";
import { getDashboardOnboardingOptions } from "@/client/@tanstack/react-query.gen";
import { EmptyState } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { OverviewContent } from "@/features/overview/overview-content";
import { OverviewLoadError } from "@/features/overview/overview-load-error";
import { OverviewPeriodFilter } from "@/features/overview/overview-period-filter";
import { OverviewSkeleton } from "@/features/overview/overview-skeleton";

const PAGE_TITLE = "Overview";

const overviewRoute = getRouteApi("/_app/");

export function OverviewPage() {
	const { period } = overviewRoute.useSearch();
	const navigate = overviewRoute.useNavigate();
	const onboardingQuery = useQuery({
		...getDashboardOnboardingOptions(),
		staleTime: (query) =>
			query.state.data?.first_check_at === null ? 0 : Infinity,
	});

	if (onboardingQuery.isPending) {
		return (
			<>
				<PageHeader title={PAGE_TITLE} />
				<OverviewSkeleton />
			</>
		);
	}
	if (onboardingQuery.isError) {
		return (
			<>
				<PageHeader title={PAGE_TITLE} />
				<OverviewLoadError
					error={onboardingQuery.error}
					onRetry={() => void onboardingQuery.refetch()}
				/>
			</>
		);
	}
	if (onboardingQuery.data.first_check_at === null) {
		return (
			<>
				<PageHeader title={PAGE_TITLE} />
				<EmptyState
					icon={RocketIcon}
					title="No checks yet"
					description="Send a first check to fill the overview."
					action={
						<Button asChild>
							<Link to="/developers/get-started">Get started</Link>
						</Button>
					}
				/>
			</>
		);
	}

	return (
		<>
			<PageHeader
				title={PAGE_TITLE}
				actions={
					<OverviewPeriodFilter
						period={period}
						onPeriodChange={(nextPeriod) =>
							navigate({ search: { period: nextPeriod } })
						}
					/>
				}
			/>
			<OverviewContent period={period} />
		</>
	);
}
