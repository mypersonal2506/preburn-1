package catalogfiles

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	pricingDirectory      = "pricing"
	pricingExtension      = ".yaml"
	aliasesPath           = "model_aliases.yaml"
	displayNamesPath      = "model_display_names.yaml"
	defaultAttributesPath = "model_default_attributes.yaml"
	parameterMappingsPath = "parameter_mappings.yaml"
)

// ModelKey identifies a model by provider and model name.
type ModelKey struct {
	// Provider is the provider name, such as fal_ai or anthropic.
	Provider string
	// Model is the model name within the provider, such as fal-ai/veo3.1/fast.
	Model string
}

// Catalog is the validated content of the catalog files.
type Catalog struct {
	// CuratedModels holds the model entries of every pricing file, ordered by
	// file name and then by position in the file.
	CuratedModels []CuratedModel
	// Aliases holds the entries of model_aliases.yaml ordered by provider and
	// alias.
	Aliases []Alias
	// DisplayNames maps every curated model to the display name in its pricing
	// file, and every LiteLLM model in model_display_names.yaml to its name
	// there.
	DisplayNames map[ModelKey]string
	// DefaultAttributes maps every model in model_default_attributes.yaml to
	// its default attributes, empty for a model that has none.
	DefaultAttributes map[ModelKey]Attributes
	// ParameterMappings maps every model in parameter_mappings.yaml to its
	// overridable parameters keyed by parameter name, empty for a model that
	// has none.
	ParameterMappings map[ModelKey]map[string]Parameter
	// LiteLLMModels lists every model named under litellm_models in any file,
	// ordered by provider and model.
	LiteLLMModels []ModelKey
}

type catalogLoader struct {
	catalog       Catalog
	curatedModels map[ModelKey]bool
	liteLLMModels map[ModelKey]bool
}

type catalogFile struct {
	path string
	load func(data []byte) error
}

// String returns the key as provider/model, the form of a LiteLLM key.
func (key ModelKey) String() string {
	return key.Provider + "/" + key.Model
}

// Load reads and validates the catalog files in files: every
// pricing/<provider>.yaml, then model_display_names.yaml,
// model_default_attributes.yaml, parameter_mappings.yaml and
// model_aliases.yaml. A missing file is an error wrapping fs.ErrNotExist.
// Every other error starts with the path of the file that broke a rule and
// wraps one of the Err sentinels.
func Load(files fs.FS) (Catalog, error) {
	pricingPaths, err := fs.Glob(files, pricingDirectory+"/*"+pricingExtension)
	if err != nil {
		return Catalog{}, fmt.Errorf("list pricing files: %w", err)
	}
	loader := &catalogLoader{
		catalog: Catalog{
			DisplayNames:      map[ModelKey]string{},
			DefaultAttributes: map[ModelKey]Attributes{},
			ParameterMappings: map[ModelKey]map[string]Parameter{},
		},
		curatedModels: map[ModelKey]bool{},
		liteLLMModels: map[ModelKey]bool{},
	}
	catalogFiles := []catalogFile{}
	for _, pricingPath := range pricingPaths {
		provider := strings.TrimSuffix(strings.TrimPrefix(pricingPath, pricingDirectory+"/"), pricingExtension)
		catalogFiles = append(catalogFiles, catalogFile{path: pricingPath, load: func(data []byte) error {
			return loader.loadPricing(provider, data)
		}})
	}
	catalogFiles = append(catalogFiles,
		catalogFile{path: displayNamesPath, load: loader.loadDisplayNames},
		catalogFile{path: defaultAttributesPath, load: loader.loadDefaultAttributes},
		catalogFile{path: parameterMappingsPath, load: loader.loadParameterMappings},
		catalogFile{path: aliasesPath, load: loader.loadAliases},
	)
	for _, file := range catalogFiles {
		data, err := fs.ReadFile(files, file.path)
		if err != nil {
			return Catalog{}, fmt.Errorf("read catalog file: %w", err)
		}
		if err := file.load(data); err != nil {
			return Catalog{}, fmt.Errorf("%s: %w", file.path, err)
		}
	}
	loader.catalog.LiteLLMModels = slices.SortedFunc(maps.Keys(loader.liteLLMModels), compareModelKeys)
	return loader.catalog, nil
}

func (loader *catalogLoader) isKnownModel(model ModelKey) bool {
	return loader.curatedModels[model] || loader.liteLLMModels[model]
}

func compareModelKeys(first, second ModelKey) int {
	return cmp.Or(cmp.Compare(first.Provider, second.Provider), cmp.Compare(first.Model, second.Model))
}

func decodeStrict(data []byte, document any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	err := decoder.Decode(document)
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: empty document", ErrInvalidDocument)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	var next yaml.Node
	if err := decoder.Decode(&next); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: content after the first document", ErrInvalidDocument)
	}
	return nil
}
