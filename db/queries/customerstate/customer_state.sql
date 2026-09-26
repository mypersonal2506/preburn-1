-- name: SelectCustomerPlan :one
SELECT customers.customer_id,
    plans.plan_id,
    plans.target_margin_basis_points,
    plans.allowance_nanos,
    coalesce(plans.hold_times, '{}') AS hold_times
FROM customers
JOIN environment_settings ON environment_settings.environment = customers.environment
LEFT JOIN plans ON plans.plan_id = coalesce(customers.plan_id, environment_settings.default_plan_id)
WHERE customers.environment = @environment
    AND customers.customer_id = @customer_id;

-- name: SelectSubscriptionRevenuePeriod :one
SELECT period_start, period_end
FROM revenue_entries
WHERE environment = @environment
    AND customer_id = @customer_id
    AND kind = 'subscription'
    AND period_start <= @now
    AND period_end > @now
ORDER BY period_start DESC, period_end DESC
LIMIT 1;

-- name: SelectPeriodRevenue :one
SELECT revenue_net_nanos
FROM period_rollups
WHERE environment = @environment
    AND customer_id = @customer_id
    AND period_start = @period_start;

-- name: ListActiveCustomerPlans :many
SELECT customers.customer_id,
    plans.plan_id,
    plans.target_margin_basis_points,
    plans.allowance_nanos,
    coalesce(plans.hold_times, '{}') AS hold_times
FROM customers
JOIN environment_settings ON environment_settings.environment = customers.environment
LEFT JOIN plans ON plans.plan_id = coalesce(customers.plan_id, environment_settings.default_plan_id)
WHERE customers.environment = @environment
    AND customers.status = 'active'
ORDER BY customers.customer_id;

-- name: ListCustomerPlans :many
SELECT customers.customer_id,
    plans.plan_id,
    plans.target_margin_basis_points,
    plans.allowance_nanos,
    coalesce(plans.hold_times, '{}') AS hold_times
FROM customers
JOIN environment_settings ON environment_settings.environment = customers.environment
LEFT JOIN plans ON plans.plan_id = coalesce(customers.plan_id, environment_settings.default_plan_id)
WHERE customers.environment = @environment
    AND customers.customer_id = ANY (@customer_ids::uuid[])
ORDER BY customers.customer_id;

-- name: ListSubscriptionRevenuePeriods :many
SELECT DISTINCT ON (customer_id) customer_id, period_start, period_end
FROM revenue_entries
WHERE environment = @environment
    AND customer_id = ANY (@customer_ids::uuid[])
    AND kind = 'subscription'
    AND period_start <= @now
    AND period_end > @now
ORDER BY customer_id, period_start DESC, period_end DESC;

-- name: ListPeriodRevenues :many
SELECT period_rollups.customer_id, period_rollups.period_start, period_rollups.revenue_net_nanos
FROM period_rollups
WHERE period_rollups.environment = @environment
    AND (period_rollups.customer_id, period_rollups.period_start) IN (
        SELECT unnest(@customer_ids::uuid[]), unnest(@period_starts::timestamptz[])
    );
