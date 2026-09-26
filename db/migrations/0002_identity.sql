-- +goose Up
CREATE TABLE members (
    member_id uuid PRIMARY KEY,
    email citext NOT NULL,
    display_name text NOT NULL,
    password_hash text NULL,
    status record_status NOT NULL,
    last_login_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT members_email_key UNIQUE (email)
);

CREATE TABLE member_sessions (
    session_token_hash bytea PRIMARY KEY,
    member_id uuid NOT NULL REFERENCES members (member_id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    user_agent text NOT NULL,
    ip_address inet NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX member_sessions_member_id_index ON member_sessions (member_id);

CREATE TABLE member_links (
    member_link_id uuid PRIMARY KEY,
    member_id uuid NOT NULL REFERENCES members (member_id) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('invite', 'password_reset')),
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz NULL,
    created_by_member_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX member_links_member_id_purpose_index ON member_links (member_id, purpose);

CREATE TABLE api_keys (
    api_key_id uuid PRIMARY KEY,
    environment environment NOT NULL,
    name text NOT NULL,
    scope text NOT NULL CHECK (scope IN ('runtime', 'admin')),
    secret_hash bytea NOT NULL UNIQUE,
    secret_last_four text NOT NULL,
    status record_status NOT NULL,
    last_used_at timestamptz NULL,
    created_by_member_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX api_keys_environment_status_index ON api_keys (environment, status);

-- +goose Down
DROP TABLE api_keys;

DROP TABLE member_links;

DROP TABLE member_sessions;

DROP TABLE members;
