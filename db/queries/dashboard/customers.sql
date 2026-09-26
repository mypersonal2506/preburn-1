-- name: CountActiveCustomers :one
SELECT count(*)
FROM customers
WHERE environment = @environment
    AND status = 'active';

-- name: ListActiveCustomers :many
SELECT customer_id, external_id, display_name
FROM customers
WHERE environment = @environment
    AND status = 'active';

-- name: SelectCustomer :one
SELECT customer_id, external_id, display_name, status, created_at
FROM customers
WHERE environment = @environment
    AND customer_id = @customer_id;

-- name: ListCustomerRollups :many
SELECT period_start, period_end, revenue_net_nanos, cost_nanos, uncosted_count, decision_counts
FROM period_rollups
WHERE environment = @environment
    AND customer_id = @customer_id
ORDER BY period_start DESC;

-- name: ListCustomerPeriodUsage :many
SELECT entries.feature,
    entries.provider,
    entries.model,
    (count(*) FILTER (WHERE entries.correction_of IS NULL))::bigint AS request_count,
    coalesce(sum(entries.cost_nanos), 0)::bigint AS cost_nanos,
    (count(*) FILTER (
        WHERE entries.cost_status = 'uncosted'
            AND NOT EXISTS (
                SELECT 1
                FROM ledger_entries AS corrections
                WHERE corrections.environment = entries.environment
                    AND corrections.customer_id = entries.customer_id
                    AND corrections.period_start = entries.period_start
                    AND corrections.correction_of = entries.ledger_entry_id
            )
    ))::bigint AS uncosted_count
FROM ledger_entries AS entries
WHERE entries.environment = @environment
    AND entries.customer_id = @customer_id
    AND entries.period_start = @period_start
GROUP BY entries.feature, entries.provider, entries.model
ORDER BY cost_nanos DESC, entries.feature, entries.provider, entries.model;

-- name: ListRecentCustomerDecisions :many
SELECT decision_id,
    feature,
    requested_provider,
    requested_model,
    provider,
    model,
    outcome,
    reason,
    matched_policy_id,
    estimated_cost_nanos,
    status,
    created_at
FROM decisions
WHERE environment = @environment
    AND customer_id = @customer_id
ORDER BY created_at DESC, decision_id DESC
LIMIT sqlc.arg(row_limit)::bigint;
