import { useQuery } from "@tanstack/react-query";
import { getRouteApi, Link } from "@tanstack/react-router";
import { UserXIcon } from "lucide-react";
import { getDashboardCustomerOptions } from "@/client/@tanstack/react-query.gen";
import { EmptyState } from "@/components/empty-state";
import { IdTag } from "@/components/id-tag";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { CustomerDecisionsCard } from "@/features/customers/customer-decisions-card";
import { CustomerHistoryCard } from "@/features/customers/customer-history-card";
import { CustomerMetricTiles } from "@/features/customers/customer-metric-tiles";
import { CustomerSignalsCard } from "@/features/customers/customer-signals-card";
import { CustomerUsageCard } from "@/features/customers/customer-usage-card";
import { toApiProblem } from "@/lib/api-problem";
import { formatPeriodEnd, formatRelativeTime } from "@/lib/format";

const NOT_FOUND_CODE = "not_found";
const RELATIVE_PERIOD_END_MILLISECONDS = 7 * 86_400_000;
const SKELETON_TILE_KEYS = ["revenue", "cost", "margin", "pace"];

const customerRoute = getRouteApi("/_app/customers/$customerId");

export function CustomerDetailPage() {
	const { customerId } = customerRoute.useParams();
	const customerQuery = useQuery({
		...getDashboardCustomerOptions({ path: { customer_id: customerId } }),
		throwOnError: (error) => toApiProblem(error).code !== NOT_FOUND_CODE,
	});

	if (customerQuery.isPending) {
		return (
			<div aria-busy="true" className="flex flex-col gap-6">
				<Skeleton className="h-7 w-48" />
				<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
					{SKELETON_TILE_KEYS.map((tileKey) => (
						<Skeleton key={tileKey} className="h-28" />
					))}
				</div>
				<Skeleton className="h-64" />
			</div>
		);
	}
	if (customerQuery.isError) {
		return (
			<EmptyState
				icon={UserXIcon}
				title="Customer not found"
				description="It may belong to the other environment."
				action={
					<Button asChild variant="outline">
						<Link to="/customers">View customers</Link>
					</Button>
				}
			/>
		);
	}

	const customer = customerQuery.data;
	return (
		<>
			<PageHeader
				title={customer.display_name ?? customer.external_id}
				meta={
					<div className="flex flex-wrap items-center gap-x-3">
						<span>
							{customer.plan_name ?? "No plan"},{" "}
							{periodEndPhrase(customer.period_end, new Date())}
						</span>
						<IdTag id={customer.external_id} />
					</div>
				}
				actions={
					<Button asChild variant="outline">
						<Link to="/decisions" search={{ customer_id: customer.id }}>
							View decisions
						</Link>
					</Button>
				}
			/>
			<CustomerMetricTiles customer={customer} />
			<div className="grid gap-6 lg:grid-cols-2">
				<CustomerSignalsCard signals={customer.signals} />
				{customer.history.length > 1 && (
					<CustomerHistoryCard history={customer.history} />
				)}
			</div>
			<CustomerUsageCard usage={customer.usage} />
			<CustomerDecisionsCard decisions={customer.recent_decisions} />
		</>
	);
}

function periodEndPhrase(periodEnd: string, now: Date): string {
	const remaining = Date.parse(periodEnd) - now.getTime();
	return remaining < RELATIVE_PERIOD_END_MILLISECONDS
		? `ends ${formatRelativeTime(periodEnd, now)}`
		: `ends ${formatPeriodEnd(periodEnd)}`;
}
