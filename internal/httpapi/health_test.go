package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/logging"
)

func TestHealthzReturns200(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	failing := httpapi.ReadinessCheck{Name: "database", Check: func(context.Context) error {
		return errors.New("database down")
	}}
	httpapi.RegisterHealthRoutes(server.mux, server.logger, []httpapi.ReadinessCheck{failing})

	recorder := server.serve(newRequest(t, http.MethodGet, "/healthz", ""))

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", recorder.Code)
	}
}

func TestReadyz(t *testing.T) {
	passing := func(context.Context) error { return nil }
	failing := func(context.Context) error { return errors.New("connection refused") }
	tests := []struct {
		name        string
		checks      []httpapi.ReadinessCheck
		status      int
		wantBody    map[string]any
		failedCount int
	}{
		{
			name:     "every check passes",
			checks:   []httpapi.ReadinessCheck{{Name: "database", Check: passing}, {Name: "valkey", Check: passing}},
			status:   http.StatusOK,
			wantBody: map[string]any{"status": "ready", "failing_checks": []any{}},
		},
		{
			name:        "one check fails",
			checks:      []httpapi.ReadinessCheck{{Name: "database", Check: passing}, {Name: "valkey", Check: failing}},
			status:      http.StatusServiceUnavailable,
			wantBody:    map[string]any{"status": "not_ready", "failing_checks": []any{"valkey"}},
			failedCount: 1,
		},
		{
			name:        "every check fails",
			checks:      []httpapi.ReadinessCheck{{Name: "database", Check: failing}, {Name: "valkey", Check: failing}},
			status:      http.StatusServiceUnavailable,
			wantBody:    map[string]any{"status": "not_ready", "failing_checks": []any{"database", "valkey"}},
			failedCount: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, httpapi.RejectingAuthenticator{})
			httpapi.RegisterHealthRoutes(server.mux, server.logger, test.checks)

			recorder := server.serve(newRequest(t, http.MethodGet, "/readyz", ""))

			if recorder.Code != test.status {
				t.Errorf("status = %d, want %d", recorder.Code, test.status)
			}
			if diff := cmp.Diff(test.wantBody, decodeBody(t, recorder)); diff != "" {
				t.Errorf("body mismatch (-want +got):\n%s", diff)
			}
			if records := server.records(t, logging.HTTPReadinessCheckFailed); len(records) != test.failedCount {
				t.Errorf("logged %d failed checks, want %d", len(records), test.failedCount)
			}
		})
	}
}

func TestReadinessChecksRunWithTimeout(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	var mutex sync.Mutex
	remaining := map[string]time.Duration{}
	recordDeadline := func(name string) func(context.Context) error {
		return func(ctx context.Context) error {
			deadline, hasDeadline := ctx.Deadline()
			if !hasDeadline {
				return errors.New("no deadline")
			}
			mutex.Lock()
			defer mutex.Unlock()
			remaining[name] = time.Until(deadline)
			return nil
		}
	}
	httpapi.RegisterHealthRoutes(server.mux, server.logger, []httpapi.ReadinessCheck{
		{Name: "database", Check: recordDeadline("database")},
		{Name: "valkey", Check: recordDeadline("valkey")},
	})

	recorder := server.serve(newRequest(t, http.MethodGet, "/readyz", ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	for _, name := range []string{"database", "valkey"} {
		if remaining[name] <= 0 || remaining[name] > 2*time.Second {
			t.Errorf("check %s had %v until its deadline, want at most 2s", name, remaining[name])
		}
	}
}
