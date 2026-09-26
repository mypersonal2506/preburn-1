// Package catalog embeds the shipped catalog files, which package
// internal/catalogfiles decodes and validates.
//
// pricing/<provider>.yaml holds one provider's curated models. Each model
// entry has model, display_name, source_url (the https pricing page),
// verified_on (the day the prices were read) and rules. A rule has a meter,
// optional conditions, unit_price in USD per unit_quantity units, and an
// optional minimum_charge and billing_increment. Provider names follow
// LiteLLM's litellm_provider values, so a curated rule and a LiteLLM rule for
// the same model and meter meet in one rule set.
//
// A rule without conditions prices the request the provider bills by default,
// and rules with conditions price the other settings. A request that sends no
// attributes is then still rated at the provider's default price.
//
// model_aliases.yaml maps other names of a model to the catalog name, per
// provider. model_display_names.yaml names LiteLLM models, since curated
// models carry display_name in their pricing file.
// model_default_attributes.yaml holds each model's default attributes, used
// for key prices. parameter_mappings.yaml holds each model's overridable
// parameters. The last two split their entries into curated_models and
// litellm_models.
package catalog
