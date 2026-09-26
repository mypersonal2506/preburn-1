-- name: SelectPricingOverrideDigest :one
SELECT md5(coalesce(string_agg(row_to_json(pricing_overrides)::text, ',' ORDER BY pricing_override_id), ''))::text AS digest
FROM pricing_overrides
WHERE environment = @environment;

-- name: ListUncostedUsage :many
WITH missing_meters AS (
    SELECT ledger_entries.provider,
        ledger_entries.model,
        cost_lines.line ->> 'meter' AS meter,
        ledger_entries.occurred_at
    FROM ledger_entries
    CROSS JOIN LATERAL jsonb_array_elements(ledger_entries.cost_breakdown -> 'lines') AS cost_lines (line)
    WHERE ledger_entries.environment = @environment
        AND ledger_entries.cost_status = 'uncosted'
        AND ledger_entries.occurred_at >= sqlc.arg(occurred_since)::timestamptz
        AND (cost_lines.line ->> 'missing')::boolean
        AND NOT EXISTS (
            SELECT 1
            FROM ledger_entries AS corrections
            WHERE corrections.environment = ledger_entries.environment
                AND corrections.idempotency_key = 'correction:' || ledger_entries.ledger_entry_id::text
        )
)
SELECT missing_meters.provider,
    missing_meters.model,
    missing_meters.meter::text AS meter,
    count(*) AS request_count,
    max(missing_meters.occurred_at)::timestamptz AS last_seen_at
FROM missing_meters
WHERE sqlc.narg(after_provider)::text IS NULL
    OR (missing_meters.provider COLLATE "C", missing_meters.model COLLATE "C", missing_meters.meter COLLATE "C") > (
        sqlc.narg(after_provider)::text COLLATE "C",
        sqlc.narg(after_model)::text COLLATE "C",
        sqlc.narg(after_meter)::text COLLATE "C"
    )
GROUP BY missing_meters.provider, missing_meters.model, missing_meters.meter
ORDER BY missing_meters.provider COLLATE "C", missing_meters.model COLLATE "C", missing_meters.meter COLLATE "C"
LIMIT sqlc.arg(row_limit)::bigint;
