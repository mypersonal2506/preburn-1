package httpapi_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/logging"
)

const testVersion = "1.2.3"

type testServer struct {
	api     *httpapi.API
	mux     *http.ServeMux
	logger  *logging.Logger
	logs    *bytes.Buffer
	handler http.Handler
}

func newTestServer(t *testing.T, authenticator httpapi.Authenticator) *testServer {
	t.Helper()
	logs := &bytes.Buffer{}
	logger := logging.New(logs, slog.LevelDebug)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, testVersion, logger, authenticator)
	handler := httpapi.RequestID(httpapi.SecurityHeaders(httpapi.AccessLog(logger, httpapi.Recover(logger, mux))))
	return &testServer{api: api, mux: mux, logger: logger, logs: logs, handler: handler}
}

func (server *testServer) serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	server.handler.ServeHTTP(recorder, request)
	return recorder
}

func (server *testServer) records(t *testing.T, event logging.Event) []map[string]any {
	t.Helper()
	var matching []map[string]any
	for line := range strings.Lines(server.logs.String()) {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if record["msg"] == string(event) {
			matching = append(matching, record)
		}
	}
	return matching
}

func newRequest(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	return body
}

func assertProblem(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) map[string]any {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d, body %s", recorder.Code, status, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", contentType)
	}
	problem := decodeBody(t, recorder)
	if problem["code"] != code {
		t.Errorf("code = %v, want %s", problem["code"], code)
	}
	if problem["status"] != float64(status) {
		t.Errorf("problem status = %v, want %d", problem["status"], status)
	}
	wantType := "https://github.com/preburn/preburn/blob/main/docs/errors.md#" + code
	if problem["type"] != wantType {
		t.Errorf("type = %v, want %s", problem["type"], wantType)
	}
	if problem["title"] != http.StatusText(status) {
		t.Errorf("title = %v, want %s", problem["title"], http.StatusText(status))
	}
	return problem
}
