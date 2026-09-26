-- name: ListLiveCounters :many
SELECT counter_periods.customer_id,
    counter_periods.period_start,
    max(counter_periods.period_end)::timestamptz AS period_end
FROM (
    SELECT decisions.customer_id, decisions.period_start, decisions.period_end
    FROM decisions
    WHERE decisions.environment = @environment
        AND (
            (decisions.period_start <= @now AND decisions.period_end > @now)
            OR (decisions.status = 'reserved' AND decisions.expires_at > @reserved_after)
        )
    UNION ALL
    SELECT ledger_entries.customer_id, ledger_entries.period_start, ledger_entries.period_end
    FROM ledger_entries
    WHERE ledger_entries.environment = @environment
        AND ledger_entries.period_start <= @now
        AND ledger_entries.period_end > @now
) AS counter_periods
GROUP BY counter_periods.customer_id, counter_periods.period_start
ORDER BY counter_periods.customer_id, counter_periods.period_start;

-- name: ListActiveCustomerLiveCounters :many
WITH active_customers AS (
    SELECT decisions.customer_id
    FROM decisions
    WHERE decisions.environment = @environment
        AND decisions.created_at >= @active_since
    UNION
    SELECT decisions.customer_id
    FROM decisions
    WHERE decisions.environment = @environment
        AND decisions.status IN ('released', 'expired')
        AND decisions.expires_at >= @active_since
    UNION
    SELECT ledger_entries.customer_id
    FROM ledger_entries
    WHERE ledger_entries.environment = @environment
        AND ledger_entries.created_at >= @active_since
)
SELECT counter_periods.customer_id,
    counter_periods.period_start,
    max(counter_periods.period_end)::timestamptz AS period_end
FROM (
    SELECT decisions.customer_id, decisions.period_start, decisions.period_end
    FROM decisions
    JOIN active_customers ON active_customers.customer_id = decisions.customer_id
    WHERE decisions.environment = @environment
        AND (
            (decisions.period_start <= @now AND decisions.period_end > @now)
            OR (decisions.status = 'reserved' AND decisions.expires_at > @reserved_after)
        )
    UNION ALL
    SELECT ledger_entries.customer_id, ledger_entries.period_start, ledger_entries.period_end
    FROM ledger_entries
    JOIN active_customers ON active_customers.customer_id = ledger_entries.customer_id
    WHERE ledger_entries.environment = @environment
        AND ledger_entries.period_start <= @now
        AND ledger_entries.period_end > @now
) AS counter_periods
GROUP BY counter_periods.customer_id, counter_periods.period_start
ORDER BY counter_periods.customer_id, counter_periods.period_start;

-- name: ListCounterFeatureTotals :many
SELECT counter_rows.customer_id,
    counter_rows.period_start,
    counter_rows.feature,
    sum(counter_rows.settled_nanos)::bigint AS settled_nanos,
    sum(counter_rows.reserved_nanos)::bigint AS reserved_nanos,
    sum(counter_rows.request_count)::bigint AS request_count
FROM (
    SELECT decisions.customer_id,
        decisions.period_start,
        decisions.feature,
        0::bigint AS settled_nanos,
        CASE
            WHEN decisions.status = 'reserved' AND decisions.expires_at > @reserved_after THEN decisions.reserved_nanos
            ELSE 0
        END AS reserved_nanos,
        CASE WHEN decisions.reason = 'hard_limit_reached' THEN 0 ELSE 1 END AS request_count
    FROM decisions
    WHERE decisions.environment = @environment
        AND (decisions.customer_id, decisions.period_start) IN (
            SELECT unnest(@customer_ids::uuid[]), unnest(@period_starts::timestamptz[])
        )
    UNION ALL
    SELECT ledger_entries.customer_id,
        ledger_entries.period_start,
        ledger_entries.feature,
        coalesce(ledger_entries.cost_nanos, 0),
        0,
        CASE WHEN ledger_entries.decision_id IS NULL AND ledger_entries.correction_of IS NULL THEN 1 ELSE 0 END
    FROM ledger_entries
    WHERE ledger_entries.environment = @environment
        AND (ledger_entries.customer_id, ledger_entries.period_start) IN (
            SELECT unnest(@customer_ids::uuid[]), unnest(@period_starts::timestamptz[])
        )
) AS counter_rows
GROUP BY counter_rows.customer_id, counter_rows.period_start, counter_rows.feature;

-- name: ListFinishedDecisions :many
SELECT decision_id, status
FROM decisions
WHERE environment = @environment
    AND decision_id = ANY (@decision_ids::uuid[])
    AND status IN ('settled', 'released', 'expired');
