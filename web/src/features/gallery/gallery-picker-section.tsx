import { useState } from "react";
import type { MeterDescription } from "@/client";
import {
	AttributePicker,
	type AttributeSelection,
} from "@/components/pickers/attribute-picker";
import {
	type CustomerChoice,
	CustomerPicker,
} from "@/components/pickers/customer-picker";
import { FeaturePicker } from "@/components/pickers/feature-picker";
import { MeterPicker } from "@/components/pickers/meter-picker";
import {
	ModelPicker,
	type ModelReference,
} from "@/components/pickers/model-picker";
import { PlanPicker } from "@/components/pickers/plan-picker";
import { SectionCard } from "@/components/section-card";
import { GalleryRow } from "@/features/gallery/gallery-row";

type Meter = MeterDescription["meter"];

const SAMPLE_MODEL: ModelReference = {
	provider: "fal_ai",
	model: "fal-ai/veo3.1/fast",
};

export function GalleryPickerSection() {
	const [model, setModel] = useState<ModelReference>(SAMPLE_MODEL);
	const [attribute, setAttribute] = useState<AttributeSelection | null>(null);
	const [feature, setFeature] = useState<string | null>(null);
	const [meter, setMeter] = useState<Meter | null>(null);
	const [planId, setPlanId] = useState<string | null>(null);
	const [customer, setCustomer] = useState<CustomerChoice | null>(null);

	return (
		<SectionCard title="Pickers">
			<div className="flex flex-col gap-4">
				<GalleryRow label="Model">
					<ModelPicker
						value={model}
						onChange={(picked) => {
							setModel({ provider: picked.provider, model: picked.model });
							setAttribute(null);
						}}
					/>
				</GalleryRow>
				<GalleryRow label="Attribute">
					<AttributePicker
						provider={model.provider}
						model={model.model}
						value={attribute}
						onChange={setAttribute}
					/>
				</GalleryRow>
				<GalleryRow label="Feature">
					<FeaturePicker value={feature} onChange={setFeature} />
				</GalleryRow>
				<GalleryRow label="Meter">
					<MeterPicker value={meter} onChange={setMeter} />
				</GalleryRow>
				<GalleryRow label="Plan">
					<PlanPicker value={planId} onChange={(plan) => setPlanId(plan.id)} />
				</GalleryRow>
				<GalleryRow label="Customer">
					<CustomerPicker value={customer} onChange={setCustomer} />
				</GalleryRow>
			</div>
		</SectionCard>
	);
}
