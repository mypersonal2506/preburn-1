-- name: InsertRevenueEntry :one
INSERT INTO revenue_entries (
    revenue_entry_id, environment, customer_id, period_start, period_end, kind, amount_nanos, source, source_reference, occurred_at, created_at
)
VALUES (
    @revenue_entry_id, @environment, @customer_id, @period_start, @period_end, @kind, @amount_nanos, @source, @source_reference, @occurred_at, @created_at
)
ON CONFLICT (environment, source, kind, source_reference) DO NOTHING
RETURNING revenue_entry_id, environment, customer_id, period_start, period_end, kind, amount_nanos, source, source_reference, occurred_at, import_id, created_at;

-- name: SelectRevenueEntryBySourceReference :one
SELECT sqlc.embed(revenue_entries), customers.external_id
FROM revenue_entries
JOIN customers ON customers.customer_id = revenue_entries.customer_id
    AND customers.environment = revenue_entries.environment
WHERE revenue_entries.environment = @environment
    AND revenue_entries.source = @source
    AND revenue_entries.kind = @kind
    AND revenue_entries.source_reference = @source_reference;

-- name: ListRevenueEntries :many
SELECT sqlc.embed(revenue_entries), customers.external_id
FROM revenue_entries
JOIN customers ON customers.customer_id = revenue_entries.customer_id
    AND customers.environment = revenue_entries.environment
WHERE revenue_entries.environment = @environment
    AND (
        sqlc.narg(customer_external_id)::text IS NULL
        OR customers.external_id = sqlc.narg(customer_external_id)::text
    )
    AND (
        sqlc.narg(kind)::text IS NULL
        OR revenue_entries.kind = sqlc.narg(kind)::text
    )
    AND (
        sqlc.narg(after_created_at)::timestamptz IS NULL
        OR (revenue_entries.created_at, revenue_entries.revenue_entry_id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_revenue_entry_id)::uuid)
    )
ORDER BY revenue_entries.created_at DESC, revenue_entries.revenue_entry_id DESC
LIMIT sqlc.arg(row_limit)::bigint;
