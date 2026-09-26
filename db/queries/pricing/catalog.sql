-- name: ListMeters :many
SELECT meter, unit, description
FROM meters
ORDER BY meter COLLATE "C";

-- name: ListModels :many
WITH model_prices AS (
    SELECT provider, model, meter, effective_to IS NULL AS in_effect
    FROM pricing_rules
    UNION ALL
    SELECT provider, model, meter, effective_to IS NULL OR effective_to > sqlc.arg(at)::timestamptz AS in_effect
    FROM pricing_overrides
    WHERE environment = sqlc.arg(environment)
)
SELECT provider, model, coalesce(bool_or(in_effect), false)::boolean AS has_price_in_effect
FROM model_prices
WHERE (sqlc.narg(provider)::text IS NULL OR provider = sqlc.narg(provider)::text)
    AND (
        sqlc.narg(search)::text IS NULL
        OR strpos(lower(model), lower(sqlc.narg(search)::text)) > 0
        OR (provider, model) IN (
            SELECT unnest(sqlc.arg(display_name_providers)::text[]), unnest(sqlc.arg(display_name_models)::text[])
        )
    )
    AND (
        sqlc.narg(after_provider)::text IS NULL
        OR (provider COLLATE "C", model COLLATE "C") > (sqlc.narg(after_provider)::text COLLATE "C", sqlc.narg(after_model)::text COLLATE "C")
    )
GROUP BY provider, model
HAVING (sqlc.narg(meter)::text IS NULL OR bool_or(meter = sqlc.narg(meter)::text))
    AND (sqlc.arg(include_deprecated)::boolean OR bool_or(in_effect))
ORDER BY provider COLLATE "C", model COLLATE "C"
LIMIT sqlc.arg(row_limit)::bigint;

-- name: ListOpenRuleConditions :many
SELECT DISTINCT conditions
FROM pricing_rules
WHERE provider = @provider
    AND model = @model
    AND effective_to IS NULL
ORDER BY conditions;

-- name: ListOpenOverrideConditions :many
SELECT DISTINCT conditions
FROM pricing_overrides
WHERE environment = @environment
    AND provider = @provider
    AND model = @model
    AND (effective_to IS NULL OR effective_to > @at::timestamptz)
ORDER BY conditions;

-- name: CatalogRuleExists :one
SELECT EXISTS (
    SELECT 1
    FROM pricing_rules
    WHERE provider = @provider
        AND model = @model
        AND meter = @meter
        AND conditions = @conditions
        AND effective_to IS NULL
);

-- name: SelectAliasedModel :one
SELECT model
FROM provider_model_aliases
WHERE provider = @provider
    AND alias = @alias;
