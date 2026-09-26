-- name: InsertSession :exec
INSERT INTO member_sessions (session_token_hash, member_id, expires_at, last_seen_at, user_agent, ip_address, created_at)
VALUES (@session_token_hash, @member_id, @expires_at, @last_seen_at, @user_agent, @ip_address, @created_at);

-- name: SelectSessionWithMember :one
SELECT sqlc.embed(members), member_sessions.last_seen_at
FROM member_sessions
JOIN members ON members.member_id = member_sessions.member_id
WHERE member_sessions.session_token_hash = @session_token_hash
    AND member_sessions.expires_at > @now
    AND members.status = 'active';

-- name: TouchSession :exec
UPDATE member_sessions
SET last_seen_at = @last_seen_at, expires_at = @expires_at
WHERE session_token_hash = @session_token_hash;

-- name: DeleteSession :exec
DELETE FROM member_sessions
WHERE session_token_hash = @session_token_hash;

-- name: DeleteSessionsForMemberExcept :exec
DELETE FROM member_sessions
WHERE member_id = @member_id
    AND session_token_hash <> @keep_session_token_hash;
