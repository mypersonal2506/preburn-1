import { cn } from "cn";
import { Badge } from "@/components/ui/badge";
import { formatMargin, ratioToNumber } from "@/lib/format";

/**
 * Where a margin stands: below zero, at or above zero but below its target,
 * or at or above its target.
 */
export type MarginTone = "negative" | "below_target" | "at_target";

interface MarginPillProps {
	margin: string | null;
	targetMargin: string | null;
}

const MARGIN_PILL_TONE_CLASSES: Record<MarginTone, string> = {
	negative: "bg-destructive/10 text-destructive",
	below_target: "bg-warning/15 text-warning",
	at_target: "bg-success/15 text-success",
};

/**
 * The one margin tone mapping of the dashboard, for API ratios. A null
 * target, as for customers without a plan, leaves only the sign, and a
 * fixed allowance plan's target of zero does the same.
 */
export function marginTone(
	margin: string,
	targetMargin: string | null,
): MarginTone {
	const marginRatio = ratioToNumber(margin);
	if (marginRatio < 0) {
		return "negative";
	}
	if (targetMargin !== null && marginRatio < ratioToNumber(targetMargin)) {
		return "below_target";
	}
	return "at_target";
}

/**
 * A margin as a percent on a soft pill in its tone: destructive below zero,
 * warning below target, success at or above target. A null margin, which
 * the API sends without revenue above zero, and a projected margin of
 * "-inf", cost without revenue, read "No revenue" without a tone.
 */
export function MarginPill({ margin, targetMargin }: MarginPillProps) {
	if (margin === null || ratioToNumber(margin) === Number.NEGATIVE_INFINITY) {
		return (
			<Badge variant="outline" className="text-muted-foreground">
				{formatMargin(margin)}
			</Badge>
		);
	}
	const tone = marginTone(margin, targetMargin);
	return (
		<Badge
			data-tone={tone}
			className={cn("numeric", MARGIN_PILL_TONE_CLASSES[tone])}
		>
			{formatMargin(margin)}
		</Badge>
	);
}
