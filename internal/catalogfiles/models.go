package catalogfiles

import (
	"fmt"
	"slices"
)

type displayNamesDocument struct {
	LiteLLMModels map[string]map[string]string `yaml:"litellm_models"`
}

type defaultAttributesDocument struct {
	CuratedModels map[string]map[string]Attributes `yaml:"curated_models"`
	LiteLLMModels map[string]map[string]Attributes `yaml:"litellm_models"`
}

func (loader *catalogLoader) loadDisplayNames(data []byte) error {
	var document displayNamesDocument
	if err := decodeStrict(data, &document); err != nil {
		return err
	}
	return forEachModel(loader, nil, document.LiteLLMModels, func(model ModelKey, displayName string) error {
		if displayName == "" {
			return fmt.Errorf("%w: display name", ErrMissingField)
		}
		loader.catalog.DisplayNames[model] = displayName
		return nil
	})
}

func (loader *catalogLoader) loadDefaultAttributes(data []byte) error {
	var document defaultAttributesDocument
	if err := decodeStrict(data, &document); err != nil {
		return err
	}
	return forEachModel(loader, document.CuratedModels, document.LiteLLMModels, func(model ModelKey, attributes Attributes) error {
		validated, err := validateAttributes(attributes)
		if err != nil {
			return err
		}
		loader.catalog.DefaultAttributes[model] = validated
		return nil
	})
}

func forEachModel[Value any](loader *catalogLoader, curated, liteLLM map[string]map[string]Value, visit func(model ModelKey, value Value) error) error {
	for _, model := range sortedModelKeys(curated) {
		if !loader.curatedModels[model] {
			return fmt.Errorf("model %s: %w: curated_models names a model without a pricing file entry", model, ErrUnknownModel)
		}
		if err := visit(model, curated[model.Provider][model.Model]); err != nil {
			return fmt.Errorf("model %s: %w", model, err)
		}
	}
	for _, model := range sortedModelKeys(liteLLM) {
		if loader.curatedModels[model] {
			return fmt.Errorf("model %s: %w: a curated model belongs under curated_models", model, ErrModelConflict)
		}
		loader.liteLLMModels[model] = true
		if err := visit(model, liteLLM[model.Provider][model.Model]); err != nil {
			return fmt.Errorf("model %s: %w", model, err)
		}
	}
	return nil
}

func sortedModelKeys[Value any](section map[string]map[string]Value) []ModelKey {
	keys := []ModelKey{}
	for provider, models := range section {
		for model := range models {
			keys = append(keys, ModelKey{Provider: provider, Model: model})
		}
	}
	slices.SortFunc(keys, compareModelKeys)
	return keys
}
