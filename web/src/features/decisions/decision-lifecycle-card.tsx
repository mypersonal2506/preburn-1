import type { DecisionDetailResponse } from "@/client";
import { HelpTip } from "@/components/help-tip";
import { KeyValueList } from "@/components/key-value-list";
import { SectionCard } from "@/components/section-card";
import { StatusDot } from "@/components/status-dot";
import { formatDateTime, formatUnitCost } from "@/lib/format";

interface DecisionLifecycleCardProps {
	decision: DecisionDetailResponse;
}

const NOTHING_RESERVED_TEXT = "Nothing";

/**
 * The lifecycle of a decision in order: when the check decided, the amount
 * it reserved, then when a report settled it, that a release freed it
 * before its hold time ended, or when it expired. A reservation still
 * waiting for its report shows when it expires. The status shows in the
 * card header.
 */
export function DecisionLifecycleCard({
	decision,
}: DecisionLifecycleCardProps) {
	return (
		<SectionCard
			title="Lifecycle"
			action={<StatusDot status={decision.status} />}
		>
			<KeyValueList
				items={[
					{ label: "Checked", value: formatDateTime(decision.created_at) },
					{
						label: "Reserved",
						value:
							decision.status === "unreserved" ? (
								NOTHING_RESERVED_TEXT
							) : (
								<span className="inline-flex items-center gap-1">
									{formatUnitCost(decision.reserved_amount)}
									<HelpTip topic="reservation">
										Held against the allowance until the request reports.
									</HelpTip>
								</span>
							),
					},
					{
						label: "Settled",
						value:
							decision.settled_at !== null &&
							formatDateTime(decision.settled_at),
					},
					{
						label: "Released",
						value:
							decision.status === "released" &&
							`Before ${formatDateTime(decision.expires_at)}`,
					},
					{
						label: "Expired",
						value:
							decision.status === "expired" &&
							formatDateTime(decision.expires_at),
					},
					{
						label: "Expires",
						value:
							decision.status === "reserved" &&
							formatDateTime(decision.expires_at),
					},
				]}
			/>
		</SectionCard>
	);
}
