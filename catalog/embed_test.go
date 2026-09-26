package catalog_test

import (
	"testing"

	"github.com/preburn/preburn/catalog"
	"github.com/preburn/preburn/internal/catalogfiles"
)

func TestShippedFilesLoad(t *testing.T) {
	if _, err := catalogfiles.Load(catalog.Files); err != nil {
		t.Fatalf("Load(catalog.Files) error = %v", err)
	}
}

func TestShippedFilesDescribeEveryModel(t *testing.T) {
	loaded, err := catalogfiles.Load(catalog.Files)
	if err != nil {
		t.Fatalf("Load(catalog.Files) error = %v", err)
	}
	models := loaded.LiteLLMModels
	for _, curated := range loaded.CuratedModels {
		models = append(models, catalogfiles.ModelKey{Provider: curated.Provider, Model: curated.Model})
	}
	for _, model := range models {
		if _, found := loaded.DisplayNames[model]; !found {
			t.Errorf("model %s/%s has no display name", model.Provider, model.Model)
		}
		if _, found := loaded.DefaultAttributes[model]; !found {
			t.Errorf("model %s/%s has no default attributes", model.Provider, model.Model)
		}
		if _, found := loaded.ParameterMappings[model]; !found {
			t.Errorf("model %s/%s has no parameter mappings", model.Provider, model.Model)
		}
	}
}
