-- name: InsertCustomerUserIfMissing :one
INSERT INTO customer_users (customer_user_id, environment, customer_id, external_id, created_at)
VALUES (@customer_user_id, @environment, @customer_id, @external_id, @created_at)
ON CONFLICT (environment, customer_id, external_id) DO NOTHING
RETURNING customer_user_id, environment, customer_id, external_id, import_id, created_at;

-- name: SelectCustomerUser :one
SELECT customer_user_id, environment, customer_id, external_id, import_id, created_at
FROM customer_users
WHERE environment = @environment
    AND customer_id = @customer_id
    AND external_id = @external_id;
