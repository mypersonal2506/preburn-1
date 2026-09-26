-- name: LockDecision :one
SELECT decision_id, customer_id, customer_user_id, feature, provider, model, attributes, overrides, status, period_start, period_end
FROM decisions
WHERE environment = @environment
    AND decision_id = @decision_id
FOR UPDATE;

-- name: MarkDecisionSettled :exec
UPDATE decisions
SET status = 'settled',
    settled_at = sqlc.arg(settled_at)::timestamptz
WHERE decision_id = @decision_id;

-- name: MarkDecisionReleased :exec
UPDATE decisions
SET status = 'released'
WHERE decision_id = @decision_id;
