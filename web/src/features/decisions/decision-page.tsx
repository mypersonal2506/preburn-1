import { useQueries, useQuery } from "@tanstack/react-query";
import { getRouteApi, Link } from "@tanstack/react-router";
import { SearchXIcon } from "lucide-react";
import type { DecisionDetailResponse } from "@/client";
import {
	getDashboardDecisionOptions,
	getPolicyOptions,
} from "@/client/@tanstack/react-query.gen";
import { CustomerLabel } from "@/components/customer-label";
import { EmptyState } from "@/components/empty-state";
import { IdTag } from "@/components/id-tag";
import { PageHeader } from "@/components/page-header";
import { RelativeTime } from "@/components/relative-time";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useModelDisplayNames } from "@/components/use-model-display-names";
import { DecisionCostCard } from "@/features/decisions/decision-cost-card";
import { decisionTitle } from "@/features/decisions/decision-labels";
import { DecisionLedgerCard } from "@/features/decisions/decision-ledger-card";
import { DecisionLifecycleCard } from "@/features/decisions/decision-lifecycle-card";
import { DecisionRequestCard } from "@/features/decisions/decision-request-card";
import { DecisionSignalsCard } from "@/features/decisions/decision-signals-card";
import { decisionSummary } from "@/features/decisions/decision-summary";
import { toApiProblem } from "@/lib/api-problem";

interface DecisionDetailsProps {
	decision: DecisionDetailResponse;
}

const decisionRoute = getRouteApi("/_app/decisions/$decisionId");

const NOT_FOUND_CODE = "not_found";
const SKELETON_CARD_KEYS = ["lifecycle", "request", "cost", "signals"];

export function DecisionPage() {
	const { decisionId } = decisionRoute.useParams();
	const decisionQuery = useQuery({
		...getDashboardDecisionOptions({ path: { decision_id: decisionId } }),
		throwOnError: (error) => toApiProblem(error).code !== NOT_FOUND_CODE,
	});
	if (decisionQuery.isError) {
		return (
			<EmptyState
				icon={SearchXIcon}
				title="Decision not found"
				description="It may belong to the other environment."
				action={
					<Button asChild variant="outline">
						<Link to="/decisions">View decisions</Link>
					</Button>
				}
			/>
		);
	}
	if (decisionQuery.isPending) {
		return <DecisionPageSkeleton />;
	}
	return <DecisionDetails decision={decisionQuery.data} />;
}

function DecisionDetails({ decision }: DecisionDetailsProps) {
	const requestedModel = {
		provider: decision.requested_provider,
		model: decision.requested_model,
	};
	const modelDisplayName = useModelDisplayNames([
		requestedModel,
		decision,
		...decision.ledger_entries,
	]);
	const [policyQuery] = useQueries({
		queries: matchedPolicyIds(decision).map((policyId) => ({
			...getPolicyOptions({ path: { policy_id: policyId } }),
			throwOnError: true,
		})),
	});
	const policy = policyQuery?.data;
	const summary =
		decision.matched_policy_id !== null && policy === undefined
			? null
			: decisionSummary(decision, {
					policyName: policy?.name ?? null,
					requestedModelName:
						modelDisplayName(requestedModel) ?? decision.requested_model,
					servedModelName: modelDisplayName(decision) ?? decision.model,
				});

	return (
		<>
			<PageHeader
				title={decisionTitle(decision)}
				meta={
					<div className="flex flex-wrap items-center gap-x-3 gap-y-1">
						<Link
							to="/customers/$customerId"
							params={{ customerId: decision.customer_id }}
							className="text-foreground underline-offset-4 hover:underline"
						>
							<CustomerLabel
								displayName={decision.customer_display_name}
								externalId={decision.customer_external_id}
							/>
						</Link>
						<RelativeTime timestamp={decision.created_at} now={new Date()} />
						<IdTag id={decision.id} />
					</div>
				}
			/>
			{summary === null ? (
				<Skeleton className="h-6 w-full max-w-xl" />
			) : (
				<p className="text-base">{summary}</p>
			)}
			<div className="grid gap-6 lg:grid-cols-2">
				<DecisionLifecycleCard decision={decision} />
				<DecisionRequestCard
					decision={decision}
					policy={policy}
					modelDisplayName={modelDisplayName}
				/>
				<DecisionCostCard decision={decision} />
				<DecisionSignalsCard decision={decision} />
			</div>
			<DecisionLedgerCard
				entries={decision.ledger_entries}
				modelDisplayName={modelDisplayName}
			/>
		</>
	);
}

function DecisionPageSkeleton() {
	return (
		<div aria-busy="true" className="flex flex-col gap-6">
			<Skeleton className="h-7 w-64" />
			<Skeleton className="h-6 w-full max-w-xl" />
			<div className="grid gap-6 lg:grid-cols-2">
				{SKELETON_CARD_KEYS.map((cardKey) => (
					<Skeleton key={cardKey} className="h-48 w-full" />
				))}
			</div>
		</div>
	);
}

function matchedPolicyIds(decision: DecisionDetailResponse): string[] {
	return decision.matched_policy_id === null
		? []
		: [decision.matched_policy_id];
}
