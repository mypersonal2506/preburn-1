-- name: ListDecisions :many
SELECT sqlc.embed(decisions),
    customers.external_id,
    customers.display_name
FROM decisions
JOIN customers ON customers.customer_id = decisions.customer_id
WHERE decisions.environment = @environment
    AND (sqlc.narg(outcome)::text IS NULL OR decisions.outcome = sqlc.narg(outcome)::text)
    AND (sqlc.narg(customer_id)::uuid IS NULL OR decisions.customer_id = sqlc.narg(customer_id)::uuid)
    AND (sqlc.narg(feature)::text IS NULL OR decisions.feature = sqlc.narg(feature)::text)
    AND (sqlc.narg(policy_id)::uuid IS NULL OR decisions.matched_policy_id = sqlc.narg(policy_id)::uuid)
    AND (
        sqlc.narg(before_created_at)::timestamptz IS NULL
        OR (decisions.created_at, decisions.decision_id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.narg(before_decision_id)::uuid)
    )
ORDER BY decisions.created_at DESC, decisions.decision_id DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: SelectDecision :one
SELECT sqlc.embed(decisions),
    customers.external_id,
    customers.display_name,
    customer_users.external_id AS customer_user_external_id
FROM decisions
JOIN customers ON customers.customer_id = decisions.customer_id
LEFT JOIN customer_users ON customer_users.customer_user_id = decisions.customer_user_id
WHERE decisions.environment = @environment
    AND decisions.decision_id = @decision_id;

-- name: ListDecisionLedgerEntries :many
SELECT entries.ledger_entry_id,
    entries.provider,
    entries.model,
    entries.usage,
    entries.cost_nanos,
    entries.cost_status,
    entries.correction_of,
    entries.occurred_at,
    entries.created_at
FROM ledger_entries AS entries
WHERE entries.environment = @environment
    AND entries.customer_id = @customer_id
    AND entries.period_start = @period_start
    AND (
        entries.decision_id = sqlc.arg(decision_id)::uuid
        OR entries.correction_of IN (
            SELECT reported.ledger_entry_id
            FROM ledger_entries AS reported
            WHERE reported.environment = @environment
                AND reported.customer_id = @customer_id
                AND reported.period_start = @period_start
                AND reported.decision_id = sqlc.arg(decision_id)::uuid
        )
    )
ORDER BY entries.created_at, entries.ledger_entry_id;
