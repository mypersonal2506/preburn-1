import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { getDashboardOnboardingOptions } from "@/client/@tanstack/react-query.gen";
import { CodePanel } from "@/components/code-panel";
import {
	curlCheckSnippet,
	pythonFirstCheckSnippet,
} from "@/features/developers/sdk-snippets";
import { useEventStream } from "@/lib/use-event-stream";

/**
 * The content of the open "Send a first check" step: a curl and a Python
 * check against this dashboard's origin, and a wait on the decision stream.
 * The first streamed decision sets `first_check_at` in the cached onboarding
 * status, which completes the step without another request.
 */
export function FirstCheckStep() {
	const queryClient = useQueryClient();
	const stream = useEventStream((decision) => {
		queryClient.setQueryData(
			getDashboardOnboardingOptions().queryKey,
			(onboarding) =>
				onboarding === undefined
					? undefined
					: {
							...onboarding,
							first_check_at: onboarding.first_check_at ?? decision.created_at,
						},
		);
	});
	const origin = window.location.origin;

	return (
		<div className="flex flex-col gap-3">
			<CodePanel
				snippets={[curlCheckSnippet(origin), pythonFirstCheckSnippet(origin)]}
			/>
			<Link
				to="/developers/sdk"
				className="w-fit text-sm underline underline-offset-4"
			>
				Install the Python SDK
			</Link>
			<p
				role="status"
				className="inline-flex items-center gap-2 text-sm text-muted-foreground"
			>
				<span
					aria-hidden
					className="size-1.5 animate-pulse rounded-full bg-success"
				/>
				{stream.status === "reconnecting"
					? "Reconnecting"
					: "Waiting for the first check"}
			</p>
		</div>
	);
}
