-- name: InsertPolicy :one
INSERT INTO policies (
    policy_id, environment, name, level, plan_id, customer_id, feature, condition_group, action,
    enforcement, on_unreachable, on_uncosted, status, version, created_at, updated_at
)
VALUES (
    @policy_id, @environment, @name, @level, @plan_id, @customer_id, @feature, @condition_group, @action,
    @enforcement, @on_unreachable, @on_uncosted, @status, 1, @created_at, @created_at
)
RETURNING policy_id, environment, name, level, plan_id, customer_id, feature, condition_group, action,
    enforcement, on_unreachable, on_uncosted, status, version, import_id, created_at, updated_at;

-- name: SelectPolicy :one
SELECT policy_id, environment, name, level, plan_id, customer_id, feature, condition_group, action,
    enforcement, on_unreachable, on_uncosted, status, version, import_id, created_at, updated_at
FROM policies
WHERE environment = @environment
    AND policy_id = @policy_id;

-- name: SelectPolicyForUpdate :one
SELECT policy_id, environment, name, level, plan_id, customer_id, feature, condition_group, action,
    enforcement, on_unreachable, on_uncosted, status, version, import_id, created_at, updated_at
FROM policies
WHERE environment = @environment
    AND policy_id = @policy_id
FOR UPDATE;

-- name: UpdatePolicy :one
UPDATE policies
SET name = @name,
    level = @level,
    plan_id = @plan_id,
    customer_id = @customer_id,
    feature = @feature,
    condition_group = @condition_group,
    action = @action,
    enforcement = @enforcement,
    on_unreachable = @on_unreachable,
    on_uncosted = @on_uncosted,
    status = @status,
    version = version + 1,
    updated_at = @updated_at
WHERE policy_id = @policy_id
RETURNING policy_id, environment, name, level, plan_id, customer_id, feature, condition_group, action,
    enforcement, on_unreachable, on_uncosted, status, version, import_id, created_at, updated_at;

-- name: ListPolicies :many
SELECT policy_id, environment, name, level, plan_id, customer_id, feature, condition_group, action,
    enforcement, on_unreachable, on_uncosted, status, version, import_id, created_at, updated_at
FROM policies
WHERE environment = @environment
    AND (sqlc.narg(status)::text IS NULL OR status::text = sqlc.narg(status)::text)
    AND (sqlc.narg(plan_id)::uuid IS NULL OR plan_id = sqlc.narg(plan_id)::uuid)
    AND (
        sqlc.narg(after_created_at)::timestamptz IS NULL
        OR (created_at, policy_id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_policy_id)::uuid)
    )
ORDER BY created_at DESC, policy_id DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: ListActivePolicies :many
SELECT policy_id, environment, name, level, plan_id, customer_id, feature, condition_group, action,
    enforcement, on_unreachable, on_uncosted, status, version, import_id, created_at, updated_at
FROM policies
WHERE environment = @environment
    AND status = 'active'
ORDER BY policy_id;

-- name: SelectPlanStatus :one
SELECT status
FROM plans
WHERE environment = @environment
    AND plan_id = @plan_id;

-- name: CustomerExists :one
SELECT EXISTS (
    SELECT 1
    FROM customers
    WHERE environment = @environment
        AND customer_id = @customer_id
);

-- name: SelectAliasedModel :one
SELECT model
FROM provider_model_aliases
WHERE provider = @provider
    AND alias = @alias;
