package metrics_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/preburn/preburn/internal/metrics"
)

const (
	serveDeadline     = 5 * time.Second
	readyPollInterval = 10 * time.Millisecond
)

func TestServeAnswersMetricsUntilContextEnds(t *testing.T) {
	address := unusedAddress(t)
	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() {
		served <- metrics.Serve(ctx, address, metrics.NewRegistry())
	}()

	body := getWhenListening(t, "http://"+address+"/metrics")
	for _, metricName := range []string{"go_goroutines", "process_start_time_seconds"} {
		if !strings.Contains(body, metricName) {
			t.Errorf("/metrics body has no %s", metricName)
		}
	}

	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve returned %v after the context ended, want nil", err)
		}
	case <-time.After(serveDeadline):
		t.Fatalf("Serve still running %s after the context ended", serveDeadline)
	}
	if response, err := get(t, "http://"+address+"/metrics"); err == nil {
		closeBody(t, response)
		t.Fatalf("GET /metrics answered %s after Serve returned", response.Status)
	}
}

func TestServeReturnsListenError(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
	})

	err = metrics.Serve(t.Context(), listener.Addr().String(), metrics.NewRegistry())
	var operationError *net.OpError
	if !errors.As(err, &operationError) || operationError.Op != "listen" {
		t.Fatalf("Serve on a taken address returned %v, want a listen error", err)
	}
}

func unusedAddress(t *testing.T) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return address
}

func getWhenListening(t *testing.T, url string) string {
	t.Helper()
	deadline := time.Now().Add(serveDeadline)
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()
	for {
		response, err := get(t, url)
		if err == nil {
			defer closeBody(t, response)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET %s status = %s, want 200", url, response.Status)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read %s: %v", url, err)
			}
			return string(body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s still failing after %s: %v", url, serveDeadline, err)
		}
		<-ticker.C
	}
}

func get(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return http.DefaultClient.Do(request)
}

func closeBody(t *testing.T, response *http.Response) {
	t.Helper()
	if err := response.Body.Close(); err != nil {
		t.Errorf("close body: %v", err)
	}
}
