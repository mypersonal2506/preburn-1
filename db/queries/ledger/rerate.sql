-- name: ListUncorrectedUncostedEntries :many
SELECT sqlc.embed(ledger_entries)
FROM ledger_entries
WHERE ledger_entries.environment = @environment
    AND ledger_entries.cost_status = 'uncosted'
    AND ledger_entries.occurred_at >= sqlc.arg(occurred_since)::timestamptz
    AND ledger_entries.ledger_entry_id > sqlc.arg(after_ledger_entry_id)::uuid
    AND NOT EXISTS (
        SELECT 1
        FROM ledger_entries AS corrections
        WHERE corrections.environment = ledger_entries.environment
            AND corrections.idempotency_key = 'correction:' || ledger_entries.ledger_entry_id::text
    )
ORDER BY ledger_entries.ledger_entry_id
LIMIT sqlc.arg(batch_size)::bigint;
