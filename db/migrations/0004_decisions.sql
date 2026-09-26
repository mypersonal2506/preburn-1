-- +goose Up
CREATE TABLE policies (
    policy_id uuid PRIMARY KEY,
    environment environment NOT NULL,
    name text NOT NULL,
    level text NOT NULL CHECK (level IN ('everyone', 'plan', 'customer')),
    plan_id uuid NULL REFERENCES plans (plan_id),
    customer_id uuid NULL REFERENCES customers (customer_id),
    feature text NULL,
    condition_group jsonb NOT NULL CHECK (jsonb_typeof(condition_group) = 'object'),
    action jsonb NOT NULL CHECK (jsonb_typeof(action) = 'object'),
    enforcement text NOT NULL CHECK (enforcement IN ('soft', 'hard')),
    on_unreachable text NOT NULL CHECK (on_unreachable IN ('allow', 'deny')),
    on_uncosted text NOT NULL CHECK (on_uncosted IN ('allow', 'deny')),
    status record_status NOT NULL,
    version integer NOT NULL CHECK (version >= 1),
    import_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT policies_level_plan_id_check CHECK ((level = 'plan') = (plan_id IS NOT NULL)),
    CONSTRAINT policies_level_customer_id_check CHECK ((level = 'customer') = (customer_id IS NOT NULL))
);

CREATE TABLE decisions (
    decision_id uuid PRIMARY KEY,
    environment environment NOT NULL,
    customer_id uuid NOT NULL REFERENCES customers (customer_id),
    customer_user_id uuid NULL,
    feature text NOT NULL,
    requested_provider text NOT NULL,
    requested_model text NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    attributes jsonb NOT NULL CHECK (jsonb_typeof(attributes) = 'object'),
    overrides jsonb NOT NULL CHECK (jsonb_typeof(overrides) = 'object'),
    outcome text NOT NULL CHECK (outcome IN ('allow', 'route', 'cap', 'deny')),
    reason text NOT NULL CHECK (reason IN (
        'no_policy_matched',
        'policy_matched',
        'hard_limit_reached',
        'route_chain_exhausted',
        'uncosted_allowed',
        'uncosted_denied',
        'cap_not_applicable'
    )),
    matched_policy_id uuid NULL,
    matched_policy_version integer NULL CHECK (matched_policy_version >= 1),
    signals jsonb NOT NULL CHECK (jsonb_typeof(signals) = 'object'),
    requested_estimated_cost_nanos bigint NULL CHECK (requested_estimated_cost_nanos >= 0),
    estimated_cost_nanos bigint NULL CHECK (estimated_cost_nanos >= 0),
    reserved_nanos bigint NOT NULL CHECK (reserved_nanos >= 0),
    estimate_basis text NOT NULL CHECK (estimate_basis IN ('ceiling', 'p95', 'request_estimate', 'none')),
    status text NOT NULL CHECK (status IN ('reserved', 'settled', 'released', 'expired', 'unreserved')),
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    settled_at timestamptz NULL,
    import_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT decisions_period_check CHECK (period_end > period_start)
);

CREATE INDEX decisions_environment_created_at_index ON decisions (environment, created_at DESC);

CREATE INDEX decisions_environment_customer_id_created_at_index ON decisions (environment, customer_id, created_at DESC);

CREATE INDEX decisions_environment_matched_policy_id_created_at_index ON decisions (environment, matched_policy_id, created_at DESC);

CREATE INDEX decisions_environment_customer_id_period_start_index ON decisions (environment, customer_id, period_start);

CREATE INDEX decisions_reserved_expires_at_index ON decisions (expires_at) WHERE status = 'reserved';

CREATE INDEX decisions_freed_environment_expires_at_index ON decisions (environment, expires_at) WHERE status IN ('released', 'expired');

CREATE TABLE ledger_entries (
    ledger_entry_id uuid PRIMARY KEY,
    environment environment NOT NULL,
    customer_id uuid NOT NULL REFERENCES customers (customer_id),
    customer_user_id uuid NULL,
    -- No foreign key: decision retention deletes decisions that ledger entries still name.
    decision_id uuid NULL,
    idempotency_key text NOT NULL,
    feature text NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    attributes jsonb NOT NULL CHECK (jsonb_typeof(attributes) = 'object'),
    usage jsonb NOT NULL CHECK (jsonb_typeof(usage) = 'object'),
    cost_nanos bigint NULL CHECK (cost_nanos >= 0),
    cost_breakdown jsonb NOT NULL,
    cost_status text NOT NULL CHECK (cost_status IN ('costed', 'uncosted')),
    decision_source text NOT NULL CHECK (decision_source IN ('server', 'fallback')),
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    correction_of uuid NULL,
    occurred_at timestamptz NOT NULL,
    import_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entries_uncosted_cost_check CHECK ((cost_nanos IS NULL) = (cost_status = 'uncosted')),
    CONSTRAINT ledger_entries_period_check CHECK (period_end > period_start),
    CONSTRAINT ledger_entries_environment_idempotency_key_key UNIQUE (environment, idempotency_key)
);

CREATE INDEX ledger_entries_environment_customer_id_period_start_index ON ledger_entries (environment, customer_id, period_start);

CREATE INDEX ledger_entries_environment_occurred_at_index ON ledger_entries (environment, occurred_at DESC);

CREATE INDEX ledger_entries_environment_created_at_index ON ledger_entries (environment, created_at);

CREATE INDEX ledger_entries_uncosted_environment_provider_model_index ON ledger_entries (environment, provider, model) WHERE cost_status = 'uncosted';

CREATE INDEX ledger_entries_uncosted_environment_ledger_entry_id_index ON ledger_entries (environment, ledger_entry_id) WHERE cost_status = 'uncosted';

CREATE TABLE revenue_entries (
    revenue_entry_id uuid PRIMARY KEY,
    environment environment NOT NULL,
    customer_id uuid NOT NULL REFERENCES customers (customer_id),
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    kind text NOT NULL CHECK (kind IN ('subscription', 'adjustment', 'stripe_fee', 'refund', 'credit_note')),
    amount_nanos bigint NOT NULL CHECK (amount_nanos >= 0),
    source text NOT NULL CHECK (source IN ('api', 'stripe', 'import')),
    source_reference text NOT NULL,
    occurred_at timestamptz NOT NULL,
    import_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT revenue_entries_period_check CHECK (period_end >= period_start),
    CONSTRAINT revenue_entries_environment_source_kind_source_reference_key UNIQUE (environment, source, kind, source_reference)
);

CREATE INDEX revenue_entries_environment_customer_id_period_start_index ON revenue_entries (environment, customer_id, period_start);

CREATE INDEX revenue_entries_environment_created_at_revenue_entry_id_index ON revenue_entries (environment, created_at, revenue_entry_id);

CREATE INDEX revenue_entries_environment_occurred_at_index ON revenue_entries (environment, occurred_at);

CREATE TABLE period_rollups (
    environment environment NOT NULL,
    customer_id uuid NOT NULL,
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    revenue_net_nanos bigint NOT NULL,
    cost_nanos bigint NOT NULL CHECK (cost_nanos >= 0),
    uncosted_count integer NOT NULL CHECK (uncosted_count >= 0),
    decision_counts jsonb NOT NULL CHECK (jsonb_typeof(decision_counts) = 'object'),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (environment, customer_id, period_start),
    CONSTRAINT period_rollups_period_check CHECK (period_end > period_start)
);

CREATE INDEX period_rollups_environment_period_end_index ON period_rollups (environment, period_end);

CREATE TABLE usage_estimates (
    environment environment NOT NULL,
    feature text NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    meter text NOT NULL,
    p95_quantity_micros bigint NOT NULL CHECK (p95_quantity_micros >= 0),
    sample_count integer NOT NULL CHECK (sample_count >= 0),
    refreshed_at timestamptz NOT NULL,
    PRIMARY KEY (environment, feature, provider, model, meter)
);

-- +goose Down
DROP TABLE usage_estimates;

DROP TABLE period_rollups;

DROP TABLE revenue_entries;

DROP TABLE ledger_entries;

DROP TABLE decisions;

DROP TABLE policies;
