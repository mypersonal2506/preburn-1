import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getRouteApi, Link } from "@tanstack/react-router";
import { FileXIcon } from "lucide-react";
import { type ReactElement, useState } from "react";
import { toast } from "sonner";
import {
	createPolicyMutation,
	getPolicyOptions,
	getSettingsOptions,
} from "@/client/@tanstack/react-query.gen";
import { zCreatePolicyBody } from "@/client/zod.gen";
import { EmptyState } from "@/components/empty-state";
import { FilterChip } from "@/components/filter-chip";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { storeSavedPolicy } from "@/features/policies/policy-cache";
import {
	createPolicyBody,
	draftFromPolicy,
	type PolicyDraft,
} from "@/features/policies/policy-draft";
import { PolicyEditor } from "@/features/policies/policy-editor";
import {
	POLICY_STARTERS,
	type PolicyStarter,
	starterDraft,
} from "@/features/policies/policy-starters";
import { useCustomerChoices } from "@/features/policies/use-policy-names";
import { toApiProblem } from "@/lib/api-problem";
import { getEnvironment, useEnvironment } from "@/lib/environment-store";
import { parseRequestBody } from "@/lib/request-body";

interface ChosenStarter {
	starter: PolicyStarter;
	choiceCount: number;
}

interface DuplicateEditorProps {
	sourceId: string;
	saving: boolean;
	saveError: unknown;
	onSave: (draft: PolicyDraft, name: string) => void;
}

const NOT_FOUND_CODE = "not_found";
const INITIAL_STARTER: ChosenStarter = { starter: "blank", choiceCount: 0 };

const newPolicyRoute = getRouteApi("/_app/policies/new");

export function PolicyNewPage() {
	const { duplicate } = newPolicyRoute.useSearch();
	const navigate = newPolicyRoute.useNavigate();
	const queryClient = useQueryClient();
	const environment = useEnvironment();
	const [chosenStarter, setChosenStarter] = useState(INITIAL_STARTER);
	const settingsQuery = useQuery(getSettingsOptions());
	const createPolicy = useMutation({
		...createPolicyMutation(),
		onMutate: getEnvironment,
		onSuccess: async (policy, _variables, createdEnvironment) => {
			await storeSavedPolicy(queryClient, policy, createdEnvironment);
			toast.success("Policy created");
			if (createdEnvironment === getEnvironment()) {
				await navigate({
					to: "/policies/$policyId",
					params: { policyId: policy.id },
				});
			}
		},
	});

	function save(draft: PolicyDraft, name: string): void {
		const body = createPolicyBody(draft, name);
		createPolicy.mutate({ body: parseRequestBody(zCreatePolicyBody, body) });
	}

	const start = starterDraft(
		chosenStarter.starter,
		settingsQuery.data?.default_plan_id ?? null,
	);

	return (
		<>
			<PageHeader title="New policy" />
			<fieldset aria-label="Starters" className="flex flex-wrap gap-2">
				{POLICY_STARTERS.map((chip) => (
					<FilterChip
						key={chip.starter}
						label={chip.label}
						pressed={
							chosenStarter.choiceCount > 0 &&
							chosenStarter.starter === chip.starter
						}
						onClick={() =>
							setChosenStarter({
								starter: chip.starter,
								choiceCount: chosenStarter.choiceCount + 1,
							})
						}
					/>
				))}
			</fieldset>
			{duplicate !== undefined && chosenStarter.choiceCount === 0 ? (
				<DuplicateEditor
					sourceId={duplicate}
					saving={createPolicy.isPending}
					saveError={createPolicy.error}
					onSave={save}
				/>
			) : (
				// Without the environment in the key a draft keeps the other environment's plan after a switch.
				<PolicyEditor
					key={`${environment}-${chosenStarter.choiceCount}`}
					initialDraft={start.draft}
					initialOpenedBlank={start.openedBlank}
					policyId={null}
					saving={createPolicy.isPending}
					saveError={createPolicy.error}
					onSave={save}
				/>
			)}
		</>
	);
}

function DuplicateEditor({
	sourceId,
	saving,
	saveError,
	onSave,
}: DuplicateEditorProps): ReactElement {
	const sourceQuery = useQuery({
		...getPolicyOptions({ path: { policy_id: sourceId } }),
		throwOnError: (error) => toApiProblem(error).code !== NOT_FOUND_CODE,
	});
	const source = sourceQuery.data;
	const customers = useCustomerChoices(
		source === undefined || source.customer_id === null
			? []
			: [source.customer_id],
	);

	if (sourceQuery.isError) {
		return (
			<EmptyState
				icon={FileXIcon}
				title="Policy not found"
				description="It may belong to the other environment."
				action={
					<Button asChild variant="outline">
						<Link to="/policies/new">Start a new policy</Link>
					</Button>
				}
			/>
		);
	}
	if (source === undefined || customers === null) {
		return <Skeleton aria-busy="true" className="h-48" />;
	}
	const customer =
		source.customer_id === null
			? null
			: (customers.get(source.customer_id) ?? null);
	return (
		<PolicyEditor
			initialDraft={{
				...draftFromPolicy(source, customer),
				name: `Copy of ${source.name}`,
				status: "active",
			}}
			initialOpenedBlank={null}
			policyId={null}
			saving={saving}
			saveError={saveError}
			onSave={onSave}
		/>
	);
}
