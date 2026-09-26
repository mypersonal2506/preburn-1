import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { ApiProblem, isAuthenticationRequired } from "@/lib/api-problem";
import { subscribeEnvironment } from "@/lib/environment-store";

const QUERY_STALE_TIME_MILLISECONDS = 30_000;
const QUERY_RETRY_MAXIMUM = 3;
const SERVER_ERROR_STATUS_MINIMUM = 500;

/**
 * Creates the dashboard's QueryClient. Data stays fresh for 30 seconds.
 * Queries retry up to 3 times, and only after a network error or a 5xx
 * ApiProblem. Any query or mutation answered with `authentication_required`
 * calls onAuthenticationRequired, which sends the router to `/login`, and
 * drops every cached query once that navigation settles, so the next member
 * starts clean. An environment switch also drops every cached query,
 * because query keys do not hold the environment.
 */
export function createQueryClient(
	onAuthenticationRequired: () => Promise<unknown>,
): QueryClient {
	const handleError = (error: Error): void => {
		if (isAuthenticationRequired(error)) {
			void onAuthenticationRequired().then(() => queryClient.clear());
		}
	};
	const queryClient = new QueryClient({
		queryCache: new QueryCache({ onError: handleError }),
		mutationCache: new MutationCache({ onError: handleError }),
		defaultOptions: {
			queries: {
				staleTime: QUERY_STALE_TIME_MILLISECONDS,
				retry: shouldRetryQuery,
			},
		},
	});
	subscribeEnvironment(() => {
		void queryClient.resetQueries();
	});
	return queryClient;
}

function shouldRetryQuery(failureCount: number, error: Error): boolean {
	if (failureCount >= QUERY_RETRY_MAXIMUM) {
		return false;
	}
	return (
		error instanceof TypeError ||
		(error instanceof ApiProblem && error.status >= SERVER_ERROR_STATUS_MINIMUM)
	);
}
