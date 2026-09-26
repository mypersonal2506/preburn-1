-- name: InsertPlan :one
INSERT INTO plans (
    plan_id, environment, name, target_margin_basis_points, allowance_nanos, hold_times, status, created_at, updated_at
)
VALUES (
    @plan_id, @environment, @name, @target_margin_basis_points, @allowance_nanos, @hold_times, 'active', @created_at, @created_at
)
RETURNING plan_id, environment, name, target_margin_basis_points, allowance_nanos, hold_times, status, import_id, created_at, updated_at;

-- name: SelectPlan :one
SELECT plan_id, environment, name, target_margin_basis_points, allowance_nanos, hold_times, status, import_id, created_at, updated_at
FROM plans
WHERE environment = @environment
    AND plan_id = @plan_id;

-- name: SelectPlanForUpdate :one
SELECT plan_id, environment, name, target_margin_basis_points, allowance_nanos, hold_times, status, import_id, created_at, updated_at
FROM plans
WHERE environment = @environment
    AND plan_id = @plan_id
FOR UPDATE;

-- name: UpdatePlan :one
UPDATE plans
SET name = @name,
    target_margin_basis_points = @target_margin_basis_points,
    allowance_nanos = @allowance_nanos,
    hold_times = @hold_times,
    status = @status,
    updated_at = @updated_at
WHERE plan_id = @plan_id
RETURNING plan_id, environment, name, target_margin_basis_points, allowance_nanos, hold_times, status, import_id, created_at, updated_at;

-- name: ListPlans :many
SELECT plan_id, environment, name, target_margin_basis_points, allowance_nanos, hold_times, status, import_id, created_at, updated_at
FROM plans
WHERE environment = @environment
    AND (
        sqlc.narg(after_created_at)::timestamptz IS NULL
        OR (created_at, plan_id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_plan_id)::uuid)
    )
ORDER BY created_at DESC, plan_id DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: CountCustomersPerPlan :many
SELECT coalesce(customers.plan_id, environment_settings.default_plan_id)::uuid AS plan_id, count(*) AS customer_count
FROM customers
JOIN environment_settings ON environment_settings.environment = customers.environment
WHERE customers.environment = @environment
    AND coalesce(customers.plan_id, environment_settings.default_plan_id) = ANY (sqlc.arg(plan_ids)::uuid[])
GROUP BY coalesce(customers.plan_id, environment_settings.default_plan_id);

-- name: SelectDefaultPlanID :one
SELECT default_plan_id
FROM environment_settings
WHERE environment = @environment;
