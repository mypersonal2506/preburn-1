import { AttentionBanner } from "@/components/attention-banner";
import { IdTag } from "@/components/id-tag";
import { MarginBar } from "@/components/margin-bar";
import { MarginPill } from "@/components/margin-pill";
import { MetricTile } from "@/components/metric-tile";
import { PageHeader } from "@/components/page-header";
import { PageMenu } from "@/components/page-menu";
import { SectionCard } from "@/components/section-card";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import {
	formatMargin,
	formatMoney,
	formatPace,
	formatPercent,
	formatPeriod,
} from "@/lib/format";

const SAMPLE_POLICY = {
	id: "pol_01jbvagescfn78y0938nkrkayd",
	name: "Heavy video users",
	status: "active",
	version: 3,
};
const SAMPLE_MARGIN = "0.3120";
const SAMPLE_TARGET_MARGIN = "0.4000";
const SAMPLE_PERIOD_START = "2026-09-01T00:00:00Z";
const SAMPLE_PERIOD_END = "2026-10-01T00:00:00Z";

export function GalleryHeaderSection() {
	return (
		<SectionCard title="Headers and metrics">
			<div className="flex flex-col gap-6">
				<PageHeader
					title={SAMPLE_POLICY.name}
					meta="Version 3, updated 2 hours ago"
					actions={
						<Button variant="outline" size="sm">
							Edit
						</Button>
					}
					menu={
						<PageMenu
							record={SAMPLE_POLICY}
							apiRequest={{
								method: "PATCH",
								path: `/api/v1/policies/${SAMPLE_POLICY.id}`,
								body: { status: "disabled" },
							}}
						>
							<DropdownMenuItem>Archive</DropdownMenuItem>
						</PageMenu>
					}
				/>
				<IdTag id={SAMPLE_POLICY.id} />
				<div className="grid gap-4 md:grid-cols-3">
					<MetricTile
						label="Revenue"
						value={formatMoney("1240.500000000")}
						foot={formatPeriod(SAMPLE_PERIOD_START, SAMPLE_PERIOD_END)}
					/>
					<MetricTile
						label="Margin"
						helpTip="Revenue minus cost, as a share of revenue."
						value={formatMargin(SAMPLE_MARGIN)}
						delta={
							<MarginPill
								margin={SAMPLE_MARGIN}
								targetMargin={SAMPLE_TARGET_MARGIN}
							/>
						}
						track={
							<MarginBar
								margin={SAMPLE_MARGIN}
								targetMargin={SAMPLE_TARGET_MARGIN}
							/>
						}
						foot={`Target ${formatPercent(SAMPLE_TARGET_MARGIN)}`}
					/>
					<MetricTile
						label="Pace"
						helpTip="Cost so far against the allowance for this date."
						value={formatPace("1.4000")}
						foot="Allowance runs out early"
					/>
				</div>
				<AttentionBanner
					title="3 customers lose money"
					description="Their cost is above their revenue this period."
					action={
						<Button variant="outline" size="sm">
							Review
						</Button>
					}
				/>
			</div>
		</SectionCard>
	);
}
