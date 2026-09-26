import { useQueries } from "@tanstack/react-query";
import { listPricingModelsOptions } from "@/client/@tanstack/react-query.gen";
import type { ModelReference } from "@/components/pickers/model-picker";

/**
 * Looks up the catalog display names of models, which decisions and ledger
 * entries name by their raw provider and model. Each distinct model is
 * searched once in the pricing models of its provider, unpriced models
 * included. The returned function gives a model's display name, or null
 * while it loads and when the catalog has none, so ModelLabel falls back to
 * the model name.
 */
export function useModelDisplayNames(
	models: readonly ModelReference[],
): (model: ModelReference) => string | null {
	const distinctModels = [
		...new Map(models.map((model) => [modelKey(model), model])).values(),
	];
	const catalogPages = useQueries({
		queries: distinctModels.map((model) =>
			listPricingModelsOptions({
				query: {
					provider: model.provider,
					search: model.model,
					include_deprecated: true,
				},
			}),
		),
	});
	const displayNames = new Map(
		catalogPages.flatMap(
			(page) =>
				page.data?.items.map((item): [string, string | null] => [
					modelKey(item),
					item.display_name,
				]) ?? [],
		),
	);
	return (model) => displayNames.get(modelKey(model)) ?? null;
}

function modelKey(model: ModelReference): string {
	return `${model.provider}/${model.model}`;
}
