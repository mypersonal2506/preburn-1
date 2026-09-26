-- name: DeleteUsageEstimates :exec
DELETE FROM usage_estimates
WHERE environment = @environment;

-- name: InsertUsageEstimates :exec
INSERT INTO usage_estimates (environment, feature, provider, model, meter, p95_quantity_micros, sample_count, refreshed_at)
SELECT ledger_entries.environment,
    ledger_entries.feature,
    ledger_entries.provider,
    ledger_entries.model,
    usage_quantities.meter,
    ceil(percentile_cont(0.95) WITHIN GROUP (ORDER BY usage_quantities.quantity_micros::bigint::double precision))::bigint,
    count(*)::integer,
    sqlc.arg(refreshed_at)::timestamptz
FROM ledger_entries
CROSS JOIN LATERAL jsonb_each_text(ledger_entries.usage) AS usage_quantities (meter, quantity_micros)
WHERE ledger_entries.environment = @environment
    AND ledger_entries.occurred_at >= sqlc.arg(since)::timestamptz
    AND ledger_entries.correction_of IS NULL
GROUP BY ledger_entries.environment, ledger_entries.feature, ledger_entries.provider, ledger_entries.model, usage_quantities.meter
HAVING count(*) >= sqlc.arg(minimum_sample_count)::integer;
