-- name: SelectSettings :one
SELECT installation.name AS installation_name, environment_settings.default_plan_id, environment_settings.stripe_customer_metadata_key
FROM installation
CROSS JOIN environment_settings
WHERE installation.installation_id = 1
    AND environment_settings.environment = @environment;

-- name: SelectSettingsForUpdate :one
SELECT installation.name AS installation_name, environment_settings.default_plan_id, environment_settings.stripe_customer_metadata_key
FROM installation
CROSS JOIN environment_settings
WHERE installation.installation_id = 1
    AND environment_settings.environment = @environment
FOR UPDATE;

-- name: SelectActivePlanForShare :one
SELECT plan_id
FROM plans
WHERE environment = @environment
    AND plan_id = @plan_id
    AND status = 'active'
FOR SHARE;

-- name: UpdateInstallationName :exec
UPDATE installation
SET name = @name, updated_at = @updated_at
WHERE installation_id = 1;

-- name: UpdateEnvironmentSettings :exec
UPDATE environment_settings
SET default_plan_id = @default_plan_id,
    stripe_customer_metadata_key = @stripe_customer_metadata_key,
    updated_at = @updated_at
WHERE environment = @environment;
