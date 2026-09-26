-- name: ListMembers :many
SELECT member_id, email, display_name, password_hash, status, last_login_at, created_at, updated_at
FROM members
WHERE sqlc.narg(after_created_at)::timestamptz IS NULL
    OR (created_at, member_id) > (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_member_id)::uuid)
ORDER BY created_at, member_id
LIMIT @row_limit::bigint;

-- name: UpdateMemberStatus :exec
UPDATE members
SET status = @status, updated_at = @updated_at
WHERE member_id = @member_id;

-- name: LockActiveMembers :one
SELECT
    count(*) FILTER (WHERE active_members.password_hash IS NOT NULL) AS password_member_count,
    count(*) FILTER (WHERE active_members.member_id = @actor_member_id::uuid) = 1 AS actor_active
FROM (
    SELECT member_id, password_hash
    FROM members
    WHERE status = 'active'
    ORDER BY member_id
    FOR UPDATE
) AS active_members;

-- name: DeleteSessionsForMember :exec
DELETE FROM member_sessions
WHERE member_id = @member_id;

-- name: InsertMemberLink :exec
INSERT INTO member_links (member_link_id, member_id, purpose, token_hash, expires_at, created_by_member_id, created_at)
VALUES (@member_link_id, @member_id, @purpose, @token_hash, @expires_at, @created_by_member_id, @created_at);

-- name: SelectMemberLinkWithMember :one
SELECT sqlc.embed(member_links), sqlc.embed(members)
FROM member_links
JOIN members ON members.member_id = member_links.member_id
WHERE member_links.token_hash = @token_hash;

-- name: SelectMemberLinkForUpdate :one
SELECT member_link_id, member_id, purpose, token_hash, expires_at, consumed_at, created_by_member_id, created_at
FROM member_links
WHERE member_link_id = @member_link_id
FOR UPDATE;

-- name: ConsumeMemberLink :exec
UPDATE member_links
SET consumed_at = @consumed_at::timestamptz
WHERE member_link_id = @member_link_id;

-- name: ExpireUnconsumedMemberLinks :exec
UPDATE member_links
SET expires_at = @now
WHERE member_id = @member_id
    AND consumed_at IS NULL
    AND expires_at > @now;

-- name: DeleteExpiredSessions :execrows
DELETE FROM member_sessions
WHERE session_token_hash IN (
    SELECT expired_sessions.session_token_hash
    FROM member_sessions AS expired_sessions
    WHERE expired_sessions.expires_at <= @now
    LIMIT @batch_size::bigint
);

-- name: DeleteOldMemberLinks :execrows
DELETE FROM member_links
WHERE member_link_id IN (
    SELECT old_links.member_link_id
    FROM member_links AS old_links
    WHERE old_links.expires_at < @cutoff OR old_links.consumed_at < @cutoff
    LIMIT @batch_size::bigint
);
