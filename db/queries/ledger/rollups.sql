-- name: LockRollupCustomer :one
SELECT customer_id
FROM customers
WHERE environment = @environment
    AND customer_id = @customer_id
FOR NO KEY UPDATE;

-- name: ListRecordedBillingPeriods :many
SELECT recorded_periods.period_start, max(recorded_periods.period_end)::timestamptz AS period_end
FROM (
    SELECT revenue_entries.period_start, revenue_entries.period_end
    FROM revenue_entries
    WHERE revenue_entries.environment = @environment
        AND revenue_entries.customer_id = @customer_id
        AND revenue_entries.kind = 'subscription'
        AND revenue_entries.period_end > revenue_entries.period_start
    UNION ALL
    SELECT period_rollups.period_start, period_rollups.period_end
    FROM period_rollups
    WHERE period_rollups.environment = @environment
        AND period_rollups.customer_id = @customer_id
) AS recorded_periods
GROUP BY recorded_periods.period_start;

-- name: SumLedgerEntriesByPeriod :many
WITH period_entries AS (
    SELECT ledger_entry_id, period_start, period_end, cost_nanos, cost_status, correction_of
    FROM ledger_entries
    WHERE environment = @environment
        AND customer_id = @customer_id
        AND period_start = ANY (sqlc.arg(period_starts)::timestamptz[])
),
corrected_entries AS (
    SELECT DISTINCT correction_of
    FROM period_entries
    WHERE correction_of IS NOT NULL
)
SELECT period_entries.period_start,
    max(period_entries.period_end)::timestamptz AS period_end,
    coalesce(sum(period_entries.cost_nanos), 0)::bigint AS cost_nanos,
    (count(*) FILTER (
        WHERE period_entries.cost_status = 'uncosted' AND corrected_entries.correction_of IS NULL
    ))::integer AS uncosted_count
FROM period_entries
LEFT JOIN corrected_entries ON corrected_entries.correction_of = period_entries.ledger_entry_id
GROUP BY period_entries.period_start;

-- name: ListRevenueEntriesStartingWithin :many
SELECT period_start, kind, amount_nanos
FROM revenue_entries
WHERE environment = @environment
    AND customer_id = @customer_id
    AND period_start >= sqlc.arg(earliest_start)::timestamptz
    AND period_start < sqlc.arg(latest_end)::timestamptz;

-- name: CountDecisionsByPeriod :many
SELECT period_start,
    max(period_end)::timestamptz AS period_end,
    count(*) FILTER (WHERE outcome = 'allow') AS allow_count,
    count(*) FILTER (WHERE outcome = 'route') AS route_count,
    count(*) FILTER (WHERE outcome = 'cap') AS cap_count,
    count(*) FILTER (WHERE outcome = 'deny') AS deny_count
FROM decisions
WHERE environment = @environment
    AND customer_id = @customer_id
    AND period_start = ANY (sqlc.arg(period_starts)::timestamptz[])
GROUP BY period_start;

-- name: UpsertPeriodRollup :exec
INSERT INTO period_rollups (
    environment, customer_id, period_start, period_end, revenue_net_nanos, cost_nanos, uncosted_count, decision_counts, updated_at
)
VALUES (
    @environment, @customer_id, @period_start, @period_end, @revenue_net_nanos, @cost_nanos, @uncosted_count, @decision_counts, now()
)
ON CONFLICT (environment, customer_id, period_start) DO UPDATE
SET period_end = EXCLUDED.period_end,
    revenue_net_nanos = EXCLUDED.revenue_net_nanos,
    cost_nanos = EXCLUDED.cost_nanos,
    uncosted_count = EXCLUDED.uncosted_count,
    decision_counts = EXCLUDED.decision_counts,
    updated_at = EXCLUDED.updated_at;
