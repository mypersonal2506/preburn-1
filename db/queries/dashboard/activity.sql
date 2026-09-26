-- name: ListLedgerEntryEvents :many
SELECT entries.ledger_entry_id,
    entries.customer_id,
    customers.external_id,
    customers.display_name,
    entries.decision_id,
    entries.feature,
    entries.provider,
    entries.model,
    entries.cost_nanos,
    entries.occurred_at
FROM ledger_entries AS entries
JOIN customers ON customers.customer_id = entries.customer_id
WHERE entries.environment = @environment
    AND (
        sqlc.narg(before_occurred_at)::timestamptz IS NULL
        OR (entries.occurred_at, entries.ledger_entry_id) < (sqlc.narg(before_occurred_at)::timestamptz, sqlc.narg(before_ledger_entry_id)::uuid)
    )
ORDER BY entries.occurred_at DESC, entries.ledger_entry_id DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: SelectOnboardingFlags :one
SELECT EXISTS (
        SELECT 1
        FROM api_keys
        WHERE api_keys.environment = @environment
            AND api_keys.status = 'active'
    ) AS has_api_key,
    EXISTS (
        SELECT 1
        FROM plans
        WHERE plans.environment = @environment
            AND plans.status = 'active'
    ) AS has_plan,
    EXISTS (
        SELECT 1
        FROM policies
        WHERE policies.environment = @environment
            AND policies.status = 'active'
    ) AS has_policy,
    EXISTS (
        SELECT 1
        FROM revenue_entries
        WHERE revenue_entries.environment = @environment
    ) AS has_revenue;

-- name: SelectFirstDecisionCreatedAt :one
SELECT created_at
FROM decisions
WHERE environment = @environment
ORDER BY created_at
LIMIT 1;

-- name: ListFeatureSources :many
SELECT usage_estimates.feature, 'usage_estimates'::text AS source
FROM usage_estimates
WHERE usage_estimates.environment = @environment
UNION
SELECT policies.feature, 'policies'::text
FROM policies
WHERE policies.environment = @environment
    AND policies.status = 'active'
    AND policies.feature IS NOT NULL
UNION
SELECT jsonb_object_keys(plans.hold_times), 'plan_hold_times'::text
FROM plans
WHERE plans.environment = @environment
    AND plans.status = 'active'
UNION
SELECT decisions.feature, 'decisions'::text
FROM decisions
WHERE decisions.environment = @environment
    AND decisions.created_at >= @since
ORDER BY feature, source;
