-- name: SelectOpenPricingRules :many
SELECT pricing_rule_id, source_key, unit_price_nanos, unit_quantity, minimum_charge_nanos, billing_increment_micros, effective_from, source_url
FROM pricing_rules
WHERE source = @source
    AND effective_to IS NULL;

-- name: UpsertPricingRule :exec
INSERT INTO pricing_rules (
    pricing_rule_id, provider, model, meter, conditions, unit_price_nanos, unit_quantity, minimum_charge_nanos,
    billing_increment_micros, effective_from, status, source, source_key, source_url, imported_at
)
VALUES (
    @pricing_rule_id, @provider, @model, @meter, @conditions, @unit_price_nanos, @unit_quantity, @minimum_charge_nanos,
    @billing_increment_micros, @effective_from, 'active', @source, @source_key, @source_url, @imported_at
)
ON CONFLICT (source, source_key, effective_from) DO UPDATE
SET unit_price_nanos = EXCLUDED.unit_price_nanos,
    unit_quantity = EXCLUDED.unit_quantity,
    minimum_charge_nanos = EXCLUDED.minimum_charge_nanos,
    billing_increment_micros = EXCLUDED.billing_increment_micros,
    source_url = EXCLUDED.source_url,
    imported_at = EXCLUDED.imported_at;

-- name: ClosePricingRule :exec
UPDATE pricing_rules
SET effective_to = @effective_to::timestamptz, status = @status, imported_at = @imported_at
WHERE pricing_rule_id = @pricing_rule_id;

-- name: SelectLatestImportedAt :one
SELECT imported_at
FROM pricing_rules
WHERE source = @source
ORDER BY imported_at DESC
LIMIT 1;

-- name: DeleteProviderModelAliases :exec
DELETE FROM provider_model_aliases;

-- name: InsertProviderModelAlias :exec
INSERT INTO provider_model_aliases (provider, alias, model)
VALUES (@provider, @alias, @model);
