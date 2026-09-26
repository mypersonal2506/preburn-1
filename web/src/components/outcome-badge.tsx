import { Badge } from "@/components/ui/badge";
import { type DecisionOutcome, outcomeStyles } from "@/lib/outcomes";

interface OutcomeBadgeProps {
	outcome: DecisionOutcome;
	kind?: "decision" | "policy";
}

/**
 * An outcome on the badge variant that lib/outcomes assigns it, such as the
 * destructive badge for deny. A decision's outcome, the default kind, reads
 * in the past tense ("Denied"), and a policy's as the action it takes
 * ("Deny").
 */
export function OutcomeBadge({
	outcome,
	kind = "decision",
}: OutcomeBadgeProps) {
	const style = outcomeStyles[outcome];
	return (
		<Badge variant={style.badgeVariant}>
			{kind === "decision" ? style.pastTenseLabel : style.actionLabel}
		</Badge>
	);
}
