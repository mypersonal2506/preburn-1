import { ScaleIcon } from "lucide-react";
import type { DecisionCountsResponse } from "@/client";
import { EmptyState } from "@/components/empty-state";
import { SectionCard } from "@/components/section-card";
import { formatShare } from "@/features/overview/format-share";
import { formatCount } from "@/lib/format";
import { decisionOutcomes, outcomeStyles } from "@/lib/outcomes";

interface OverviewDecisionMixProps {
	decisionCounts: DecisionCountsResponse;
}

/**
 * The Decision mix card: a share bar of the period's decisions by outcome
 * in the series colors of lib/outcomes, then each outcome's count and
 * share. A period without decisions shows an empty state.
 */
export function OverviewDecisionMix({
	decisionCounts,
}: OverviewDecisionMixProps) {
	const total = decisionOutcomes.reduce(
		(sum, outcome) => sum + decisionCounts[outcome],
		0,
	);
	if (total === 0) {
		return (
			<SectionCard title="Decision mix">
				<EmptyState icon={ScaleIcon} title="No decisions in this period" />
			</SectionCard>
		);
	}

	return (
		<SectionCard
			title="Decision mix"
			description={`${formatCount(total)} decisions`}
		>
			<div className="flex flex-col gap-4">
				<div aria-hidden className="flex h-2 overflow-hidden rounded-full">
					{decisionOutcomes.map((outcome) => (
						<div
							key={outcome}
							style={{
								flexGrow: decisionCounts[outcome],
								backgroundColor: outcomeStyles[outcome].seriesColor,
							}}
						/>
					))}
				</div>
				<ul className="flex flex-col gap-2 text-sm">
					{decisionOutcomes.map((outcome) => (
						<li key={outcome} className="flex items-center gap-2">
							<span
								aria-hidden
								className="size-2.5 shrink-0 rounded-[2px]"
								style={{ backgroundColor: outcomeStyles[outcome].seriesColor }}
							/>
							<span>{outcomeStyles[outcome].pastTenseLabel}</span>
							<span className="numeric ml-auto text-muted-foreground">
								{formatCount(decisionCounts[outcome])}
							</span>
							<span className="numeric w-14 text-right">
								{formatShare(decisionCounts[outcome], total)}
							</span>
						</li>
					))}
				</ul>
			</div>
		</SectionCard>
	);
}
