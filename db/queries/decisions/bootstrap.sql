-- name: ListLiveReservations :many
SELECT decision_id, customer_id, feature, reserved_nanos, period_start, period_end, expires_at
FROM decisions
WHERE environment = @environment
    AND status = 'reserved'
    AND expires_at > @reserved_after
ORDER BY expires_at, decision_id;

-- name: MarkLapsedReservationsExpired :many
WITH expired_decisions AS (
    UPDATE decisions
    SET status = 'expired'
    WHERE decisions.environment = @environment
        AND decisions.status = 'reserved'
        AND decisions.expires_at <= @reserved_after
    RETURNING decisions.environment, decisions.customer_id, decisions.period_start
)
SELECT DISTINCT expired_decisions.environment, expired_decisions.customer_id, expired_decisions.period_start
FROM expired_decisions;
