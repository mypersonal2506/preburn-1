-- name: CountActiveCustomers :one
SELECT count(*)
FROM customers
WHERE environment = @environment
    AND status = 'active';

-- name: ListPeriodCounters :many
SELECT counter_rows.customer_id,
    sum(counter_rows.settled_nanos)::bigint AS settled_nanos,
    sum(counter_rows.decision_count)::bigint AS decision_count
FROM (
    SELECT decisions.customer_id,
        0::bigint AS settled_nanos,
        CASE WHEN decisions.reason = 'hard_limit_reached' THEN 0 ELSE 1 END AS decision_count
    FROM decisions
    WHERE decisions.environment = @environment
        AND (decisions.customer_id, decisions.period_start) IN (
            SELECT unnest(@customer_ids::uuid[]), unnest(@period_starts::timestamptz[])
        )
    UNION ALL
    SELECT ledger_entries.customer_id,
        coalesce(ledger_entries.cost_nanos, 0),
        CASE WHEN ledger_entries.decision_id IS NULL AND ledger_entries.correction_of IS NULL THEN 1 ELSE 0 END
    FROM ledger_entries
    WHERE ledger_entries.environment = @environment
        AND (ledger_entries.customer_id, ledger_entries.period_start) IN (
            SELECT unnest(@customer_ids::uuid[]), unnest(@period_starts::timestamptz[])
        )
) AS counter_rows
GROUP BY counter_rows.customer_id;
