package decisions_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
)

func TestStreamWriterAppendsDecisionSummary(t *testing.T) {
	cacheClient := cachetest.NewClient(t)
	writer := decisions.NewStreamWriter(cacheClient, logging.New(t.Output(), slog.LevelDebug))
	decisionID := uuid.New()
	customerID := uuid.New()
	policyID := uuid.New()
	displayName := "Acme Studio"
	cost := money.Amount(560_000_000)
	createdAt := time.Date(2026, time.September, 26, 10, 0, 0, 123_000_000, time.FixedZone("CEST", 2*60*60))

	writer.Append(t.Context(), httpapi.EnvironmentLive, decisions.StreamEntry{
		DecisionID:          decisionID,
		CreatedAt:           createdAt,
		CustomerID:          customerID,
		CustomerExternalID:  "acme",
		CustomerDisplayName: &displayName,
		Feature:             "text_to_video",
		RequestedModel:      "fal-ai/veo3.1/fast",
		Model:               "fal-ai/kling-video/v2.5-turbo/pro/text-to-video",
		Outcome:             policies.OutcomeRoute,
		Reason:              policies.ReasonPolicyMatched,
		EstimatedCost:       &cost,
		MatchedPolicyID:     &policyID,
	})
	writer.Append(t.Context(), httpapi.EnvironmentLive, decisions.StreamEntry{
		DecisionID:         decisionID,
		CreatedAt:          createdAt,
		CustomerID:         customerID,
		CustomerExternalID: "acme",
		Feature:            "text_to_video",
		RequestedModel:     "acme-video-1",
		Model:              "acme-video-1",
		Outcome:            policies.OutcomeAllow,
		Reason:             policies.ReasonUncostedAllowed,
	})

	entries, err := cacheClient.Redis().XRange(t.Context(), decisions.StreamKey(cacheClient, httpapi.EnvironmentLive), "-", "+").Result()
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("stream entries = %d, want 2", len(entries))
	}
	want := map[string]any{
		"decision_id":           identifiers.Encode(identifiers.PrefixDecision, decisionID),
		"created_at":            "2026-09-26T08:00:00.123Z",
		"customer_id":           identifiers.Encode(identifiers.PrefixCustomer, customerID),
		"customer_external_id":  "acme",
		"customer_display_name": "Acme Studio",
		"feature":               "text_to_video",
		"requested_model":       "fal-ai/veo3.1/fast",
		"model":                 "fal-ai/kling-video/v2.5-turbo/pro/text-to-video",
		"outcome":               "route",
		"reason":                "policy_matched",
		"estimated_cost":        "0.560000000",
		"matched_policy_id":     identifiers.Encode(identifiers.PrefixPolicy, policyID),
	}
	if diff := cmp.Diff(want, entries[0].Values); diff != "" {
		t.Errorf("first entry mismatch (-want +got):\n%s", diff)
	}
	wantEmpty := map[string]any{"customer_display_name": "", "estimated_cost": "", "matched_policy_id": ""}
	for field, value := range wantEmpty {
		if entries[1].Values[field] != value {
			t.Errorf("second entry %s = %q, want %q", field, entries[1].Values[field], value)
		}
	}
	if live := decisions.StreamKey(cacheClient, httpapi.EnvironmentLive); live != cacheClient.Key("decisions", "live") {
		t.Errorf("stream key = %s, want the decisions:live key", live)
	}
}

func TestStreamWriterTrimsToAboutTenThousandEntries(t *testing.T) {
	cacheClient := cachetest.NewClient(t)
	writer := decisions.NewStreamWriter(cacheClient, logging.New(t.Output(), slog.LevelDebug))
	entry := decisions.StreamEntry{DecisionID: uuid.New(), CreatedAt: time.Now(), CustomerID: uuid.New(), Outcome: policies.OutcomeAllow}
	for range 10_200 {
		writer.Append(t.Context(), httpapi.EnvironmentTest, entry)
	}
	length, err := cacheClient.Redis().XLen(t.Context(), decisions.StreamKey(cacheClient, httpapi.EnvironmentTest)).Result()
	if err != nil {
		t.Fatalf("read stream length: %v", err)
	}
	if length < 10_000 || length >= 10_200 {
		t.Errorf("stream length = %d, want trimmed to about 10000", length)
	}
}

func TestStreamWriterLogsFailedAppend(t *testing.T) {
	cacheClient := cachetest.NewClient(t)
	var logs bytes.Buffer
	writer := decisions.NewStreamWriter(cacheClient, logging.New(&logs, slog.LevelDebug))
	decisionID := uuid.New()
	if err := cacheClient.Redis().Set(t.Context(), decisions.StreamKey(cacheClient, httpapi.EnvironmentTest), "not a stream", 0).Err(); err != nil {
		t.Fatalf("occupy stream key: %v", err)
	}

	writer.Append(t.Context(), httpapi.EnvironmentTest, decisions.StreamEntry{DecisionID: decisionID, CreatedAt: time.Now(), CustomerID: uuid.New()})

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &line); err != nil {
		t.Fatalf("decode log line %q: %v", logs.String(), err)
	}
	if line["msg"] != "decisions.stream_append_failed" || line["level"] != "WARN" || line["environment"] != "test" ||
		line["decision_id"] != identifiers.Encode(identifiers.PrefixDecision, decisionID) || !strings.Contains(line["error"].(string), "WRONGTYPE") {
		t.Errorf("log line = %v, want a warn decisions.stream_append_failed with environment, decision_id and the error", line)
	}
}

func TestStreamWriterGivesUpAfterTheDeadline(t *testing.T) {
	cacheClient := cachetest.NewClient(t)
	var logs bytes.Buffer
	writer := decisions.NewStreamWriter(cacheClient, logging.New(&logs, slog.LevelDebug))
	cacheClient.Redis().AddHook(unreachableRedisHook{})

	started := time.Now()
	writer.Append(t.Context(), httpapi.EnvironmentTest, decisions.StreamEntry{DecisionID: uuid.New(), CreatedAt: time.Now(), CustomerID: uuid.New()})
	elapsed := time.Since(started)

	if elapsed >= 200*time.Millisecond || !strings.Contains(logs.String(), `"msg":"decisions.stream_append_failed"`) {
		t.Errorf("append took %s and logged %q, want under 200ms and decisions.stream_append_failed", elapsed, logs.String())
	}
}
