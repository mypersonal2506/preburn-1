package httpapi_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
)

type thingSortKey struct {
	CreatedAt time.Time `json:"created_at"`
	ThingID   string    `json:"thing_id"`
}

type listThingsInput struct {
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit"`
}

var (
	thingsCursor    = httpapi.NewCursor[thingSortKey]("things")
	decisionsCursor = httpapi.NewCursor[thingSortKey]("decisions")
)

func TestCursorRoundTrip(t *testing.T) {
	key := thingSortKey{CreatedAt: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC), ThingID: "thing_1"}

	encoded, err := thingsCursor.Encode(httpapi.EnvironmentLive, key)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := thingsCursor.Decode(httpapi.EnvironmentLive, encoded)

	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if diff := cmp.Diff(key, decoded); diff != "" {
		t.Errorf("key mismatch (-want +got):\n%s", diff)
	}
}

func TestCursorDecodeRejectsForeignAndMalformedCursors(t *testing.T) {
	decisionCursor, err := decisionsCursor.Encode(httpapi.EnvironmentTest, thingSortKey{ThingID: "dec_1"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	liveCursor, err := thingsCursor.Encode(httpapi.EnvironmentLive, thingSortKey{ThingID: "thing_1"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	tests := []struct {
		name    string
		encoded string
	}{
		{name: "cursor of another listing", encoded: decisionCursor},
		{name: "cursor of another environment", encoded: liveCursor},
		{name: "cursor without an environment", encoded: base64.RawURLEncoding.EncodeToString([]byte(`{"listing":"things","key":{"thing_id":"thing_1"}}`))},
		{name: "not base64url", encoded: "not a cursor"},
		{name: "not JSON", encoded: "bm90IGpzb24"},
		{name: "wrong key shape", encoded: "eyJsaXN0aW5nIjoidGhpbmdzIiwia2V5IjoxfQ"},
		{name: "empty", encoded: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := thingsCursor.Decode(httpapi.EnvironmentTest, test.encoded)

			if !errors.Is(err, httpapi.ErrInvalidCursor) {
				t.Errorf("Decode error = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

func TestParseLimit(t *testing.T) {
	tests := []struct {
		name    string
		limit   int
		want    int
		invalid bool
	}{
		{name: "absent selects the default", limit: 0, want: 50},
		{name: "one", limit: 1, want: 1},
		{name: "maximum", limit: 100, want: 100},
		{name: "above maximum", limit: 101, invalid: true},
		{name: "negative", limit: -1, invalid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := httpapi.ParseLimit(test.limit)

			if test.invalid != (err != nil) {
				t.Fatalf("ParseLimit(%d) error = %v, want invalid %t", test.limit, err, test.invalid)
			}
			if got != test.want {
				t.Errorf("ParseLimit(%d) = %d, want %d", test.limit, got, test.want)
			}
		})
	}
}

func TestListRoute(t *testing.T) {
	decisionCursor, err := decisionsCursor.Encode(httpapi.EnvironmentTest, thingSortKey{ThingID: "dec_1"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	thingCursor, err := thingsCursor.Encode(httpapi.EnvironmentTest, thingSortKey{ThingID: "thing_1"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	tests := []struct {
		name        string
		environment httpapi.Environment
		query       string
		status      int
		code        string
		location    string
		wantBody    map[string]any
	}{
		{
			name:        "first page with the default limit",
			environment: httpapi.EnvironmentTest,
			status:      http.StatusOK,
			wantBody:    map[string]any{"items": []any{"limit=50"}, "next_cursor": thingCursor},
		},
		{
			name:        "last page",
			environment: httpapi.EnvironmentTest,
			query:       "?limit=100&cursor=" + thingCursor,
			status:      http.StatusOK,
			wantBody:    map[string]any{"items": []any{}, "next_cursor": nil},
		},
		{name: "limit above maximum", environment: httpapi.EnvironmentTest, query: "?limit=101", status: http.StatusUnprocessableEntity, code: "validation_failed", location: "query.limit"},
		{name: "cursor of another listing", environment: httpapi.EnvironmentTest, query: "?cursor=" + decisionCursor, status: http.StatusUnprocessableEntity, code: "invalid_cursor"},
		{name: "cursor of another environment", environment: httpapi.EnvironmentLive, query: "?cursor=" + thingCursor, status: http.StatusUnprocessableEntity, code: "invalid_cursor"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, &environmentAuthenticator{})
			httpapi.Register(server.api, httpapi.RouteGroupAdmin, huma.Operation{
				OperationID: "list-things",
				Method:      http.MethodGet,
				Path:        "/api/v1/things",
			}, listThings)
			request := newRequest(t, http.MethodGet, "/api/v1/things"+test.query, "")
			request.Header.Set(httpapi.EnvironmentHeader, string(test.environment))

			recorder := server.serve(request)

			if test.code != "" {
				problem := assertProblem(t, recorder, test.status, test.code)
				if test.location != "" && problem["errors"].([]any)[0].(map[string]any)["location"] != test.location {
					t.Errorf("errors = %v, want location %s", problem["errors"], test.location)
				}
				return
			}
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d, body %s", recorder.Code, test.status, recorder.Body.String())
			}
			if diff := cmp.Diff(test.wantBody, decodeBody(t, recorder)); diff != "" {
				t.Errorf("body mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func listThings(ctx context.Context, input *listThingsInput) (*httpapi.Page[string], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	limit, err := httpapi.ParseLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	if input.Cursor != "" {
		if _, err := thingsCursor.Decode(environment, input.Cursor); err != nil {
			return nil, err
		}
		return httpapi.NewPage[string](nil, ""), nil
	}
	nextCursor, err := thingsCursor.Encode(environment, thingSortKey{ThingID: "thing_1"})
	if err != nil {
		return nil, err
	}
	return httpapi.NewPage([]string{"limit=" + strconv.Itoa(limit)}, nextCursor), nil
}
