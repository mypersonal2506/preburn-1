-- name: InsertPricingOverride :one
INSERT INTO pricing_overrides (
    pricing_override_id, environment, provider, model, meter, conditions, unit_price_nanos, unit_quantity, native_unit,
    native_unit_price_nanos, minimum_charge_nanos, billing_increment_micros, effective_from, effective_to, created_at, updated_at
)
VALUES (
    @pricing_override_id, @environment, @provider, @model, @meter, @conditions, @unit_price_nanos, @unit_quantity, @native_unit,
    @native_unit_price_nanos, @minimum_charge_nanos, @billing_increment_micros, @created_at, sqlc.narg(effective_to)::timestamptz,
    @created_at, @created_at
)
RETURNING pricing_override_id, environment, provider, model, meter, conditions, unit_price_nanos, unit_quantity, native_unit,
    native_unit_price_nanos, minimum_charge_nanos, billing_increment_micros, effective_from, effective_to, import_id,
    created_at, updated_at;

-- name: SelectPricingOverride :one
SELECT pricing_override_id, environment, provider, model, meter, conditions, unit_price_nanos, unit_quantity, native_unit,
    native_unit_price_nanos, minimum_charge_nanos, billing_increment_micros, effective_from, effective_to, import_id,
    created_at, updated_at
FROM pricing_overrides
WHERE environment = @environment
    AND pricing_override_id = @pricing_override_id;

-- name: SelectPricingOverrideForUpdate :one
SELECT pricing_override_id, environment, provider, model, meter, conditions, unit_price_nanos, unit_quantity, native_unit,
    native_unit_price_nanos, minimum_charge_nanos, billing_increment_micros, effective_from, effective_to, import_id,
    created_at, updated_at
FROM pricing_overrides
WHERE environment = @environment
    AND pricing_override_id = @pricing_override_id
FOR UPDATE;

-- name: UpdatePricingOverride :one
UPDATE pricing_overrides
SET unit_price_nanos = @unit_price_nanos,
    unit_quantity = @unit_quantity,
    native_unit = @native_unit,
    native_unit_price_nanos = @native_unit_price_nanos,
    minimum_charge_nanos = @minimum_charge_nanos,
    billing_increment_micros = @billing_increment_micros,
    effective_to = @effective_to,
    updated_at = @updated_at
WHERE pricing_override_id = @pricing_override_id
RETURNING pricing_override_id, environment, provider, model, meter, conditions, unit_price_nanos, unit_quantity, native_unit,
    native_unit_price_nanos, minimum_charge_nanos, billing_increment_micros, effective_from, effective_to, import_id,
    created_at, updated_at;

-- name: ListPricingOverrides :many
SELECT pricing_override_id, environment, provider, model, meter, conditions, unit_price_nanos, unit_quantity, native_unit,
    native_unit_price_nanos, minimum_charge_nanos, billing_increment_micros, effective_from, effective_to, import_id,
    created_at, updated_at
FROM pricing_overrides
WHERE environment = @environment
    AND (sqlc.arg(include_ended)::boolean OR effective_to IS NULL OR effective_to > sqlc.arg(at)::timestamptz)
    AND (
        sqlc.narg(after_created_at)::timestamptz IS NULL
        OR (created_at, pricing_override_id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_pricing_override_id)::uuid)
    )
ORDER BY created_at DESC, pricing_override_id DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: ListAdjustmentOverrideIDs :many
SELECT pricing_overrides.pricing_override_id
FROM pricing_overrides
WHERE pricing_overrides.pricing_override_id = ANY (sqlc.arg(pricing_override_ids)::uuid[])
    AND EXISTS (
        SELECT 1
        FROM pricing_rules
        WHERE pricing_rules.provider = pricing_overrides.provider
            AND pricing_rules.model = pricing_overrides.model
            AND pricing_rules.meter = pricing_overrides.meter
            AND pricing_rules.conditions = pricing_overrides.conditions
            AND pricing_rules.effective_to IS NULL
    );
