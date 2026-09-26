import { useState } from "react";
import type { MeterDescription } from "@/client";
import { AmountInput } from "@/components/inputs/amount-input";
import { DurationInput } from "@/components/inputs/duration-input";
import { PaceInput } from "@/components/inputs/pace-input";
import { PercentInput } from "@/components/inputs/percent-input";
import { UnitPriceInput } from "@/components/inputs/unit-price-input";
import { MeterPicker } from "@/components/pickers/meter-picker";
import { SectionCard } from "@/components/section-card";
import { GalleryRow } from "@/features/gallery/gallery-row";
import type { UnitPrice } from "@/lib/decimal";

type Meter = MeterDescription["meter"];

const INPUT_WIDTH = "w-64";

export function GalleryInputSection() {
	const [amount, setAmount] = useState<string | null>("12.500000000");
	const [meter, setMeter] = useState<Meter>("cached_input_tokens");
	const [unitPrice, setUnitPrice] = useState<UnitPrice | null>({
		unit_price: "0.0375",
		unit_quantity: 1_000_000,
	});
	const [percent, setPercent] = useState<string | null>("0.4000");
	const [pace, setPace] = useState<string | null>("1.5000");
	const [seconds, setSeconds] = useState<number | null>(30);

	return (
		<SectionCard title="Inputs">
			<div className="flex flex-col gap-4">
				<GalleryRow label="Amount">
					<div className={INPUT_WIDTH}>
						<AmountInput
							aria-label="Amount"
							value={amount}
							onChange={setAmount}
						/>
					</div>
				</GalleryRow>
				<GalleryRow label="Unit price">
					<MeterPicker value={meter} onChange={setMeter} />
					<div className="w-80">
						<UnitPriceInput
							aria-label="Unit price"
							meter={meter}
							value={unitPrice}
							onChange={setUnitPrice}
						/>
					</div>
				</GalleryRow>
				<GalleryRow label="Percent">
					<div className={INPUT_WIDTH}>
						<PercentInput
							aria-label="Percent"
							value={percent}
							onChange={setPercent}
						/>
					</div>
				</GalleryRow>
				<GalleryRow label="Pace">
					<div className={INPUT_WIDTH}>
						<PaceInput aria-label="Pace" value={pace} onChange={setPace} />
					</div>
				</GalleryRow>
				<GalleryRow label="Duration">
					<DurationInput
						aria-label="Duration"
						value={seconds}
						onChange={setSeconds}
					/>
				</GalleryRow>
			</div>
		</SectionCard>
	);
}
