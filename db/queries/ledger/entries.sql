-- name: InsertLedgerEntry :one
INSERT INTO ledger_entries (
    ledger_entry_id,
    environment,
    customer_id,
    customer_user_id,
    decision_id,
    idempotency_key,
    feature,
    provider,
    model,
    attributes,
    usage,
    cost_nanos,
    cost_breakdown,
    cost_status,
    decision_source,
    period_start,
    period_end,
    correction_of,
    occurred_at,
    created_at
) VALUES (
    @ledger_entry_id,
    @environment,
    @customer_id,
    @customer_user_id,
    @decision_id,
    @idempotency_key,
    @feature,
    @provider,
    @model,
    @attributes,
    @usage,
    @cost_nanos,
    @cost_breakdown,
    @cost_status,
    @decision_source,
    @period_start,
    @period_end,
    @correction_of,
    @occurred_at,
    @created_at
)
ON CONFLICT (environment, idempotency_key) DO NOTHING
RETURNING ledger_entry_id, environment, customer_id, customer_user_id, decision_id, idempotency_key, feature, provider, model,
    attributes, usage, cost_nanos, cost_breakdown, cost_status, decision_source, period_start, period_end, correction_of,
    occurred_at, import_id, created_at;

-- name: SelectLedgerEntryByIdempotencyKey :one
SELECT ledger_entry_id, environment, customer_id, customer_user_id, decision_id, idempotency_key, feature, provider, model,
    attributes, usage, cost_nanos, cost_breakdown, cost_status, decision_source, period_start, period_end, correction_of,
    occurred_at, import_id, created_at
FROM ledger_entries
WHERE environment = @environment
    AND idempotency_key = @idempotency_key;
