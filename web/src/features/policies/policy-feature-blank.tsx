import type { ReactElement } from "react";
import { FeaturePicker } from "@/components/pickers/feature-picker";
import { Blank } from "@/components/sentence/blank";
import { Button } from "@/components/ui/button";
import { featureLabel } from "@/lib/labels";

interface PolicyFeatureBlankProps {
	feature: string | null;
	invalidMessage: string | undefined;
	onChange: (feature: string | null) => void;
}

/**
 * The feature blank of a policy sentence: a feature such as "text to video",
 * or "any feature". Its popover picks a feature, or goes back to any
 * feature.
 */
export function PolicyFeatureBlank({
	feature,
	invalidMessage,
	onChange,
}: PolicyFeatureBlankProps): ReactElement {
	return (
		<Blank
			placeholder="any feature"
			phrase={feature === null ? "any feature" : featureLabel(feature)}
			invalidMessage={invalidMessage}
		>
			<div className="flex flex-col gap-2">
				<FeaturePicker value={feature} onChange={onChange} />
				{feature !== null && (
					<Button
						type="button"
						variant="ghost"
						size="sm"
						onClick={() => onChange(null)}
					>
						Any feature
					</Button>
				)}
			</div>
		</Blank>
	);
}
