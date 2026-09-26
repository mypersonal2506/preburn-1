// Package catalogfiles decodes and validates the catalog files: the curated
// pricing files under pricing/, model_aliases.yaml, model_display_names.yaml,
// model_default_attributes.yaml and parameter_mappings.yaml.
//
// Decoding is strict. An unknown field, a duplicate key, a value of the wrong
// YAML type, an empty document or a second document is an error. Every meter
// and attribute is checked against the fixed vocabulary, so a typo fails at
// load time instead of leaving a rule that never matches. Errors start with
// the file path, name the entry, and wrap one of the Err sentinels.
//
// A curated model is one with an entry in a pricing file. The other files
// split their entries into curated_models, which must name curated models, and
// litellm_models, which declares models priced by the LiteLLM snapshot. The
// LiteLLM import checks Catalog.LiteLLMModels against the snapshot, because
// this package never reads it.
package catalogfiles
