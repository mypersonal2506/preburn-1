package catalogfiles

import (
	"fmt"
	"maps"
	"slices"
)

// Alias is another name for a model of the same provider. A request that
// names Alias is rated as Model.
type Alias struct {
	// Provider is the provider of both names.
	Provider string
	// Alias is the other name. It is never the name of a curated or listed
	// LiteLLM model.
	Alias string
	// Model is the curated or listed LiteLLM model the alias resolves to.
	Model string
}

type aliasesDocument struct {
	Aliases map[string]map[string]string `yaml:"aliases"`
}

func (loader *catalogLoader) loadAliases(data []byte) error {
	var document aliasesDocument
	if err := decodeStrict(data, &document); err != nil {
		return err
	}
	for _, provider := range slices.Sorted(maps.Keys(document.Aliases)) {
		models := document.Aliases[provider]
		for _, alias := range slices.Sorted(maps.Keys(models)) {
			aliasKey := ModelKey{Provider: provider, Model: alias}
			model := models[alias]
			switch {
			case alias == "" || model == "":
				return fmt.Errorf("alias %s: %w: alias and model", aliasKey, ErrMissingField)
			case loader.isKnownModel(aliasKey):
				return fmt.Errorf("alias %s: %w: the alias is a model name", aliasKey, ErrModelConflict)
			case !loader.isKnownModel(ModelKey{Provider: provider, Model: model}):
				return fmt.Errorf("alias %s: %w %q", aliasKey, ErrUnknownModel, model)
			}
			loader.catalog.Aliases = append(loader.catalog.Aliases, Alias{Provider: provider, Alias: alias, Model: model})
		}
	}
	return nil
}
