package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/secrets"
	"github.com/preburn/preburn/internal/tracing"
)

const (
	testRedisURLVariable = "PREBURN_TEST_REDIS_URL"
	waitTimeout          = 10 * time.Second
	pollInterval         = 20 * time.Millisecond
)

type lockedBuffer struct {
	mutex    sync.Mutex
	contents bytes.Buffer
}

func TestConfigurationErrorsAreInvalidInput(t *testing.T) {
	for _, variable := range []string{"PREBURN_DATABASE_URL", "PREBURN_REDIS_URL", "PREBURN_SECRET_KEY"} {
		t.Setenv(variable, "")
	}
	for _, command := range []string{"serve", "worker", "migrate"} {
		t.Run(command, func(t *testing.T) {
			err := execute(t.Context(), io.Discard, command)

			if code := exitCode(err); code != exitInvalidInput {
				t.Errorf("exit code = %d, want %d, error %v", code, exitInvalidInput, err)
			}
			if err != nil && !strings.Contains(err.Error(), "PREBURN_DATABASE_URL is required") {
				t.Errorf("error = %v, want it to name PREBURN_DATABASE_URL", err)
			}
		})
	}
}

func TestUnsupportedTracingProtocolIsInvalidInput(t *testing.T) {
	setEnvironment(t, "postgres://preburn@127.0.0.1:1/unreachable")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "carrier_pigeon")
	for _, command := range []string{"serve", "worker"} {
		t.Run(command, func(t *testing.T) {
			err := execute(t.Context(), io.Discard, command)

			if code := exitCode(err); code != exitInvalidInput {
				t.Errorf("exit code = %d, want %d, error %v", code, exitInvalidInput, err)
			}
			if !errors.Is(err, tracing.ErrUnsupportedProtocol) {
				t.Errorf("error = %v, want tracing.ErrUnsupportedProtocol", err)
			}
		})
	}
}

func TestServeAnswersUntilCancelled(t *testing.T) {
	setEnvironment(t, databasetest.NewPool(t).Config().ConnString())
	output := &lockedBuffer{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- execute(ctx, output, "serve")
	}()

	waitForEvent(t, output, finished, logging.ServerStarted)
	address := output.records(t, logging.ServerStarted)[0]["address"].(string)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/healthz", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz status = %d, want 200", response.StatusCode)
	}
	cancel()

	if err := receive(t, finished); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if records := output.records(t, logging.ServerStopped); len(records) != 1 {
		t.Errorf("logged server.stopped %d times, want 1", len(records))
	}
}

func TestWorkerWorksUntilCancelled(t *testing.T) {
	setEnvironment(t, databasetest.NewPool(t).Config().ConnString())
	output := &lockedBuffer{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- execute(ctx, output, "worker")
	}()

	waitForEvent(t, output, finished, logging.WorkerStarted)
	cancel()

	if err := receive(t, finished); err != nil {
		t.Fatalf("worker: %v", err)
	}
}

func setEnvironment(t *testing.T, databaseURL string) {
	t.Helper()
	redisURL := os.Getenv(testRedisURLVariable)
	if redisURL == "" {
		t.Fatalf("%s is not set, run the tests with make test-go", testRedisURLVariable)
	}
	t.Setenv("PREBURN_DATABASE_URL", databaseURL)
	t.Setenv("PREBURN_DATABASE_MAXIMUM_CONNECTIONS", "4")
	t.Setenv("PREBURN_REDIS_URL", redisURL)
	t.Setenv("PREBURN_REDIS_KEY_PREFIX", "test:"+rand.Text()+":")
	t.Setenv("PREBURN_SECRET_KEY", secrets.NewSecretKey())
	t.Setenv("PREBURN_HTTP_ADDRESS", "127.0.0.1:0")
	t.Setenv("PREBURN_METRICS_ADDRESS", "127.0.0.1:0")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
}

func execute(ctx context.Context, output io.Writer, arguments ...string) error {
	return executeWithInput(ctx, "", output, arguments...)
}

func waitForEvent(t *testing.T, output *lockedBuffer, finished <-chan error, event logging.Event) {
	t.Helper()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	deadline := time.After(waitTimeout)
	for len(output.records(t, event)) == 0 {
		select {
		case err := <-finished:
			t.Fatalf("command returned before logging %s: %v", event, err)
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("%s not logged within %s", event, waitTimeout)
		}
	}
}

func receive(t *testing.T, finished <-chan error) error {
	t.Helper()
	select {
	case err := <-finished:
		return err
	case <-time.After(waitTimeout):
	}
	t.Fatalf("command did not return within %s", waitTimeout)
	return nil
}

func (buffer *lockedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.contents.Write(data)
}

func (buffer *lockedBuffer) records(t *testing.T, event logging.Event) []map[string]any {
	t.Helper()
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	var matching []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(buffer.contents.Bytes()))
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("parse log line %q: %v", scanner.Text(), err)
		}
		if record["msg"] == string(event) {
			matching = append(matching, record)
		}
	}
	return matching
}
