import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getRouteApi, Link } from "@tanstack/react-router";
import { FileXIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import type { PolicyResponse } from "@/client";
import {
	getPolicyOptions,
	updatePolicyMutation,
} from "@/client/@tanstack/react-query.gen";
import { zUpdatePolicyBody } from "@/client/zod.gen";
import { ConfirmModal } from "@/components/confirm-modal";
import { EmptyState } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { PageMenu } from "@/components/page-menu";
import { RelativeTime } from "@/components/relative-time";
import { StatusDot } from "@/components/status-dot";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { storeSavedPolicy } from "@/features/policies/policy-cache";
import { PolicyDecisionsCard } from "@/features/policies/policy-decisions-card";
import {
	createPolicyBody,
	draftFromPolicy,
	type PolicyDraft,
	updatePolicyBody,
} from "@/features/policies/policy-draft";
import { PolicyEditor } from "@/features/policies/policy-editor";
import { useCustomerChoices } from "@/features/policies/use-policy-names";
import { toApiProblem } from "@/lib/api-problem";
import { getEnvironment } from "@/lib/environment-store";
import { parseRequestBody } from "@/lib/request-body";

type PolicyStatus = PolicyResponse["status"];

const NOT_FOUND_CODE = "not_found";
const POLICIES_PATH = "/api/v1/policies";

const STATUS_TOASTS: Record<PolicyStatus, string> = {
	active: "Policy enabled",
	disabled: "Policy disabled",
	archived: "Policy archived",
};

const policyRoute = getRouteApi("/_app/policies/$policyId");

export function PolicyDetailPage() {
	const { policyId } = policyRoute.useParams();
	const queryClient = useQueryClient();
	const [archiveOpen, setArchiveOpen] = useState(false);
	const [savedCount, setSavedCount] = useState(0);
	const policyQuery = useQuery({
		...getPolicyOptions({ path: { policy_id: policyId } }),
		throwOnError: (error) => toApiProblem(error).code !== NOT_FOUND_CODE,
	});
	const policy = policyQuery.data;
	const customers = useCustomerChoices(
		policy === undefined || policy.customer_id === null
			? []
			: [policy.customer_id],
	);

	const savePolicy = useMutation({
		...updatePolicyMutation(),
		onMutate: getEnvironment,
		onSuccess: async (updatedPolicy, _variables, savedEnvironment) => {
			await storeSavedPolicy(queryClient, updatedPolicy, savedEnvironment);
			setSavedCount((count) => count + 1);
			toast.success("Policy saved");
		},
	});
	const changeStatus = useMutation({
		...updatePolicyMutation(),
		onMutate: getEnvironment,
		onSuccess: async (updatedPolicy, _variables, savedEnvironment) => {
			await storeSavedPolicy(queryClient, updatedPolicy, savedEnvironment);
			setArchiveOpen(false);
			toast.success(STATUS_TOASTS[updatedPolicy.status]);
		},
		onError: (_error, { body }) => {
			if (body.status !== "archived") {
				toast.error("Policy not updated");
			}
		},
	});

	if (policyQuery.isError) {
		return (
			<EmptyState
				icon={FileXIcon}
				title="Policy not found"
				description="It may belong to the other environment."
				action={
					<Button asChild variant="outline">
						<Link to="/policies">View policies</Link>
					</Button>
				}
			/>
		);
	}
	if (policy === undefined || customers === null) {
		return (
			<div aria-busy="true" className="flex flex-col gap-6">
				<Skeleton className="h-7 w-48" />
				<Skeleton className="h-48" />
				<Skeleton className="h-64" />
			</div>
		);
	}

	const draft = draftFromPolicy(
		policy,
		policy.customer_id === null
			? null
			: (customers.get(policy.customer_id) ?? null),
	);

	function save(editedDraft: PolicyDraft, name: string): void {
		const body = updatePolicyBody(editedDraft, name);
		savePolicy.mutate({
			path: { policy_id: policyId },
			body: parseRequestBody(zUpdatePolicyBody, body),
		});
	}

	function setStatus(status: PolicyStatus): void {
		changeStatus.mutate({
			path: { policy_id: policyId },
			body: parseRequestBody(zUpdatePolicyBody, { status }),
		});
	}

	return (
		<>
			<PageHeader
				title={policy.name}
				meta={
					<div className="flex flex-wrap items-center gap-x-3">
						<StatusDot status={policy.status} />
						<span>
							Version {policy.version}, updated{" "}
							<RelativeTime timestamp={policy.updated_at} now={new Date()} />
						</span>
					</div>
				}
				menu={
					<PageMenu
						record={policy}
						apiRequest={{
							method: "POST",
							path: POLICIES_PATH,
							body: createPolicyBody(draft, policy.name),
						}}
					>
						<DropdownMenuItem asChild>
							<Link to="/policies/new" search={{ duplicate: policy.id }}>
								Duplicate
							</Link>
						</DropdownMenuItem>
						{policy.status === "active" ? (
							<DropdownMenuItem onSelect={() => setStatus("disabled")}>
								Disable
							</DropdownMenuItem>
						) : (
							<DropdownMenuItem onSelect={() => setStatus("active")}>
								Enable
							</DropdownMenuItem>
						)}
						{policy.status !== "archived" && (
							<DropdownMenuItem
								variant="destructive"
								onSelect={() => setArchiveOpen(true)}
							>
								Archive
							</DropdownMenuItem>
						)}
					</PageMenu>
				}
			/>
			<PolicyEditor
				key={savedCount}
				initialDraft={draft}
				initialOpenedBlank={null}
				policyId={policy.id}
				saving={savePolicy.isPending}
				saveError={savePolicy.error}
				onSave={save}
			/>
			<PolicyDecisionsCard policyId={policy.id} />
			<ConfirmModal
				open={archiveOpen}
				onOpenChange={setArchiveOpen}
				title="Archive policy?"
				description="It stops applying to checks."
				confirmLabel="Archive"
				pending={changeStatus.isPending}
				error={changeStatus.error}
				onConfirm={() => setStatus("archived")}
			/>
		</>
	);
}
