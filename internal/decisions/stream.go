package decisions

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
)

const (
	streamKeyName       = "decisions"
	streamMaximumLength = 10000
	streamAppendTimeout = 150 * time.Millisecond
)

// StreamEntry is the summary of one decision in the decision stream, which
// the dashboard relays as decision events.
type StreamEntry struct {
	// DecisionID is the decision's UUID.
	DecisionID uuid.UUID
	// CreatedAt is when the check decided.
	CreatedAt time.Time
	// CustomerID is the customer's UUID.
	CustomerID uuid.UUID
	// CustomerExternalID is the customer's id in the operator's system.
	CustomerExternalID string
	// CustomerDisplayName is the customer's display name, or nil.
	CustomerDisplayName *string
	// Feature is the checked feature.
	Feature string
	// RequestedModel is the model the check asked for.
	RequestedModel string
	// Model is the model the decision runs, which a route changes.
	Model string
	// Outcome is the decided outcome.
	Outcome policies.Outcome
	// Reason is why the check decided the outcome.
	Reason policies.Reason
	// EstimatedCost is the rated cost of the request the decision runs, or
	// nil when it is uncosted.
	EstimatedCost *money.Amount
	// MatchedPolicyID is the policy that decided, or nil.
	MatchedPolicyID *uuid.UUID
}

// StreamWriter appends decision summaries to the decision stream of each
// environment. Create one with NewStreamWriter. It is safe for concurrent
// use.
type StreamWriter struct {
	cache  *cache.Client
	logger *logging.Logger
}

// NewStreamWriter returns a StreamWriter that appends to streams in
// cacheClient and logs failed appends to logger.
func NewStreamWriter(cacheClient *cache.Client, logger *logging.Logger) *StreamWriter {
	return &StreamWriter{cache: cacheClient, logger: logger}
}

// StreamKey returns the key of the decision stream of environment in
// cacheClient: decisions:{environment}, after the key prefix.
func StreamKey(cacheClient *cache.Client, environment httpapi.Environment) string {
	return cacheClient.Key(streamKeyName, string(environment))
}

// Append adds entry to the decision stream of environment with XADD, and
// trims the stream to about 10000 entries. Ids are written with their
// prefixes, such as dec_ and cust_, CreatedAt in RFC 3339 UTC with
// nanoseconds, the estimated cost as an amount string, and a nil display
// name, cost or policy as an empty string. An append that fails or takes
// more than 150 ms logs decisions.stream_append_failed and returns, because
// the stream only feeds the dashboard.
func (writer *StreamWriter) Append(ctx context.Context, environment httpapi.Environment, entry StreamEntry) {
	appendContext, cancel := context.WithTimeout(ctx, streamAppendTimeout)
	defer cancel()
	estimatedCost := ""
	if entry.EstimatedCost != nil {
		estimatedCost = money.FormatAmount(*entry.EstimatedCost)
	}
	matchedPolicyID := ""
	if entry.MatchedPolicyID != nil {
		matchedPolicyID = identifiers.Encode(identifiers.PrefixPolicy, *entry.MatchedPolicyID)
	}
	displayName := ""
	if entry.CustomerDisplayName != nil {
		displayName = *entry.CustomerDisplayName
	}
	err := writer.cache.Redis().XAdd(appendContext, &redis.XAddArgs{
		Stream: StreamKey(writer.cache, environment),
		MaxLen: streamMaximumLength,
		Approx: true,
		Values: []any{
			"decision_id", identifiers.Encode(identifiers.PrefixDecision, entry.DecisionID),
			"created_at", entry.CreatedAt.UTC().Format(time.RFC3339Nano),
			"customer_id", identifiers.Encode(identifiers.PrefixCustomer, entry.CustomerID),
			"customer_external_id", entry.CustomerExternalID,
			"customer_display_name", displayName,
			"feature", entry.Feature,
			"requested_model", entry.RequestedModel,
			"model", entry.Model,
			"outcome", string(entry.Outcome),
			"reason", string(entry.Reason),
			"estimated_cost", estimatedCost,
			"matched_policy_id", matchedPolicyID,
		},
	}).Err()
	if err != nil {
		writer.logger.Warn(ctx, logging.DecisionsStreamAppendFailed,
			slog.String("environment", string(environment)),
			slog.String("decision_id", identifiers.Encode(identifiers.PrefixDecision, entry.DecisionID)),
			slog.String("error", err.Error()),
		)
	}
}
