-- name: InsertDecision :exec
INSERT INTO decisions (
    decision_id,
    environment,
    customer_id,
    customer_user_id,
    feature,
    requested_provider,
    requested_model,
    provider,
    model,
    attributes,
    overrides,
    outcome,
    reason,
    matched_policy_id,
    matched_policy_version,
    signals,
    requested_estimated_cost_nanos,
    estimated_cost_nanos,
    reserved_nanos,
    estimate_basis,
    status,
    period_start,
    period_end,
    expires_at,
    created_at
) VALUES (
    @decision_id,
    @environment,
    @customer_id,
    @customer_user_id,
    @feature,
    @requested_provider,
    @requested_model,
    @provider,
    @model,
    @attributes,
    @overrides,
    @outcome,
    @reason,
    @matched_policy_id,
    @matched_policy_version,
    @signals,
    @requested_estimated_cost_nanos,
    @estimated_cost_nanos,
    @reserved_nanos,
    @estimate_basis,
    @status,
    @period_start,
    @period_end,
    @expires_at,
    @created_at
);

-- name: ListFeatureUsageEstimates :many
SELECT provider, model, meter, p95_quantity_micros, sample_count
FROM usage_estimates
WHERE environment = @environment
    AND feature = @feature;
