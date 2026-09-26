import {
	keepPreviousData,
	useInfiniteQuery,
	useQuery,
} from "@tanstack/react-query";
import { type ReactElement, useState } from "react";
import type { KeyPriceResponse, ModelResponse } from "@/client";
import {
	listPricingModelsInfiniteOptions,
	listPricingModelsOptions,
} from "@/client/@tanstack/react-query.gen";
import { zKeyPriceResponse } from "@/client/zod.gen";
import {
	type PickerFieldProps,
	type PickerGroup,
	type PickerOption,
	PickerShell,
} from "@/components/pickers/picker-shell";
import { unitPriceLabel } from "@/lib/labels";

/** A provider model as policies and prices name it. */
export type ModelReference = Pick<ModelResponse, "provider" | "model">;

/** Props of ModelPicker. */
export interface ModelPickerProps extends PickerFieldProps {
	value: ModelReference | null;
	onChange: (model: ModelResponse) => void;
	cheapestFirst?: boolean;
}

const METER_ORDER = zKeyPriceResponse.shape.meter.options;

/**
 * Picks a model of the pricing catalog or the environment's overrides,
 * unpriced models included, searching by model or display name on the
 * server. Options are grouped by provider and show the display name, or the
 * model name when the model has none, with the headline price or Unpriced.
 * The headline price is the key price of the meter that comes first in the
 * API's meter order, such as input tokens for a language model. With
 * cheapestFirst the loaded models are sorted by headline price, lowest first
 * among models whose headline prices the same meter, meters in API order and
 * unpriced models last, and each provider group follows its cheapest model.
 * The trigger looks the selected model up to show its display name.
 */
export function ModelPicker({
	value,
	onChange,
	cheapestFirst = false,
	...fieldProps
}: ModelPickerProps): ReactElement {
	const [search, setSearch] = useState("");
	const modelsQuery = useInfiniteQuery({
		...listPricingModelsInfiniteOptions({
			query: { search, include_deprecated: true },
		}),
		initialPageParam: {},
		getNextPageParam: (page) => page.next_cursor,
		placeholderData: keepPreviousData,
	});
	const selectedQuery = useQuery({
		...listPricingModelsOptions({
			query: {
				provider: value?.provider,
				search: value?.model,
				include_deprecated: true,
			},
		}),
		enabled: value !== null,
	});

	const loadedModels =
		modelsQuery.data?.pages.flatMap((page) => page.items) ?? [];
	const models = cheapestFirst
		? loadedModels.toSorted(compareHeadlinePrices)
		: loadedModels;
	const selectedValue = value === null ? null : modelValue(value);
	const selectedModel = selectedQuery.data?.items.find(
		(model) => modelValue(model) === selectedValue,
	);

	return (
		<PickerShell
			{...fieldProps}
			placeholder="Select a model"
			searchPlaceholder="Search models"
			emptyText="No models found"
			selectedValue={selectedValue}
			selectedLabel={
				value === null ? null : (selectedModel?.display_name ?? value.model)
			}
			groups={providerGroups(models)}
			loading={modelsQuery.isPending}
			error={modelsQuery.error}
			onSelect={onChange}
			onSearchChange={setSearch}
			nextPage={
				modelsQuery.hasNextPage
					? {
							loading: modelsQuery.isFetchingNextPage,
							load: () => void modelsQuery.fetchNextPage(),
						}
					: undefined
			}
		/>
	);
}

function providerGroups(
	models: readonly ModelResponse[],
): PickerGroup<ModelResponse>[] {
	return Array.from(
		Map.groupBy(models, (model) => model.provider),
		([provider, providerModels]) => ({
			heading: provider,
			options: providerModels.map(modelOption),
		}),
	);
}

function modelOption(model: ModelResponse): PickerOption<ModelResponse> {
	return {
		value: modelValue(model),
		label: model.display_name ?? model.model,
		detail: modelDetail(model),
		choice: model,
	};
}

function modelValue(model: ModelReference): string {
	return `${model.provider}/${model.model}`;
}

function modelDetail(model: ModelResponse): string | null {
	if (model.status === "deprecated") {
		return "Unpriced";
	}
	const headline = headlinePrice(model);
	if (headline === null) {
		return null;
	}
	return unitPriceLabel(
		headline.unit_price,
		headline.unit_quantity,
		headline.meter,
	);
}

function headlinePrice(model: ModelResponse): KeyPriceResponse | null {
	if (model.status === "deprecated") {
		return null;
	}
	const [headline] = model.key_prices.toSorted(compareMeterOrder);
	return headline ?? null;
}

function compareHeadlinePrices(
	left: ModelResponse,
	right: ModelResponse,
): number {
	const leftHeadline = headlinePrice(left);
	const rightHeadline = headlinePrice(right);
	if (leftHeadline === null || rightHeadline === null) {
		return Number(leftHeadline === null) - Number(rightHeadline === null);
	}
	return (
		compareMeterOrder(leftHeadline, rightHeadline) ||
		pricePerUnit(leftHeadline) - pricePerUnit(rightHeadline)
	);
}

function pricePerUnit(keyPrice: KeyPriceResponse): number {
	return Number(keyPrice.unit_price) / keyPrice.unit_quantity;
}

function compareMeterOrder(
	left: KeyPriceResponse,
	right: KeyPriceResponse,
): number {
	return METER_ORDER.indexOf(left.meter) - METER_ORDER.indexOf(right.meter);
}
