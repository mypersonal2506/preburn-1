import { cn } from "cn";
import { Progress } from "@/components/ui/progress";
import { ratioToNumber } from "@/lib/format";

interface AllowanceTrackProps {
	costAllowance: string;
	allowanceRemaining: string;
}

const FULL_PERCENT = 100;

/**
 * A track filled to the share of the cost allowance that settled and
 * reserved cost use, from API amounts, full with a destructive fill once the
 * allowance is spent. Render it only for an allowance above zero.
 */
export function AllowanceTrack({
	costAllowance,
	allowanceRemaining,
}: AllowanceTrackProps) {
	const allowance = ratioToNumber(costAllowance);
	const usedPercent = Math.min(
		FULL_PERCENT,
		((allowance - ratioToNumber(allowanceRemaining)) / allowance) *
			FULL_PERCENT,
	);
	return (
		<Progress
			aria-label="Allowance used"
			value={usedPercent}
			className={cn(
				usedPercent === FULL_PERCENT &&
					"*:data-[slot=progress-indicator]:bg-destructive",
			)}
		/>
	);
}
