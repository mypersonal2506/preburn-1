-- name: InsertAPIKey :one
INSERT INTO api_keys (
    api_key_id, environment, name, scope, secret_hash, secret_last_four, status, created_by_member_id, created_at, updated_at
)
VALUES (
    @api_key_id, @environment, @name, @scope, @secret_hash, @secret_last_four, 'active', @created_by_member_id, @created_at, @created_at
)
RETURNING api_key_id, environment, name, scope, secret_hash, secret_last_four, status, last_used_at, created_by_member_id, created_at, updated_at;

-- name: SelectActiveAPIKeyBySecretHash :one
SELECT api_key_id, environment, scope
FROM api_keys
WHERE secret_hash = @secret_hash
    AND status = 'active';

-- name: ListAPIKeys :many
SELECT api_key_id, environment, name, scope, secret_hash, secret_last_four, status, last_used_at, created_by_member_id, created_at, updated_at
FROM api_keys
WHERE environment = @environment
    AND (
        sqlc.narg(after_created_at)::timestamptz IS NULL
        OR (created_at, api_key_id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_api_key_id)::uuid)
    )
ORDER BY created_at DESC, api_key_id DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: DisableAPIKey :execrows
UPDATE api_keys
SET status = 'disabled', updated_at = @updated_at
WHERE api_key_id = @api_key_id
    AND environment = @environment;

-- name: UpdateAPIKeyLastUsed :exec
UPDATE api_keys
SET last_used_at = sqlc.arg(last_used_at)::timestamptz
WHERE api_key_id = @api_key_id;
