package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
)

type totalsOutput struct {
	Body totalsResponse
}

type totalsResponse struct {
	Total int `json:"total"`
}

type recordThingOutput struct {
	Status int
	Body   thingBody
}

func (totalsResponse) SchemaName() string {
	return "ThingTotalsResponse"
}

func TestWriteOpenAPIIsDeterministic(t *testing.T) {
	first := writeOpenAPI(t, newDocumentedServer(t))
	second := writeOpenAPI(t, newDocumentedServer(t))
	server := newDocumentedServer(t)
	repeated := writeOpenAPI(t, server)
	repeatedAgain := writeOpenAPI(t, server)

	if !bytes.Equal(first, second) {
		t.Errorf("two APIs wrote different documents:\n%s", cmp.Diff(string(first), string(second)))
	}
	if !bytes.Equal(repeated, repeatedAgain) {
		t.Errorf("one API wrote different documents:\n%s", cmp.Diff(string(repeated), string(repeatedAgain)))
	}
	if !strings.HasPrefix(string(first), "{\n  \"components\": {\n    \"schemas\": {") {
		t.Errorf("document does not start with sorted keys and a 2-space indent:\n%.200s", first)
	}
	if !strings.HasSuffix(string(first), "}\n") {
		t.Error("document does not end with a newline")
	}
}

func TestOpenAPIDocumentDescribesAPI(t *testing.T) {
	var document map[string]any
	if err := json.Unmarshal(writeOpenAPI(t, newDocumentedServer(t)), &document); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	checks := []struct {
		path []string
		want any
	}{
		{path: []string{"openapi"}, want: "3.1.0"},
		{path: []string{"info", "title"}, want: "Preburn"},
		{path: []string{"info", "version"}, want: testVersion},
		{path: []string{"info", "license"}, want: map[string]any{"name": "Apache-2.0", "identifier": "Apache-2.0"}},
		{path: []string{"components", "securitySchemes", "api_key"}, want: map[string]any{
			"type": "http", "scheme": "bearer", "description": "Runtime or admin API key sent as a bearer token.",
		}},
		{path: []string{"components", "securitySchemes", "member_session"}, want: map[string]any{
			"type": "apiKey", "in": "cookie", "name": "preburn_session",
			"description": "Member session cookie. Unsafe methods also send the X-CSRF-Token header.",
		}},
		{path: []string{"paths", "/api/v1/setup", "post", "security"}, want: nil},
		{path: []string{"paths", "/api/v1/check", "post", "security"}, want: []any{map[string]any{"api_key": []any{}}}},
		{path: []string{"paths", "/api/v1/plans", "get", "security"}, want: []any{
			map[string]any{"api_key": []any{}}, map[string]any{"member_session": []any{}},
		}},
		{path: []string{"paths", "/api/v1/dashboard/overview", "get", "security"}, want: []any{map[string]any{"member_session": []any{}}}},
		{path: []string{"paths", "/api/v1/check", "post", "responses", "default", "content"}, want: map[string]any{
			"application/problem+json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Problem"}},
		}},
	}
	for _, check := range checks {
		if diff := cmp.Diff(check.want, lookup(document, check.path)); diff != "" {
			t.Errorf("%s mismatch (-want +got):\n%s", strings.Join(check.path, "."), diff)
		}
	}
	problemProperties := lookup(document, []string{"components", "schemas", "Problem", "properties"}).(map[string]any)
	for _, property := range []string{"type", "title", "status", "detail", "code", "errors"} {
		if _, found := problemProperties[property]; !found {
			t.Errorf("Problem schema has no %s property", property)
		}
	}
	if diff := cmp.Diff("array", lookup(document, []string{"components", "schemas", "Problem", "properties", "errors", "type"})); diff != "" {
		t.Errorf("Problem errors type mismatch (-want +got):\n%s", diff)
	}
	if containsKey(lookup(document, []string{"components", "schemas"}), "value") {
		t.Error("problem schemas define a value property")
	}
}

func TestOpenAPIAndDocsAreServed(t *testing.T) {
	server := newDocumentedServer(t)

	document := server.serve(newRequest(t, http.MethodGet, "/api/v1/openapi.json", ""))
	docs := server.serve(newRequest(t, http.MethodGet, "/api/docs", ""))

	if document.Code != http.StatusOK || !strings.Contains(document.Body.String(), `"/api/v1/check"`) {
		t.Errorf("GET /api/v1/openapi.json = %d, want 200 with the document", document.Code)
	}
	if docs.Code != http.StatusOK || !strings.Contains(docs.Header().Get("Content-Type"), "text/html") {
		t.Errorf("GET /api/docs = %d %s, want 200 text/html", docs.Code, docs.Header().Get("Content-Type"))
	}
}

func TestSchemaNameNamesTheSchemaOfItsType(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "get-totals",
		Method:      http.MethodGet,
		Path:        "/api/v1/totals",
	}, func(context.Context, *emptyInput) (*totalsOutput, error) {
		return &totalsOutput{}, nil
	})
	httpapi.Register(server.api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-totals",
		Method:      http.MethodGet,
		Path:        "/api/v1/totals/pages",
	}, func(context.Context, *emptyInput) (*httpapi.Page[totalsResponse], error) {
		return httpapi.NewPage([]totalsResponse{}, ""), nil
	})
	httpapi.Register(server.api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-names",
		Method:      http.MethodGet,
		Path:        "/api/v1/names",
	}, func(context.Context, *emptyInput) (*httpapi.Page[string], error) {
		return httpapi.NewPage([]string{}, ""), nil
	})
	var document map[string]any
	if err := json.Unmarshal(writeOpenAPI(t, server), &document); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	schemas := lookup(document, []string{"components", "schemas"}).(map[string]any)
	if _, named := schemas["ThingTotalsResponse"]; !named {
		t.Errorf("schemas %v, want ThingTotalsResponse", slices.Sorted(maps.Keys(schemas)))
	}
	if _, named := schemas["TotalsResponse"]; named {
		t.Error("schemas name the type by its Go name TotalsResponse")
	}
	wantReference := map[string]any{"$ref": "#/components/schemas/ThingTotalsResponse"}
	if diff := cmp.Diff(wantReference, lookup(document, []string{"paths", "/api/v1/totals", "get", "responses", "200", "content", "application/json", "schema"})); diff != "" {
		t.Errorf("response schema mismatch (-want +got):\n%s", diff)
	}
	wantPageReference := map[string]any{"$ref": "#/components/schemas/PageBodyThingTotalsResponse"}
	if diff := cmp.Diff(wantPageReference, lookup(document, []string{"paths", "/api/v1/totals/pages", "get", "responses", "200", "content", "application/json", "schema"})); diff != "" {
		t.Errorf("page schema mismatch (-want +got):\n%s", diff)
	}
	if _, named := schemas["PageBodyString"]; !named {
		t.Errorf("schemas %v, want PageBodyString for a page of strings", slices.Sorted(maps.Keys(schemas)))
	}
}

func TestJSONResponseDocumentsAnotherStatusOfTheBody(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID:   "record-thing",
		Method:        http.MethodPost,
		Path:          "/api/v1/things",
		DefaultStatus: http.StatusCreated,
		Responses: map[string]*huma.Response{
			"200": httpapi.JSONResponse[thingBody](server.api, "The thing was recorded before."),
		},
	}, func(context.Context, *emptyInput) (*recordThingOutput, error) {
		return &recordThingOutput{Status: http.StatusOK}, nil
	})
	var document map[string]any
	if err := json.Unmarshal(writeOpenAPI(t, server), &document); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	responses := lookup(document, []string{"paths", "/api/v1/things", "post", "responses"})
	thingSchema := map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ThingBody"}}}
	want := map[string]any{
		"200": map[string]any{"description": "The thing was recorded before.", "content": thingSchema},
		"201": map[string]any{"description": "Created", "content": thingSchema},
		"default": map[string]any{
			"description": "Error",
			"content":     map[string]any{"application/problem+json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Problem"}}},
		},
	}
	if diff := cmp.Diff(want, responses); diff != "" {
		t.Errorf("responses mismatch (-want +got):\n%s", diff)
	}
}

func newDocumentedServer(t *testing.T) *testServer {
	t.Helper()
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	routes := []struct {
		group     httpapi.RouteGroup
		operation huma.Operation
	}{
		{group: httpapi.RouteGroupPublic, operation: huma.Operation{OperationID: "run-setup", Method: http.MethodPost, Path: "/api/v1/setup"}},
		{group: httpapi.RouteGroupRuntime, operation: huma.Operation{OperationID: "check", Method: http.MethodPost, Path: "/api/v1/check"}},
		{group: httpapi.RouteGroupAdmin, operation: huma.Operation{OperationID: "list-plans", Method: http.MethodGet, Path: "/api/v1/plans"}},
		{group: httpapi.RouteGroupDashboard, operation: huma.Operation{OperationID: "get-overview", Method: http.MethodGet, Path: "/api/v1/dashboard/overview"}},
	}
	for _, route := range routes {
		httpapi.Register(server.api, route.group, route.operation, func(context.Context, *createThingInput) (*thingOutput, error) {
			return &thingOutput{}, nil
		})
	}
	httpapi.Register(server.api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-things",
		Method:      http.MethodGet,
		Path:        "/api/v1/things",
	}, listThings)
	return server
}

func writeOpenAPI(t *testing.T, server *testServer) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := httpapi.WriteOpenAPI(server.api, &output); err != nil {
		t.Fatalf("WriteOpenAPI: %v", err)
	}
	return output.Bytes()
}

func lookup(document any, path []string) any {
	for _, key := range path {
		object, isObject := document.(map[string]any)
		if !isObject {
			return nil
		}
		document = object[key]
	}
	return document
}
