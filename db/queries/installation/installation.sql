-- name: SelectInstallation :one
SELECT installation_id, name, setup_token_hash, setup_completed_at, created_at, updated_at
FROM installation
WHERE installation_id = 1;

-- name: SelectInstallationForUpdate :one
SELECT installation_id, name, setup_token_hash, setup_completed_at, created_at, updated_at
FROM installation
WHERE installation_id = 1
FOR UPDATE;

-- name: SetSetupTokenHash :execrows
UPDATE installation
SET setup_token_hash = @setup_token_hash, updated_at = @updated_at
WHERE installation_id = 1
    AND setup_completed_at IS NULL;

-- name: CompleteSetup :exec
UPDATE installation
SET updated_at = @completed_at, setup_completed_at = @completed_at, setup_token_hash = NULL
WHERE installation_id = 1;
