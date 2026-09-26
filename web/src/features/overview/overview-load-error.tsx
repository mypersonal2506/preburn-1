import { TriangleAlertIcon, UsersIcon } from "lucide-react";
import { EmptyState } from "@/components/empty-state";
import { Button } from "@/components/ui/button";
import { toApiProblem } from "@/lib/api-problem";

interface OverviewLoadErrorProps {
	error: unknown;
	onRetry: () => void;
}

const ENVIRONMENT_TOO_LARGE_CODE = "environment_too_large";

/**
 * Error state of the overview inside the page: "Something went wrong" with
 * Try again, which calls onRetry. An environment with more active customers
 * than the overview evaluates says so instead, since trying again cannot
 * help.
 */
export function OverviewLoadError({ error, onRetry }: OverviewLoadErrorProps) {
	if (toApiProblem(error).code === ENVIRONMENT_TOO_LARGE_CODE) {
		return (
			<EmptyState
				icon={UsersIcon}
				title="Too many customers to summarize"
				description="The overview covers up to 50,000 active customers."
			/>
		);
	}
	return (
		<EmptyState
			icon={TriangleAlertIcon}
			title="Something went wrong"
			action={
				<Button variant="outline" onClick={onRetry}>
					Try again
				</Button>
			}
		/>
	);
}
