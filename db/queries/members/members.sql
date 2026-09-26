-- name: InsertMember :one
INSERT INTO members (member_id, email, display_name, password_hash, status, created_at, updated_at)
VALUES (@member_id, @email, @display_name, @password_hash, 'active', @created_at, @created_at)
ON CONFLICT (email) DO NOTHING
RETURNING member_id, email, display_name, password_hash, status, last_login_at, created_at, updated_at;

-- name: SelectMemberByID :one
SELECT member_id, email, display_name, password_hash, status, last_login_at, created_at, updated_at
FROM members
WHERE member_id = @member_id;

-- name: SelectMemberByIDForUpdate :one
SELECT member_id, email, display_name, password_hash, status, last_login_at, created_at, updated_at
FROM members
WHERE member_id = @member_id
FOR UPDATE;

-- name: SelectSignInMemberForUpdate :one
SELECT member_id, email, display_name, password_hash, status, last_login_at, created_at, updated_at
FROM members
WHERE member_id = @member_id
    AND password_hash = @password_hash
    AND status = 'active'
FOR UPDATE;

-- name: SelectMemberByEmail :one
SELECT member_id, email, display_name, password_hash, status, last_login_at, created_at, updated_at
FROM members
WHERE email = @email;

-- name: UpdateMemberPasswordHash :exec
UPDATE members
SET password_hash = @password_hash, updated_at = @updated_at
WHERE member_id = @member_id;

-- name: UpdateMemberDisplayName :one
UPDATE members
SET display_name = @display_name, updated_at = @updated_at
WHERE member_id = @member_id
RETURNING member_id, email, display_name, password_hash, status, last_login_at, created_at, updated_at;

-- name: UpdateMemberLastLogin :one
UPDATE members
SET last_login_at = @last_login_at
WHERE member_id = @member_id
RETURNING member_id, email, display_name, password_hash, status, last_login_at, created_at, updated_at;
