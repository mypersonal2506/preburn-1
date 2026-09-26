package catalog

import "embed"

// Files holds the shipped catalog files at their paths relative to this
// directory, such as pricing/fal_ai.yaml and parameter_mappings.yaml.
//
//go:embed pricing/*.yaml model_aliases.yaml model_display_names.yaml model_default_attributes.yaml parameter_mappings.yaml
//go:embed litellm
var Files embed.FS
