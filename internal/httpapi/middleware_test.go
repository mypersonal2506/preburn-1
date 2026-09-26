package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/logging"
)

const dashboardContentSecurityPolicy = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'"

func TestRequestIDIsEchoedOrGenerated(t *testing.T) {
	tests := []struct {
		name     string
		incoming string
		echoed   bool
	}{
		{name: "incoming id echoed", incoming: "edge-7f3a.01:2", echoed: true},
		{name: "id of 128 characters echoed", incoming: strings.Repeat("a", 128), echoed: true},
		{name: "missing id generated"},
		{name: "id with spaces replaced", incoming: "two words"},
		{name: "id above 128 characters replaced", incoming: strings.Repeat("a", 129)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, httpapi.RejectingAuthenticator{})
			request := newRequest(t, http.MethodGet, "/api/v1/openapi.json", "")
			request.Header.Set("X-Request-Id", test.incoming)

			recorder := server.serve(request)

			responseRequestID := recorder.Header().Get("X-Request-Id")
			if test.echoed && responseRequestID != test.incoming {
				t.Errorf("X-Request-Id = %q, want the incoming %q", responseRequestID, test.incoming)
			}
			if _, err := uuid.Parse(responseRequestID); !test.echoed && err != nil {
				t.Errorf("X-Request-Id = %q, want a generated UUID: %v", responseRequestID, err)
			}
			if logged := server.records(t, logging.HTTPRequestCompleted)[0]["request_id"]; logged != responseRequestID {
				t.Errorf("logged request_id = %v, want %q", logged, responseRequestID)
			}
		})
	}
}

func TestAccessLogRecordsRoutePattern(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "get-thing",
		Method:      http.MethodGet,
		Path:        "/api/v1/things/{thing_id}",
	}, func(_ context.Context, input *thingPathInput) (*thingOutput, error) {
		return &thingOutput{Body: thingBody{Name: input.ThingID}}, nil
	})

	recorder := server.serve(newRequest(t, http.MethodGet, "/api/v1/things/thing_1", ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	records := server.records(t, logging.HTTPRequestCompleted)
	if len(records) != 1 {
		t.Fatalf("logged %d completed requests, want 1", len(records))
	}
	record := records[0]
	if record["route_pattern"] != "GET /api/v1/things/{thing_id}" {
		t.Errorf("route_pattern = %v, want GET /api/v1/things/{thing_id}", record["route_pattern"])
	}
	if record["method"] != http.MethodGet {
		t.Errorf("method = %v, want GET", record["method"])
	}
	if record["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", record["status"])
	}
	if duration, isNumber := record["duration_seconds"].(float64); !isNumber || duration < 0 {
		t.Errorf("duration_seconds = %v, want a non-negative number", record["duration_seconds"])
	}
	if record["request_id"] != recorder.Header().Get("X-Request-Id") {
		t.Errorf("request_id = %v, want %s", record["request_id"], recorder.Header().Get("X-Request-Id"))
	}
}

func TestPanicReturnsInternalErrorAndServerKeepsServing(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "explode",
		Method:      http.MethodGet,
		Path:        "/api/v1/explode",
	}, func(context.Context, *emptyInput) (*thingOutput, error) {
		panic("handler exploded")
	})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "get-thing",
		Method:      http.MethodGet,
		Path:        "/api/v1/things",
	}, func(context.Context, *emptyInput) (*thingOutput, error) {
		return &thingOutput{Body: thingBody{Name: "still serving"}}, nil
	})

	panicked := server.serve(newRequest(t, http.MethodGet, "/api/v1/explode", ""))
	served := server.serve(newRequest(t, http.MethodGet, "/api/v1/things", ""))

	assertProblem(t, panicked, http.StatusInternalServerError, "internal_error")
	if strings.Contains(panicked.Body.String(), "handler exploded") {
		t.Errorf("problem leaks the panic: %s", panicked.Body.String())
	}
	records := server.records(t, logging.HTTPPanicRecovered)
	if len(records) != 1 {
		t.Fatalf("logged %d recovered panics, want 1", len(records))
	}
	if records[0]["panic"] != "handler exploded" {
		t.Errorf("panic = %v, want handler exploded", records[0]["panic"])
	}
	if stack, isString := records[0]["stack"].(string); !isString || !strings.Contains(stack, "goroutine") {
		t.Errorf("stack = %v, want a goroutine stack", records[0]["stack"])
	}
	if records[0]["request_id"] != panicked.Header().Get("X-Request-Id") {
		t.Errorf("request_id = %v, want %s", records[0]["request_id"], panicked.Header().Get("X-Request-Id"))
	}
	completed := server.records(t, logging.HTTPRequestCompleted)
	if len(completed) != 2 || completed[0]["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("completed requests = %v, want the panic logged with status 500", completed)
	}
	if served.Code != http.StatusOK {
		t.Errorf("status after the panic = %d, want 200", served.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	tests := []struct {
		name                  string
		path                  string
		contentSecurityPolicy string
	}{
		{name: "dashboard path", path: "/dashboard/decisions", contentSecurityPolicy: dashboardContentSecurityPolicy},
		{name: "health path", path: "/healthz", contentSecurityPolicy: dashboardContentSecurityPolicy},
		{name: "api path", path: "/api/v1/openapi.json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, httpapi.RejectingAuthenticator{})
			server.mux.HandleFunc("GET /dashboard/", func(http.ResponseWriter, *http.Request) {})
			httpapi.RegisterHealthRoutes(server.mux, server.logger, nil)

			recorder := server.serve(newRequest(t, http.MethodGet, test.path, ""))

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			header := recorder.Header()
			if got := header.Get("Content-Security-Policy"); got != test.contentSecurityPolicy {
				t.Errorf("Content-Security-Policy = %q, want %q", got, test.contentSecurityPolicy)
			}
			if got := header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := header.Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
				t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", got)
			}
		})
	}
}

func TestDocsPageHasRelaxedContentSecurityPolicy(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})

	recorder := server.serve(newRequest(t, http.MethodGet, "/api/docs", ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	policy := recorder.Header().Get("Content-Security-Policy")
	if policy == dashboardContentSecurityPolicy || !strings.Contains(policy, "https://unpkg.com/") {
		t.Errorf("Content-Security-Policy = %q, want the docs renderer policy", policy)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
