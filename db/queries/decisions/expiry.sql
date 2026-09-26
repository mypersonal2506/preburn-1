-- name: MarkDecisionsExpired :many
WITH expired_decisions AS (
    UPDATE decisions
    SET status = 'expired'
    WHERE decision_id = ANY (@decision_ids::uuid[])
        AND status = 'reserved'
    RETURNING decisions.environment, decisions.customer_id, decisions.period_start
)
SELECT DISTINCT expired_decisions.environment, expired_decisions.customer_id, expired_decisions.period_start
FROM expired_decisions;
