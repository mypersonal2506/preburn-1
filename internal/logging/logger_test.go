package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/logging"
)

const testEvent logging.Event = "logging.test_event"

const secretValue = "value-that-must-not-leak"

func TestLoggerWritesEventAsJSON(t *testing.T) {
	var output bytes.Buffer
	logger := logging.New(&output, slog.LevelDebug)
	twoHoursEast := time.FixedZone("UTC+2", 2*60*60)

	logger.Info(t.Context(), testEvent,
		slog.String("customer_id", "cus_1"),
		slog.Int("attempt_count", 2),
		slog.Time("expires_at", time.Date(2026, 9, 26, 12, 30, 0, 0, twoHoursEast)),
	)

	record := decodeRecord(t, output.Bytes())
	recordTime, isString := record["time"].(string)
	if !isString {
		t.Fatalf("time = %v, want a string", record["time"])
	}
	if _, err := time.Parse(time.RFC3339, recordTime); err != nil {
		t.Errorf("time %q is not RFC 3339: %v", recordTime, err)
	}
	if !strings.HasSuffix(recordTime, "Z") {
		t.Errorf("time %q is not UTC", recordTime)
	}
	delete(record, "time")

	want := map[string]any{
		"level":         "INFO",
		"msg":           "logging.test_event",
		"customer_id":   "cus_1",
		"attempt_count": float64(2),
		"expires_at":    "2026-09-26T10:30:00Z",
	}
	if diff := cmp.Diff(want, record); diff != "" {
		t.Errorf("record mismatch (-want +got):\n%s", diff)
	}
}

func TestLoggerLevels(t *testing.T) {
	tests := []struct {
		name string
		log  func(*logging.Logger, context.Context, logging.Event, ...slog.Attr)
		want string
	}{
		{name: "debug", log: (*logging.Logger).Debug, want: "DEBUG"},
		{name: "info", log: (*logging.Logger).Info, want: "INFO"},
		{name: "warn", log: (*logging.Logger).Warn, want: "WARN"},
		{name: "error", log: (*logging.Logger).Error, want: "ERROR"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer

			test.log(logging.New(&output, slog.LevelDebug), t.Context(), testEvent)

			if got := decodeRecord(t, output.Bytes())["level"]; got != test.want {
				t.Errorf("level = %v, want %s", got, test.want)
			}
		})
	}
}

func TestLoggerDropsEventsBelowLevel(t *testing.T) {
	var output bytes.Buffer
	logger := logging.New(&output, slog.LevelWarn)

	logger.Debug(t.Context(), testEvent)
	logger.Info(t.Context(), testEvent)

	if output.Len() != 0 {
		t.Errorf("output = %q, want nothing below the warn level", output.String())
	}
}

func TestLoggerAddsRequestIDFromContext(t *testing.T) {
	var output bytes.Buffer
	logger := logging.New(&output, slog.LevelDebug)

	logger.Info(logging.WithRequestID(t.Context(), "req_1"), testEvent)

	if got := decodeRecord(t, output.Bytes())["request_id"]; got != "req_1" {
		t.Errorf("request_id = %v, want req_1", got)
	}
}

func TestLoggerOmitsRequestIDWithoutOne(t *testing.T) {
	var output bytes.Buffer
	logger := logging.New(&output, slog.LevelDebug)

	logger.Info(t.Context(), testEvent)

	if got, found := decodeRecord(t, output.Bytes())["request_id"]; found {
		t.Errorf("request_id = %v, want no request_id", got)
	}
}

func TestLoggerRedactsSensitiveAttributes(t *testing.T) {
	tests := []struct {
		name      string
		attribute slog.Attr
		want      map[string]any
	}{
		{
			name:      "password",
			attribute: slog.String("password", secretValue),
			want:      map[string]any{"password": "[redacted]"},
		},
		{
			name:      "key ending in password",
			attribute: slog.String("admin_password", secretValue),
			want:      map[string]any{"admin_password": "[redacted]"},
		},
		{
			name:      "secret",
			attribute: slog.String("client_secret", secretValue),
			want:      map[string]any{"client_secret": "[redacted]"},
		},
		{
			name:      "token",
			attribute: slog.String("session_token", secretValue),
			want:      map[string]any{"session_token": "[redacted]"},
		},
		{
			name:      "api key",
			attribute: slog.String("stripe_api_key", secretValue),
			want:      map[string]any{"stripe_api_key": "[redacted]"},
		},
		{
			name:      "upper case key",
			attribute: slog.Attr{Key: "API_KEY", Value: slog.StringValue(secretValue)},
			want:      map[string]any{"API_KEY": "[redacted]"},
		},
		{
			name:      "structured value",
			attribute: slog.Any("secret", struct{ Value string }{Value: secretValue}),
			want:      map[string]any{"secret": "[redacted]"},
		},
		{
			name: "key inside a group",
			attribute: slog.Group("stripe",
				slog.String("api_key", secretValue),
				slog.String("account_id", "acct_1"),
			),
			want: map[string]any{"stripe": map[string]any{"api_key": "[redacted]", "account_id": "acct_1"}},
		},
		{
			name:      "secret key",
			attribute: slog.String("secret_key", secretValue),
			want:      map[string]any{"secret_key": "[redacted]"},
		},
		{
			name:      "sensitive group",
			attribute: slog.Group("session_token", slog.String("value", secretValue)),
			want:      map[string]any{"session_token": map[string]any{"value": "[redacted]"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer

			logging.New(&output, slog.LevelDebug).Info(t.Context(), testEvent, test.attribute)

			if strings.Contains(output.String(), secretValue) {
				t.Fatalf("output contains the secret value: %s", output.String())
			}
			record := decodeRecord(t, output.Bytes())
			for _, builtInKey := range []string{slog.TimeKey, slog.LevelKey, slog.MessageKey} {
				delete(record, builtInKey)
			}
			if diff := cmp.Diff(test.want, record); diff != "" {
				t.Errorf("attributes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLoggerKeepsUsageQuantityAttributes(t *testing.T) {
	var output bytes.Buffer

	logging.New(&output, slog.LevelDebug).Info(t.Context(), testEvent,
		slog.String("input_tokens", "1200"),
		slog.String("output_tokens", "300"),
		slog.String("tokens_per_second", "42"),
	)

	record := decodeRecord(t, output.Bytes())
	for _, builtInKey := range []string{slog.TimeKey, slog.LevelKey, slog.MessageKey} {
		delete(record, builtInKey)
	}
	want := map[string]any{"input_tokens": "1200", "output_tokens": "300", "tokens_per_second": "42"}
	if diff := cmp.Diff(want, record); diff != "" {
		t.Errorf("attributes mismatch (-want +got):\n%s", diff)
	}
}

func decodeRecord(t *testing.T, output []byte) map[string]any {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal(output, &record); err != nil {
		t.Fatalf("decode one JSON record from %q: %v", output, err)
	}
	return record
}
