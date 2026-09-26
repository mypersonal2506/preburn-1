-- name: UpsertCustomer :one
INSERT INTO customers (
    customer_id, environment, external_id, display_name, plan_id, metadata, status, created_at, updated_at
)
VALUES (
    @customer_id, @environment, @external_id, sqlc.narg(display_name), sqlc.narg(plan_id), @metadata, 'active', @written_at, @written_at
)
ON CONFLICT (environment, external_id) DO UPDATE
SET display_name = EXCLUDED.display_name,
    plan_id = EXCLUDED.plan_id,
    metadata = EXCLUDED.metadata,
    updated_at = EXCLUDED.updated_at
RETURNING customer_id, environment, external_id, display_name, plan_id, stripe_customer_id, metadata, status, import_id, created_at, updated_at;

-- name: InsertCustomerIfMissing :one
INSERT INTO customers (customer_id, environment, external_id, status, created_at, updated_at)
VALUES (@customer_id, @environment, @external_id, 'active', @created_at, @created_at)
ON CONFLICT (environment, external_id) DO NOTHING
RETURNING customer_id, environment, external_id, display_name, plan_id, stripe_customer_id, metadata, status, import_id, created_at, updated_at;

-- name: SelectCustomerByExternalID :one
SELECT customer_id, environment, external_id, display_name, plan_id, stripe_customer_id, metadata, status, import_id, created_at, updated_at
FROM customers
WHERE environment = @environment
    AND external_id = @external_id;

-- name: SelectCustomerByID :one
SELECT customer_id, environment, external_id, display_name, plan_id, stripe_customer_id, metadata, status, import_id, created_at, updated_at
FROM customers
WHERE environment = @environment
    AND customer_id = @customer_id;

-- name: ListCustomers :many
SELECT customer_id, environment, external_id, display_name, plan_id, stripe_customer_id, metadata, status, import_id, created_at, updated_at
FROM customers
WHERE environment = @environment
    AND (
        sqlc.narg(external_id_pattern)::text IS NULL
        OR lower(external_id) LIKE sqlc.narg(external_id_pattern)::text
    )
    AND (
        sqlc.narg(after_created_at)::timestamptz IS NULL
        OR (created_at, customer_id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_customer_id)::uuid)
    )
ORDER BY created_at DESC, customer_id DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: SelectActivePlanExists :one
SELECT EXISTS (
    SELECT 1
    FROM plans
    WHERE environment = @environment
        AND plan_id = @plan_id
        AND status = 'active'
);
