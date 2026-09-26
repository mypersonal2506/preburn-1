-- name: ListPlans :many
SELECT plan_id, name, target_margin_basis_points, allowance_nanos, status
FROM plans
WHERE environment = @environment
ORDER BY name;

-- name: ListCurrentPeriodTotals :many
SELECT DISTINCT ON (period_rollups.customer_id) period_rollups.customer_id,
    customers.external_id,
    customers.display_name,
    plans.plan_id,
    period_rollups.period_start,
    period_rollups.period_end,
    period_rollups.revenue_net_nanos,
    period_rollups.cost_nanos,
    period_rollups.uncosted_count
FROM period_rollups
JOIN customers ON customers.customer_id = period_rollups.customer_id
JOIN environment_settings ON environment_settings.environment = customers.environment
LEFT JOIN plans ON plans.plan_id = coalesce(customers.plan_id, environment_settings.default_plan_id)
WHERE period_rollups.environment = @environment
    AND period_rollups.period_start <= @now
    AND period_rollups.period_end > @now
ORDER BY period_rollups.customer_id, period_rollups.period_start DESC;

-- name: ListPreviousPeriodTotals :many
WITH current_periods AS (
    SELECT DISTINCT ON (current_rollups.customer_id) current_rollups.customer_id, current_rollups.period_start
    FROM period_rollups AS current_rollups
    WHERE current_rollups.environment = @environment
        AND current_rollups.period_start <= @now
        AND current_rollups.period_end > @now
    ORDER BY current_rollups.customer_id, current_rollups.period_start DESC
)
SELECT DISTINCT ON (period_rollups.customer_id) period_rollups.customer_id,
    customers.external_id,
    customers.display_name,
    plans.plan_id,
    period_rollups.period_start,
    period_rollups.period_end,
    period_rollups.revenue_net_nanos,
    period_rollups.cost_nanos,
    period_rollups.uncosted_count
FROM period_rollups
JOIN customers ON customers.customer_id = period_rollups.customer_id
JOIN environment_settings ON environment_settings.environment = customers.environment
LEFT JOIN plans ON plans.plan_id = coalesce(customers.plan_id, environment_settings.default_plan_id)
LEFT JOIN current_periods ON current_periods.customer_id = period_rollups.customer_id
WHERE period_rollups.environment = @environment
    AND period_rollups.period_start < coalesce(current_periods.period_start, sqlc.arg(calendar_month_start)::timestamptz)
    AND period_rollups.period_end >= coalesce(current_periods.period_start, sqlc.arg(calendar_month_start)::timestamptz)
ORDER BY period_rollups.customer_id, period_rollups.period_start DESC;

-- name: ListCustomerTotalsBetween :many
WITH revenue_totals AS (
    SELECT revenue_entries.customer_id,
        sum(
            CASE WHEN revenue_entries.kind IN ('subscription', 'adjustment') THEN revenue_entries.amount_nanos
            ELSE -revenue_entries.amount_nanos END
        ) AS revenue_net_nanos
    FROM revenue_entries
    WHERE revenue_entries.environment = @environment
        AND revenue_entries.occurred_at >= @window_start
        AND revenue_entries.occurred_at < @window_end
    GROUP BY revenue_entries.customer_id
),
ledger_totals AS (
    SELECT entries.customer_id,
        sum(entries.cost_nanos) AS cost_nanos,
        count(*) FILTER (
            WHERE entries.cost_status = 'uncosted'
                AND NOT EXISTS (
                    SELECT 1
                    FROM ledger_entries AS corrections
                    WHERE corrections.environment = entries.environment
                        AND corrections.customer_id = entries.customer_id
                        AND corrections.period_start = entries.period_start
                        AND corrections.correction_of = entries.ledger_entry_id
                )
        ) AS uncosted_count
    FROM ledger_entries AS entries
    WHERE entries.environment = @environment
        AND entries.occurred_at >= @window_start
        AND entries.occurred_at < @window_end
    GROUP BY entries.customer_id
),
window_customers AS (
    SELECT revenue_totals.customer_id FROM revenue_totals
    UNION
    SELECT ledger_totals.customer_id FROM ledger_totals
)
SELECT customers.customer_id,
    customers.external_id,
    customers.display_name,
    plans.plan_id,
    coalesce(revenue_totals.revenue_net_nanos, 0)::bigint AS revenue_net_nanos,
    coalesce(ledger_totals.cost_nanos, 0)::bigint AS cost_nanos,
    coalesce(ledger_totals.uncosted_count, 0)::integer AS uncosted_count
FROM window_customers
JOIN customers ON customers.customer_id = window_customers.customer_id
JOIN environment_settings ON environment_settings.environment = customers.environment
LEFT JOIN plans ON plans.plan_id = coalesce(customers.plan_id, environment_settings.default_plan_id)
LEFT JOIN revenue_totals ON revenue_totals.customer_id = window_customers.customer_id
LEFT JOIN ledger_totals ON ledger_totals.customer_id = window_customers.customer_id
ORDER BY customers.customer_id;

-- name: SumDecisionsInPeriods :one
SELECT count(*) FILTER (WHERE outcome = 'allow') AS allow_count,
    count(*) FILTER (WHERE outcome = 'route') AS route_count,
    count(*) FILTER (WHERE outcome = 'cap') AS cap_count,
    count(*) FILTER (WHERE outcome = 'deny') AS deny_count,
    coalesce(sum(requested_estimated_cost_nanos) FILTER (WHERE outcome = 'deny'), 0)::bigint AS denied_nanos,
    coalesce(sum(greatest(requested_estimated_cost_nanos - estimated_cost_nanos, 0)) FILTER (WHERE outcome = 'route'), 0)::bigint AS routed_nanos,
    coalesce(sum(greatest(requested_estimated_cost_nanos - estimated_cost_nanos, 0)) FILTER (WHERE outcome = 'cap'), 0)::bigint AS capped_nanos
FROM decisions
WHERE environment = @environment
    AND (customer_id, period_start) IN (
        SELECT unnest(@customer_ids::uuid[]), unnest(@period_starts::timestamptz[])
    );

-- name: SumDecisionsBetween :one
SELECT count(*) FILTER (WHERE outcome = 'allow') AS allow_count,
    count(*) FILTER (WHERE outcome = 'route') AS route_count,
    count(*) FILTER (WHERE outcome = 'cap') AS cap_count,
    count(*) FILTER (WHERE outcome = 'deny') AS deny_count,
    coalesce(sum(requested_estimated_cost_nanos) FILTER (WHERE outcome = 'deny'), 0)::bigint AS denied_nanos,
    coalesce(sum(greatest(requested_estimated_cost_nanos - estimated_cost_nanos, 0)) FILTER (WHERE outcome = 'route'), 0)::bigint AS routed_nanos,
    coalesce(sum(greatest(requested_estimated_cost_nanos - estimated_cost_nanos, 0)) FILTER (WHERE outcome = 'cap'), 0)::bigint AS capped_nanos
FROM decisions
WHERE environment = @environment
    AND created_at >= @window_start
    AND created_at < @window_end;

-- name: ListDailyTotals :many
SELECT daily_rows.day::timestamptz AS day,
    sum(daily_rows.revenue_net_nanos)::bigint AS revenue_net_nanos,
    sum(daily_rows.cost_nanos)::bigint AS cost_nanos
FROM (
    SELECT date_trunc('day', revenue_entries.occurred_at, 'UTC') AS day,
        CASE WHEN revenue_entries.kind IN ('subscription', 'adjustment') THEN revenue_entries.amount_nanos
        ELSE -revenue_entries.amount_nanos END AS revenue_net_nanos,
        0::bigint AS cost_nanos
    FROM revenue_entries
    WHERE revenue_entries.environment = @environment
        AND revenue_entries.occurred_at >= @range_start
        AND revenue_entries.occurred_at < @range_end
    UNION ALL
    SELECT date_trunc('day', ledger_entries.occurred_at, 'UTC') AS day,
        0::bigint AS revenue_net_nanos,
        coalesce(ledger_entries.cost_nanos, 0) AS cost_nanos
    FROM ledger_entries
    WHERE ledger_entries.environment = @environment
        AND ledger_entries.occurred_at >= @range_start
        AND ledger_entries.occurred_at < @range_end
) AS daily_rows
GROUP BY daily_rows.day
ORDER BY daily_rows.day;

-- name: ListLatestPolicyOutcomes :many
SELECT latest.customer_id, latest.outcome, latest.matched_policy_id, latest.created_at
FROM unnest(@customer_ids::uuid[]) AS watched_customers (customer_id)
CROSS JOIN LATERAL (
    SELECT decisions.customer_id, decisions.outcome, decisions.matched_policy_id, decisions.created_at
    FROM decisions
    WHERE decisions.environment = @environment
        AND decisions.customer_id = watched_customers.customer_id
        AND decisions.matched_policy_id IS NOT NULL
    ORDER BY decisions.created_at DESC, decisions.decision_id DESC
    LIMIT 1
) AS latest;

-- name: ListRecentPolicyChanges :many
SELECT policy_id, name, status, version, created_at, updated_at
FROM policies
WHERE environment = @environment
ORDER BY updated_at DESC, policy_id DESC
LIMIT sqlc.arg(row_limit)::bigint;
