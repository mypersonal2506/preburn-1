package main

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/secrets"
)

const (
	deniedCustomerID    = "bench-customer-1"
	failingCustomerID   = "bench-customer-2"
	benchTestDuration   = "300ms"
	benchTestCustomers  = "3"
	benchTestWorkers    = "4"
	benchDecisionPrefix = "dec_"
)

var benchTestKey = "pb_test_runtime_" + secrets.RandomBase62(32)

type fakeDecisionAPI struct {
	mutex          sync.Mutex
	checks         map[string]int
	reports        map[string]int
	authorizations map[string]int
	decisions      map[string]string
}

func TestBenchCheckSendsChecksThenReportsAndPrintsLatencies(t *testing.T) {
	api := &fakeDecisionAPI{checks: map[string]int{}, reports: map[string]int{}, authorizations: map[string]int{}, decisions: map[string]string{}}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	var output bytes.Buffer

	err := execute(t.Context(), &output, "bench", "check",
		"--api-url", server.URL,
		"--api-key", benchTestKey,
		"--customers", benchTestCustomers,
		"--duration", benchTestDuration,
		"--concurrency", benchTestWorkers,
	)

	if err != nil {
		t.Fatalf("bench check: %v", err)
	}
	api.mutex.Lock()
	defer api.mutex.Unlock()
	if diff := cmp.Diff([]string{"Bearer " + benchTestKey}, keys(api.authorizations)); diff != "" {
		t.Errorf("authorizations mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"bench-customer-0", deniedCustomerID, failingCustomerID}, keys(api.checks)); diff != "" {
		t.Errorf("checked customers mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"bench-customer-0"}, keys(api.reports)); diff != "" {
		t.Errorf("reported customers mismatch (-want +got):\n%s", diff)
	}
	checkRequests := api.checks["bench-customer-0"] + api.checks[deniedCustomerID] + api.checks[failingCustomerID]
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("output lines = %q, want a header, check, report and error line", lines)
	}
	checkFields := strings.Fields(lines[1])
	reportFields := strings.Fields(lines[2])
	if diff := cmp.Diff([]string{"operation", "requests", "errors", "p50", "p95", "p99"}, strings.Fields(lines[0])); diff != "" {
		t.Errorf("header mismatch (-want +got):\n%s", diff)
	}
	if len(checkFields) != 6 || checkFields[0] != "check" || checkFields[1] != strconv.Itoa(checkRequests) || checkFields[2] != strconv.Itoa(api.checks[failingCustomerID]) {
		t.Errorf("check line = %q, want %d requests and %d errors", lines[1], checkRequests, api.checks[failingCustomerID])
	}
	if len(reportFields) != 6 || reportFields[0] != "report" || reportFields[1] != strconv.Itoa(api.reports["bench-customer-0"]) || reportFields[2] != "0" {
		t.Errorf("report line = %q, want %d requests and no errors", lines[2], api.reports["bench-customer-0"])
	}
	if want := "check errors: " + strconv.Itoa(api.checks[failingCustomerID]) + " with status 503"; lines[3] != want {
		t.Errorf("error line = %q, want %q", lines[3], want)
	}
}

func TestBenchCheckRejectsInvalidFlags(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
	}{
		{name: "missing api url", arguments: []string{"--api-key", benchTestKey}},
		{name: "missing api key", arguments: []string{"--api-url", "http://localhost:8080"}},
		{name: "api url without a host", arguments: []string{"--api-url", "localhost", "--api-key", benchTestKey}},
		{name: "no customers", arguments: []string{"--api-url", "http://localhost:8080", "--api-key", benchTestKey, "--customers", "0"}},
		{name: "no workers", arguments: []string{"--api-url", "http://localhost:8080", "--api-key", benchTestKey, "--concurrency", "0"}},
		{name: "no duration", arguments: []string{"--api-url", "http://localhost:8080", "--api-key", benchTestKey, "--duration", "0s"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := execute(t.Context(), io.Discard, append([]string{"bench", "check"}, test.arguments...)...)

			if code := exitCode(err); code != exitInvalidInput {
				t.Errorf("exit code = %d, err %v, want %d", code, err, exitInvalidInput)
			}
		})
	}
}

func (api *fakeDecisionAPI) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	api.mutex.Lock()
	defer api.mutex.Unlock()
	api.authorizations[request.Header.Get("Authorization")]++
	var body map[string]any
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	switch request.URL.Path {
	case "/api/v1/check":
		customerID := body["customer_id"].(string)
		api.checks[customerID]++
		if customerID == failingCustomerID {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		outcome := "allow"
		if customerID == deniedCustomerID {
			outcome = "deny"
		}
		decisionID := benchDecisionPrefix + strconv.Itoa(len(api.decisions))
		api.decisions[decisionID] = customerID
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"decision_id": decisionID, "outcome": outcome})
	case "/api/v1/report":
		api.reports[api.decisions[body["decision_id"].(string)]]++
		writer.WriteHeader(http.StatusAccepted)
	default:
		http.NotFound(writer, request)
	}
}

func keys(counts map[string]int) []string {
	return slices.Sorted(maps.Keys(counts))
}
