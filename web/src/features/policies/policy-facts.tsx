import { skipToken, useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { type ReactElement, useEffect } from "react";
import type { PolicyPreviewResult, PolicyResponse } from "@/client";
import { listPoliciesInfiniteOptions } from "@/client/@tanstack/react-query.gen";
import { previewPolicy } from "@/client/sdk.gen";
import {
	type SentenceFactQuery,
	SentenceFacts,
} from "@/components/sentence/sentence-facts";
import { useFactsDraft } from "@/components/sentence/use-facts-draft";
import {
	createPolicyBody,
	customerChoiceName,
	draftProblems,
	type PolicyDraft,
	suggestPolicyName,
} from "@/features/policies/policy-draft";
import { formatCount } from "@/lib/format";

interface PolicyFactsProps {
	draft: PolicyDraft;
	planName: string | null;
	policyId: string | null;
}

const ACTIVE_POLICY_PAGE_SIZE = 100;
const WAITING_FACT: SentenceFactQuery = {
	data: undefined,
	status: "pending",
	fetchStatus: "idle",
};
const LOADING_FACT: SentenceFactQuery = {
	data: undefined,
	status: "pending",
	fetchStatus: "fetching",
};
const FAILED_FACT: SentenceFactQuery = {
	data: undefined,
	status: "error",
	fetchStatus: "idle",
};

/**
 * The facts under a policy sentence, loaded 400 ms after the last edit:
 * how many customers the draft matches today, from the policy preview once
 * no blank is empty, and how many other active policies apply to the same
 * customers. policyId is the policy being edited, which the second fact
 * leaves out, or null on the new policy page.
 */
export function PolicyFacts({
	draft,
	planName,
	policyId,
}: PolicyFactsProps): ReactElement {
	const settledDraft = useFactsDraft(draft);
	const previewBody =
		draftProblems(settledDraft).length === 0
			? createPolicyBody(
					settledDraft,
					settledDraft.name ?? suggestPolicyName(settledDraft, planName),
				)
			: null;
	const previewQuery = useQuery({
		queryKey: ["previewPolicy", { body: previewBody }],
		queryFn:
			previewBody === null
				? skipToken
				: async ({ signal }) =>
						(
							await previewPolicy({
								body: previewBody,
								signal,
								throwOnError: true,
							})
						).data,
		select: matchesPhrase,
	});
	const otherPolicies = useOtherActivePolicies(
		settledDraft,
		planName,
		policyId,
	);

	return (
		<SentenceFacts
			facts={[
				{ label: "Matches today", query: previewQuery },
				{ label: "Other active policies", query: otherPolicies },
			]}
		/>
	);
}

function useOtherActivePolicies(
	draft: PolicyDraft,
	planName: string | null,
	policyId: string | null,
): SentenceFactQuery {
	const policiesQuery = useInfiniteQuery({
		...listPoliciesInfiniteOptions({
			query: { status: "active", limit: ACTIVE_POLICY_PAGE_SIZE },
		}),
		initialPageParam: {},
		getNextPageParam: (page) => page.next_cursor,
	});
	const {
		hasNextPage,
		isFetchingNextPage,
		isFetchNextPageError,
		fetchNextPage,
	} = policiesQuery;

	useEffect(() => {
		if (hasNextPage && !isFetchingNextPage && !isFetchNextPageError) {
			void fetchNextPage();
		}
	}, [hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage]);

	const audience = audiencePhrase(draft, planName);
	if (audience === null) {
		return WAITING_FACT;
	}
	if (policiesQuery.isError) {
		return FAILED_FACT;
	}
	if (policiesQuery.data === undefined || hasNextPage) {
		return LOADING_FACT;
	}
	const count = policiesQuery.data.pages
		.flatMap((page) => page.items)
		.filter(
			(policy) => policy.id !== policyId && sameAudience(policy, draft),
		).length;
	return {
		data: `${formatCount(count)} ${count === 1 ? "applies" : "apply"} to ${audience}`,
		status: "success",
		fetchStatus: policiesQuery.isFetching ? "fetching" : "idle",
	};
}

function matchesPhrase(preview: PolicyPreviewResult): string {
	const count = preview.matched_customer_count;
	const customers = `${formatCount(count)} ${count === 1 ? "customer" : "customers"}`;
	return preview.request_dependent
		? `${customers}, depends on each request`
		: customers;
}

function audiencePhrase(
	draft: PolicyDraft,
	planName: string | null,
): string | null {
	switch (draft.level) {
		case "everyone":
			return "all customers";
		case "plan":
			return draft.plan_id === null || planName === null
				? null
				: `${planName} customers`;
		case "customer":
			return draft.customer === null
				? null
				: customerChoiceName(draft.customer);
	}
}

function sameAudience(policy: PolicyResponse, draft: PolicyDraft): boolean {
	return (
		policy.level === draft.level &&
		policy.plan_id === draft.plan_id &&
		policy.customer_id === (draft.customer?.id ?? null)
	);
}
