import type {
	ApiKeyResponse,
	OnboardingResponse,
	PageBodyApiKeyResponse,
} from "@/client";
import type { DecisionEvent } from "@/lib/use-event-stream";

const MILLISECONDS_PER_HOUR = 3_600_000;
const MILLISECONDS_PER_DAY = 86_400_000;

/** Route of the onboarding status in the fake API. */
export const ONBOARDING_ROUTE = "GET /api/v1/dashboard/onboarding";

/** Route of the API key list in the fake API. */
export const API_KEY_LIST_ROUTE = "GET /api/v1/api-keys";

/** Route that creates an API key in the fake API. */
export const CREATE_API_KEY_ROUTE = "POST /api/v1/api-keys";

/** Onboarding of an environment with nothing set up. */
export const onboardingNothingDone: OnboardingResponse = {
	first_check_at: null,
	has_api_key: false,
	has_plan: false,
	has_policy: false,
	has_revenue: false,
};

/** Onboarding of an environment with an API key and nothing else. */
export const onboardingWithApiKey: OnboardingResponse = {
	...onboardingNothingDone,
	has_api_key: true,
};

/** Onboarding of an environment with every step done. */
export const onboardingAllDone: OnboardingResponse = {
	first_check_at: "2026-09-20T08:00:00Z",
	has_api_key: true,
	has_plan: true,
	has_policy: true,
	has_revenue: true,
};

/** An active runtime key that never authenticated a request. */
export const runtimeKey: ApiKeyResponse = {
	id: "key_01jbvagescfn78y0938nkrka01",
	name: "Production app",
	scope: "runtime",
	secret_last_four: "7Qx2",
	status: "active",
	last_used_at: null,
	created_at: new Date(Date.now() - 2 * MILLISECONDS_PER_DAY).toISOString(),
};

/** An active admin key that authenticated a request an hour ago. */
export const adminKey: ApiKeyResponse = {
	id: "key_01jbvagescfn78y0938nkrka02",
	name: "CI deploys",
	scope: "admin",
	secret_last_four: "Lm9k",
	status: "active",
	last_used_at: new Date(Date.now() - MILLISECONDS_PER_HOUR).toISOString(),
	created_at: new Date(Date.now() - 3 * MILLISECONDS_PER_DAY).toISOString(),
};

/** The first decision the decision stream sends. */
export const firstDecisionEvent: DecisionEvent = {
	id: "dec_01jbvagescfn78y0938nkrka01",
	created_at: "2026-09-26T10:15:30.123456789Z",
	customer_id: "cust_01jbvagescfn78y0938nkrka01",
	customer_external_id: "customer_42",
	customer_display_name: null,
	feature: "chat",
	requested_model: "gpt-6-luna",
	model: "gpt-6-luna",
	outcome: "allow",
	reason: "no_policy_matched",
	estimated_cost: "0.002400000",
	matched_policy_id: null,
};

/** Returns an API key list page of items followed by nextCursor. */
export function apiKeyPage(
	items: ApiKeyResponse[],
	nextCursor: string | null = null,
): PageBodyApiKeyResponse {
	return { items, next_cursor: nextCursor };
}
