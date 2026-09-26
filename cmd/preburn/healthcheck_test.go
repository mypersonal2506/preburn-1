package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthcheckExitCodeForAnswer(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   int
	}{
		{name: "ok", status: http.StatusOK, want: exitSuccess},
		{name: "no content", status: http.StatusNoContent, want: exitSuccess},
		{name: "not found", status: http.StatusNotFound, want: exitFailure},
		{name: "service unavailable", status: http.StatusServiceUnavailable, want: exitFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
			}))
			defer server.Close()

			if code := exitCode(execute(t.Context(), io.Discard, "healthcheck", "--url", server.URL+"/readyz")); code != test.want {
				t.Errorf("exit code = %d, want %d", code, test.want)
			}
		})
	}
}

func TestHealthcheckSendsGetToPath(t *testing.T) {
	var method, path string
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		method, path = request.Method, request.URL.Path
	}))
	defer server.Close()

	if err := execute(t.Context(), io.Discard, "healthcheck", "--url", server.URL+"/readyz"); err != nil {
		t.Fatalf("healthcheck: %v", err)
	}

	if method != http.MethodGet || path != "/readyz" {
		t.Errorf("request = %s %s, want GET /readyz", method, path)
	}
}

func TestHealthcheckFailsWhenUnreachable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	unreachableURL := server.URL + "/readyz"
	server.Close()

	if code := exitCode(execute(t.Context(), io.Discard, "healthcheck", "--url", unreachableURL)); code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
}

func TestHealthcheckFailsAfterTimeout(t *testing.T) {
	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
		case <-released:
		}
	}))
	defer server.Close()
	defer close(released)

	started := time.Now()
	code := exitCode(execute(t.Context(), io.Discard, "healthcheck", "--url", server.URL+"/readyz"))
	elapsed := time.Since(started)

	if code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
	if elapsed < healthcheckTimeout || elapsed > healthcheckTimeout+time.Second {
		t.Errorf("returned after %s, want the %s timeout", elapsed, healthcheckTimeout)
	}
}

func TestHealthcheckInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
	}{
		{name: "missing url", arguments: []string{"healthcheck"}},
		{name: "url without scheme", arguments: []string{"healthcheck", "--url", "127.0.0.1:8080/readyz"}},
		{name: "url with other scheme", arguments: []string{"healthcheck", "--url", "ftp://127.0.0.1/readyz"}},
		{name: "url without host", arguments: []string{"healthcheck", "--url", "http:///readyz"}},
		{name: "extra argument", arguments: []string{"healthcheck", "--url", "http://127.0.0.1:8080/readyz", "extra"}},
		{name: "unknown flag", arguments: []string{"healthcheck", "--unknown"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if code := exitCode(execute(t.Context(), io.Discard, test.arguments...)); code != exitInvalidInput {
				t.Errorf("exit code = %d, want %d", code, exitInvalidInput)
			}
		})
	}
}

func TestHealthcheckIsHiddenFromHelp(t *testing.T) {
	var output bytes.Buffer

	if err := execute(t.Context(), &output, "--help"); err != nil {
		t.Fatalf("help: %v", err)
	}

	if strings.Contains(output.String(), "healthcheck") {
		t.Errorf("help lists healthcheck:\n%s", output.String())
	}
}
