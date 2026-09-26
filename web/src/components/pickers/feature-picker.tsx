import { useQuery } from "@tanstack/react-query";
import type { ReactElement } from "react";
import type { KnownFeatureResponse } from "@/client";
import { listDashboardFeaturesOptions } from "@/client/@tanstack/react-query.gen";
import {
	type PickerFieldProps,
	type PickerOption,
	type PickerSearchOffer,
	PickerShell,
} from "@/components/pickers/picker-shell";
import { featureLabel } from "@/lib/labels";

/** Props of FeaturePicker. */
export interface FeaturePickerProps extends PickerFieldProps {
	value: string | null;
	onChange: (feature: string) => void;
}

const FEATURE_LENGTH_MAXIMUM = 64;
const FEATURE_CHARACTERS_PATTERN = /^[a-z][a-z0-9_]*$/;

/**
 * Picks a feature key from the features the environment knows: those of
 * recent decisions, plan hold times, policies and usage estimates. Options
 * show the humanized label, and the search also matches the key. Typed text
 * that names no known feature is offered as a new feature after the matches
 * when the server would accept it as a feature name: a lowercase letter
 * followed by up to 63 lowercase letters, digits or underscores. Otherwise
 * a hint says why it cannot be one.
 */
export function FeaturePicker({
	value,
	onChange,
	...fieldProps
}: FeaturePickerProps): ReactElement {
	const featuresQuery = useQuery(listDashboardFeaturesOptions());

	const knownFeatures =
		featuresQuery.data?.items.map(({ feature }) => feature) ?? [];

	return (
		<PickerShell
			{...fieldProps}
			placeholder="Select a feature"
			searchPlaceholder="Search features"
			emptyText="No features found"
			selectedValue={value}
			selectedLabel={value === null ? null : featureLabel(value)}
			groups={[{ options: featuresQuery.data?.items.map(featureOption) ?? [] }]}
			loading={featuresQuery.isPending}
			error={featuresQuery.error}
			onSelect={onChange}
			searchOffer={(search) => newFeatureOffer(search, knownFeatures)}
		/>
	);
}

function featureOption({
	feature,
}: KnownFeatureResponse): PickerOption<string> {
	return {
		value: feature,
		label: featureLabel(feature),
		detail: null,
		choice: feature,
	};
}

function newFeatureOffer(
	search: string,
	knownFeatures: readonly string[],
): PickerSearchOffer<string> | null {
	const feature = search.trim();
	if (feature === "" || knownFeatures.includes(feature)) {
		return null;
	}
	if (feature.length > FEATURE_LENGTH_MAXIMUM) {
		return {
			kind: "hint",
			hint: `Use at most ${FEATURE_LENGTH_MAXIMUM} characters`,
		};
	}
	if (!FEATURE_CHARACTERS_PATTERN.test(feature)) {
		return { kind: "hint", hint: "Use a-z, 0-9 and _, letter first" };
	}
	return {
		kind: "option",
		option: {
			value: feature,
			label: featureLabel(feature),
			detail: "New feature",
			choice: feature,
		},
	};
}
