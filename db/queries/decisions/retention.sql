-- name: DeleteExpiredDecisions :many
WITH expired_decisions AS (
    SELECT decisions.decision_id
    FROM decisions
    WHERE decisions.environment = @environment
        AND decisions.created_at < sqlc.arg(created_before)::timestamptz
        AND decisions.decision_id > sqlc.arg(after_decision_id)::uuid
    ORDER BY decisions.decision_id
    LIMIT sqlc.arg(batch_size)::bigint
),
deleted_decisions AS (
    DELETE FROM decisions
    WHERE decisions.decision_id IN (SELECT expired_decisions.decision_id FROM expired_decisions)
    RETURNING decisions.decision_id
)
SELECT deleted_decisions.decision_id
FROM deleted_decisions
ORDER BY deleted_decisions.decision_id;
