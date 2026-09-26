import { CustomerLabel } from "@/components/customer-label";
import { FeatureLabel } from "@/components/feature-label";
import { LiveIndicator } from "@/components/live-indicator";
import { MarginBar } from "@/components/margin-bar";
import { MarginPill } from "@/components/margin-pill";
import { MeterLabel } from "@/components/meter-label";
import { ModelLabel } from "@/components/model-label";
import { OutcomeBadge } from "@/components/outcome-badge";
import { SectionCard } from "@/components/section-card";
import { StatusDot } from "@/components/status-dot";
import { GalleryRow } from "@/features/gallery/gallery-row";
import { decisionOutcomes } from "@/lib/outcomes";
import type { EventStream, EventStreamStatus } from "@/lib/use-event-stream";

interface MarginSample {
	name: string;
	margin: string | null;
}

const RECORD_STATUSES = [
	"active",
	"disabled",
	"archived",
	"deprecated",
	"reserved",
	"settled",
	"released",
	"expired",
] as const;
const TARGET_MARGIN = "0.4000";
const MARGIN_SAMPLES: readonly MarginSample[] = [
	{ name: "loss", margin: "-0.1200" },
	{ name: "below target", margin: "0.2500" },
	{ name: "at target", margin: "0.4500" },
	{ name: "no revenue", margin: null },
];
const STREAM_STATUSES: readonly EventStreamStatus[] = [
	"connecting",
	"live",
	"reconnecting",
	"paused",
];
const PAUSED_NEW_COUNT = 12;

export function GalleryStatusSection() {
	return (
		<SectionCard title="Status and labels">
			<div className="flex flex-col gap-4">
				<GalleryRow label="Status">
					{RECORD_STATUSES.map((status) => (
						<StatusDot key={status} status={status} />
					))}
				</GalleryRow>
				<GalleryRow label="Outcomes">
					{decisionOutcomes.map((outcome) => (
						<OutcomeBadge key={outcome} outcome={outcome} />
					))}
				</GalleryRow>
				<GalleryRow label="Margins">
					{MARGIN_SAMPLES.map((sample) => (
						<MarginPill
							key={sample.name}
							margin={sample.margin}
							targetMargin={TARGET_MARGIN}
						/>
					))}
				</GalleryRow>
				<GalleryRow label="Margin bars">
					{MARGIN_SAMPLES.map((sample) => (
						<div key={sample.name} className="w-32">
							<MarginBar margin={sample.margin} targetMargin={TARGET_MARGIN} />
						</div>
					))}
				</GalleryRow>
				<GalleryRow label="Labels">
					<ModelLabel
						provider="fal_ai"
						model="fal-ai/veo3.1/fast"
						displayName="Veo 3.1 Fast"
					/>
					<FeatureLabel feature="text_to_video" />
					<MeterLabel meter="output_seconds" />
					<CustomerLabel displayName="Acme" externalId="acme" />
				</GalleryRow>
				<GalleryRow label="Live indicator">
					{STREAM_STATUSES.map((status) => (
						<LiveIndicator key={status} stream={sampleStream(status)} />
					))}
				</GalleryRow>
			</div>
		</SectionCard>
	);
}

function sampleStream(status: EventStreamStatus): EventStream {
	return {
		status,
		newCount: status === "paused" ? PAUSED_NEW_COUNT : 0,
		pause: () => undefined,
		resume: () => undefined,
	};
}
