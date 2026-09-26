import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getRouteApi, Link } from "@tanstack/react-router";
import { SearchXIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import type { PlanResponse } from "@/client";
import {
	getPlanOptions,
	getSettingsOptions,
	updatePlanMutation,
} from "@/client/@tanstack/react-query.gen";
import { ConfirmModal } from "@/components/confirm-modal";
import { EmptyState } from "@/components/empty-state";
import { IdTag } from "@/components/id-tag";
import { PageHeader } from "@/components/page-header";
import { PageMenu } from "@/components/page-menu";
import { SentenceFacts } from "@/components/sentence/sentence-facts";
import { StatusDot } from "@/components/status-dot";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { DefaultPlanBadge } from "@/features/plans/default-plan-badge";
import { storeSavedPlan } from "@/features/plans/plan-cache";
import { draftFromPlan, updatePlanBody } from "@/features/plans/plan-draft";
import { PlanPoliciesCard } from "@/features/plans/plan-policies-card";
import { PlanSentence } from "@/features/plans/plan-sentence";
import { toApiProblem } from "@/lib/api-problem";
import { getEnvironment } from "@/lib/environment-store";
import { formatCount } from "@/lib/format";

interface PlanDetailProps {
	plan: PlanResponse;
}

const NOT_FOUND_CODE = "not_found";

const planDetailRoute = getRouteApi("/_app/plans/$planId");

export function PlanDetailPage() {
	const { planId } = planDetailRoute.useParams();
	const planQuery = useQuery({
		...getPlanOptions({ path: { plan_id: planId } }),
		throwOnError: (error) => toApiProblem(error).code !== NOT_FOUND_CODE,
	});

	if (planQuery.isPending) {
		return (
			<div aria-busy="true" className="flex flex-col gap-6">
				<Skeleton className="h-7 w-48" />
				<Skeleton className="h-48" />
			</div>
		);
	}
	if (planQuery.isError) {
		return (
			<EmptyState
				icon={SearchXIcon}
				title="Plan not found"
				description="It may belong to the other environment."
				action={
					<Button asChild variant="outline">
						<Link to="/plans">View plans</Link>
					</Button>
				}
			/>
		);
	}
	return <PlanDetail plan={planQuery.data} />;
}

function PlanDetail({ plan }: PlanDetailProps) {
	const queryClient = useQueryClient();
	const [archiveOpen, setArchiveOpen] = useState(false);
	const savedDraft = useMemo(() => draftFromPlan(plan), [plan]);
	const customerCountQuery = useQuery({
		...getPlanOptions({ path: { plan_id: plan.id } }),
		select: (savedPlan) => formatCount(savedPlan.customer_count),
	});
	const isDefaultQuery = useQuery({
		...getSettingsOptions(),
		select: (settings) => settings.default_plan_id === plan.id,
	});
	const updatePlan = useMutation({
		...updatePlanMutation(),
		onMutate: getEnvironment,
		onSuccess: (savedPlan, _variables, savedEnvironment) => {
			storeSavedPlan(queryClient, savedPlan, savedEnvironment);
			toast.success("Plan saved");
		},
	});
	const archivePlan = useMutation({
		...updatePlanMutation(),
		onMutate: getEnvironment,
		onSuccess: (savedPlan, _variables, savedEnvironment) => {
			storeSavedPlan(queryClient, savedPlan, savedEnvironment);
			setArchiveOpen(false);
			toast.success("Plan archived");
		},
	});
	const restorePlan = useMutation({
		...updatePlanMutation(),
		onMutate: getEnvironment,
		onSuccess: (savedPlan, _variables, savedEnvironment) => {
			storeSavedPlan(queryClient, savedPlan, savedEnvironment);
			toast.success("Plan restored");
		},
		onError: () => toast.error("Plan not restored"),
	});

	return (
		<>
			<PageHeader
				title={plan.name}
				meta={
					<div className="flex flex-wrap items-center gap-x-3">
						<StatusDot status={plan.status} />
						{isDefaultQuery.data && <DefaultPlanBadge />}
						<IdTag id={plan.id} />
					</div>
				}
				actions={
					<Button asChild variant="outline">
						<Link to="/customers" search={{ plan_id: plan.id }}>
							View customers
						</Link>
					</Button>
				}
				menu={
					<PageMenu record={plan}>
						{plan.status === "active" ? (
							<DropdownMenuItem onSelect={() => setArchiveOpen(true)}>
								Archive
							</DropdownMenuItem>
						) : (
							<DropdownMenuItem
								onSelect={() =>
									restorePlan.mutate({
										path: { plan_id: plan.id },
										body: { status: "active" },
									})
								}
							>
								Restore
							</DropdownMenuItem>
						)}
					</PageMenu>
				}
			/>
			<PlanSentence
				savedDraft={savedDraft}
				action="edit"
				facts={
					<SentenceFacts
						facts={[
							{ label: "Customers on this plan", query: customerCountQuery },
						]}
					/>
				}
				planSave={{
					pending: updatePlan.isPending,
					error: updatePlan.error,
					save: (draft, onSaved) =>
						updatePlan.mutate(
							{
								path: { plan_id: plan.id },
								body: updatePlanBody(plan, draft),
							},
							{ onSuccess: onSaved },
						),
				}}
			/>
			<PlanPoliciesCard planId={plan.id} />
			<ConfirmModal
				open={archiveOpen}
				onOpenChange={(open) => {
					setArchiveOpen(open);
					archivePlan.reset();
				}}
				title={`Archive ${plan.name}?`}
				description="Its customers keep it. New customers cannot join it."
				confirmLabel="Archive"
				pending={archivePlan.isPending}
				error={archivePlan.error}
				onConfirm={() =>
					archivePlan.mutate({
						path: { plan_id: plan.id },
						body: { status: "archived" },
					})
				}
			/>
		</>
	);
}
