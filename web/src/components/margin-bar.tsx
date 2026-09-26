import { type MarginTone, marginTone } from "@/components/margin-pill";
import { Progress } from "@/components/ui/progress";
import { ratioToNumber } from "@/lib/format";

interface MarginBarProps {
	margin: string | null;
	targetMargin: string | null;
}

const FULL_PERCENT = 100;

const MARGIN_BAR_TONE_CLASSES: Record<MarginTone, string> = {
	negative: "bg-destructive/15",
	below_target: "*:data-[slot=progress-indicator]:bg-warning",
	at_target: "*:data-[slot=progress-indicator]:bg-success",
};

/**
 * A track filled to the share of the target margin reached, in the margin
 * tone of MarginPill: full at or above target or without a target, empty
 * with a destructive tint below zero. A null margin renders nothing.
 */
export function MarginBar({ margin, targetMargin }: MarginBarProps) {
	if (margin === null) {
		return null;
	}
	const tone = marginTone(margin, targetMargin);
	const filled = filledPercent(margin, targetMargin);
	return (
		<Progress
			aria-label="Margin against target"
			data-tone={tone}
			value={filled}
			className={MARGIN_BAR_TONE_CLASSES[tone]}
		/>
	);
}

function filledPercent(margin: string, targetMargin: string | null): number {
	const marginRatio = ratioToNumber(margin);
	if (marginRatio < 0) {
		return 0;
	}
	if (targetMargin === null) {
		return FULL_PERCENT;
	}
	const targetRatio = ratioToNumber(targetMargin);
	if (marginRatio >= targetRatio) {
		return FULL_PERCENT;
	}
	return (marginRatio / targetRatio) * FULL_PERCENT;
}
