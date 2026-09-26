-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TYPE environment AS ENUM ('test', 'live');

CREATE TYPE record_status AS ENUM ('active', 'disabled', 'archived');

CREATE TABLE installation (
    installation_id smallint PRIMARY KEY CHECK (installation_id = 1),
    name text NOT NULL DEFAULT 'Preburn',
    setup_token_hash bytea NULL,
    setup_completed_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO installation (installation_id) VALUES (1);

CREATE TABLE environment_settings (
    environment environment PRIMARY KEY,
    default_plan_id uuid NULL,
    stripe_customer_metadata_key text NOT NULL DEFAULT 'preburn_customer_id',
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO environment_settings (environment) VALUES ('test'), ('live');

-- +goose Down
DROP TABLE environment_settings;

DROP TABLE installation;

DROP TYPE record_status;

DROP TYPE environment;

DROP EXTENSION citext;
