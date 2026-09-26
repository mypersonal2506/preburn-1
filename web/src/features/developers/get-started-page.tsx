import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheckIcon, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";
import { getDashboardOnboardingOptions } from "@/client/@tanstack/react-query.gen";
import { CodePanel } from "@/components/code-panel";
import { EmptyState } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { CreateApiKeyModal } from "@/features/developers/create-api-key-modal";
import { FirstCheckStep } from "@/features/developers/first-check-step";
import {
	OnboardingChecklist,
	type OnboardingStep,
} from "@/features/developers/onboarding-checklist";
import { curlRevenueSnippet } from "@/features/developers/sdk-snippets";

const PAGE_TITLE = "Get started";
const FULL_PERCENT = 100;
const STEP_SKELETON_KEYS = [
	"api-key",
	"first-check",
	"plan",
	"policy",
	"revenue",
];

export function GetStartedPage() {
	const [createKeyOpen, setCreateKeyOpen] = useState(false);
	const onboardingQuery = useQuery({
		...getDashboardOnboardingOptions(),
		staleTime: 0,
	});

	if (onboardingQuery.isPending) {
		return (
			<>
				<PageHeader title={PAGE_TITLE} />
				<div aria-busy="true" className="flex flex-col gap-2">
					{STEP_SKELETON_KEYS.map((stepKey) => (
						<Skeleton key={stepKey} className="h-14 w-full" />
					))}
				</div>
			</>
		);
	}
	if (onboardingQuery.isError) {
		return (
			<>
				<PageHeader title={PAGE_TITLE} />
				<EmptyState
					icon={TriangleAlertIcon}
					title="Something went wrong"
					action={
						<Button variant="outline" onClick={() => onboardingQuery.refetch()}>
							Try again
						</Button>
					}
				/>
			</>
		);
	}

	const onboarding = onboardingQuery.data;
	const steps: OnboardingStep[] = [
		{
			title: "Create an API key",
			done: onboarding.has_api_key,
			content: (
				<div>
					<Button onClick={() => setCreateKeyOpen(true)}>Create API key</Button>
				</div>
			),
		},
		{
			title: "Send a first check",
			done: onboarding.first_check_at !== null,
			content: <FirstCheckStep />,
		},
		{
			title: "Create a plan",
			done: onboarding.has_plan,
			content: (
				<div>
					<Button asChild>
						<Link to="/plans/new">Create plan</Link>
					</Button>
				</div>
			),
		},
		{
			title: "Create a policy",
			done: onboarding.has_policy,
			content: (
				<div>
					<Button asChild>
						<Link to="/policies/new">Create policy</Link>
					</Button>
				</div>
			),
		},
		{
			title: "Record revenue",
			done: onboarding.has_revenue,
			content: (
				<CodePanel
					snippets={[curlRevenueSnippet(window.location.origin, new Date())]}
				/>
			),
		},
	];
	const doneCount = steps.filter((step) => step.done).length;
	const donePercent = (doneCount * FULL_PERCENT) / steps.length;

	return (
		<>
			<PageHeader
				title={PAGE_TITLE}
				meta={
					<div className="flex items-center gap-3">
						<span>{`${doneCount} of ${steps.length} done`}</span>
						<Progress
							aria-label="Setup progress"
							value={donePercent}
							className="w-24"
						/>
					</div>
				}
			/>
			{doneCount === steps.length ? (
				<EmptyState
					icon={CircleCheckIcon}
					title="You're set up"
					action={
						<div className="flex gap-2">
							<Button asChild>
								<Link to="/">Overview</Link>
							</Button>
							<Button asChild variant="outline">
								<Link to="/decisions">Decisions</Link>
							</Button>
						</div>
					}
				/>
			) : (
				<OnboardingChecklist steps={steps} />
			)}
			<CreateApiKeyModal open={createKeyOpen} onOpenChange={setCreateKeyOpen} />
		</>
	);
}
