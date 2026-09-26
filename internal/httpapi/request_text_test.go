package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
)

const (
	textRule   = "expected valid Unicode text without NUL characters"
	thingsPath = "/api/v1/things/"
)

type storedThingInput struct {
	ThingID string `path:"thing_id"`
	Search  string `query:"search"`
	Body    storedThingBody
}

type storedThingBody struct {
	Name     string                     `json:"name,omitempty"`
	Tags     []string                   `json:"tags,omitempty"`
	Labels   map[string]string          `json:"labels,omitempty"`
	Metadata map[string]json.RawMessage `json:"metadata,omitempty"`
	Items    []storedThingItem          `json:"items,omitempty"`
}

type storedThingItem struct {
	Note *string `json:"note,omitempty"`
}

func TestRegisterRejectsTextPostgresCannotStore(t *testing.T) {
	server := newStoredThingServer(t)
	tests := []struct {
		name      string
		target    string
		body      string
		locations []string
	}{
		{name: "NUL in a string", target: thingsPath + "a", body: `{"name":"a\u0000b"}`, locations: []string{"body.name"}},
		{name: "NUL in a list item", target: thingsPath + "a", body: `{"tags":["a","\u0000"]}`, locations: []string{"body.tags[1]"}},
		{name: "NUL in a map key", target: thingsPath + "a", body: `{"labels":{"a\u0000":"b"}}`, locations: []string{"body.labels"}},
		{name: "NUL in a map value", target: thingsPath + "a", body: `{"labels":{"a":"\u0000"}}`, locations: []string{"body.labels"}},
		{name: "NUL in raw JSON", target: thingsPath + "a", body: `{"metadata":{"a":{"b":["\u0000"]}}}`, locations: []string{"body.metadata"}},
		{name: "lone high surrogate in raw JSON", target: thingsPath + "a", body: `{"metadata":{"a":"\ud800"}}`, locations: []string{"body.metadata"}},
		{name: "high surrogate before another escape in raw JSON", target: thingsPath + "a", body: `{"metadata":{"a":"\ud800A"}}`, locations: []string{"body.metadata"}},
		{name: "lone low surrogate in raw JSON", target: thingsPath + "a", body: `{"metadata":{"a":"x\udc00"}}`, locations: []string{"body.metadata"}},
		{name: "NUL in a nested struct", target: thingsPath + "a", body: `{"items":[{},{"note":"\u0000"}]}`, locations: []string{"body.items[1].note"}},
		{name: "NUL in a query parameter", target: thingsPath + "a?search=a%00", body: `{}`, locations: []string{"query.search"}},
		{name: "invalid UTF-8 in a query parameter", target: thingsPath + "a?search=a%ff", body: `{}`, locations: []string{"query.search"}},
		{name: "NUL in a path parameter", target: thingsPath + "a%00b", body: `{}`, locations: []string{"path.thing_id"}},
		{
			name:      "every field",
			target:    thingsPath + "a%00?search=%00",
			body:      `{"name":"\u0000","labels":{"\u0000":""},"metadata":{"a":"\udfff"}}`,
			locations: []string{"path.thing_id", "query.search", "body.name", "body.labels", "body.metadata"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := server.serve(newRequest(t, http.MethodPut, test.target, test.body))

			problem := assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed")
			var got []any
			for _, location := range test.locations {
				got = append(got, map[string]any{"location": location, "message": textRule})
			}
			if diff := cmp.Diff(got, problem["errors"]); diff != "" {
				t.Errorf("errors mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRegisterLeavesPublicRoutesToTheirDomain(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "put-public-thing",
		Method:      http.MethodPut,
		Path:        "/api/v1/things/{thing_id}",
	}, func(context.Context, *storedThingInput) (*thingOutput, error) {
		return &thingOutput{Body: thingBody{Name: "stored"}}, nil
	})

	recorder := server.serve(newRequest(t, http.MethodPut, thingsPath+"a%00?search=%00", `{"name":"\u0000"}`))

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 from the handler, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestRegisterAcceptsStorableText(t *testing.T) {
	server := newStoredThingServer(t)
	tests := []struct {
		name string
		body string
	}{
		{name: "surrogate pair in raw JSON", body: `{"metadata":{"a":"😀"}}`},
		{name: "escaped backslash before u0000 text", body: `{"metadata":{"a":"\\u0000"},"name":"\\u0000"}`},
		{name: "replacement character", body: `{"metadata":{"a":"�"},"name":"` + "�" + `"}`},
		{name: "lone surrogate decoded into a string", body: `{"name":"\ud800","tags":["\udc00"]}`},
		{name: "control characters other than NUL", body: `{"name":"a\tb\u0001","metadata":{"a":"\u001f"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := server.serve(newRequest(t, http.MethodPut, thingsPath+"a?search=b", test.body))

			if recorder.Code != http.StatusOK {
				t.Errorf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func newStoredThingServer(t *testing.T) *testServer {
	t.Helper()
	server := newTestServer(t, fixedAuthenticator{principal: keyPrincipal{environment: httpapi.EnvironmentTest}})
	httpapi.Register(server.api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID: "put-thing",
		Method:      http.MethodPut,
		Path:        "/api/v1/things/{thing_id}",
	}, func(context.Context, *storedThingInput) (*thingOutput, error) {
		return &thingOutput{Body: thingBody{Name: "stored"}}, nil
	})
	return server
}
