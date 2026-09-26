-- name: ListRatingRules :many
SELECT pricing_rule_id, provider, model, meter, conditions, unit_price_nanos, unit_quantity, minimum_charge_nanos,
    billing_increment_micros, effective_from, effective_to, status, source
FROM pricing_rules
WHERE effective_to IS NULL OR effective_to > @since::timestamptz;

-- name: ListRatingOverrides :many
SELECT pricing_override_id, environment, provider, model, meter, conditions, unit_price_nanos, unit_quantity, native_unit,
    native_unit_price_nanos, minimum_charge_nanos, billing_increment_micros, effective_from, effective_to, import_id,
    created_at, updated_at
FROM pricing_overrides
WHERE environment = @environment
    AND (effective_to IS NULL OR effective_to > @since::timestamptz);

-- name: ListProviderModelAliases :many
SELECT provider, alias, model
FROM provider_model_aliases;
