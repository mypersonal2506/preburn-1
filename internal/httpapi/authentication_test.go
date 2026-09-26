package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/logging"
)

type testPrincipal struct {
	keyID string
}

type principalOutput struct {
	Body principalBody
}

type principalBody struct {
	KeyID string `json:"key_id"`
}

type recordingAuthenticator struct {
	groups    []httpapi.RouteGroup
	principal httpapi.Principal
	err       error
}

type environmentAuthenticator struct {
	environments []string
}

func (authenticator *recordingAuthenticator) Authenticate(_ context.Context, request *http.Request, group httpapi.RouteGroup) (httpapi.Principal, error) {
	if request.Header.Get("Authorization") != "Bearer test" {
		return nil, errors.New("authenticate called without the request headers")
	}
	authenticator.groups = append(authenticator.groups, group)
	return authenticator.principal, authenticator.err
}

func (authenticator *environmentAuthenticator) Authenticate(_ context.Context, request *http.Request, _ httpapi.RouteGroup) (httpapi.Principal, error) {
	authenticator.environments = append(authenticator.environments, request.Header.Values(httpapi.EnvironmentHeader)...)
	environment, err := httpapi.EnvironmentFromHeader(request.Header)
	if err != nil {
		return nil, err
	}
	return httpapi.MemberPrincipal{Environment: environment}, nil
}

func TestRejectingAuthenticatorReturns401OnNonPublicRoutes(t *testing.T) {
	for _, group := range []httpapi.RouteGroup{httpapi.RouteGroupRuntime, httpapi.RouteGroupAdmin, httpapi.RouteGroupDashboard} {
		t.Run(string(group), func(t *testing.T) {
			server := newTestServer(t, httpapi.RejectingAuthenticator{})
			httpapi.Register(server.api, group, huma.Operation{
				OperationID: "get-thing",
				Method:      http.MethodGet,
				Path:        "/api/v1/things",
			}, func(context.Context, *emptyInput) (*thingOutput, error) {
				t.Error("handler ran without a principal")
				return &thingOutput{}, nil
			})

			recorder := server.serve(newRequest(t, http.MethodGet, "/api/v1/things", ""))

			assertProblem(t, recorder, http.StatusUnauthorized, "authentication_required")
		})
	}
}

func TestPublicRouteSkipsAuthentication(t *testing.T) {
	authenticator := &recordingAuthenticator{err: errors.New("authenticate called on a public route")}
	server := newTestServer(t, authenticator)
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "get-thing",
		Method:      http.MethodGet,
		Path:        "/api/v1/things",
	}, func(ctx context.Context, _ *emptyInput) (*thingOutput, error) {
		if principal := httpapi.PrincipalFromContext(ctx); principal != nil {
			t.Errorf("principal = %v on a public route, want nil", principal)
		}
		return &thingOutput{Body: thingBody{Name: "public"}}, nil
	})

	recorder := server.serve(newRequest(t, http.MethodGet, "/api/v1/things", ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	if len(authenticator.groups) != 0 {
		t.Errorf("authenticator ran for groups %v", authenticator.groups)
	}
}

func TestAuthenticatedPrincipalReachesHandler(t *testing.T) {
	authenticator := &recordingAuthenticator{principal: testPrincipal{keyID: "key_1"}}
	server := newTestServer(t, authenticator)
	httpapi.Register(server.api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "get-principal",
		Method:      http.MethodGet,
		Path:        "/api/v1/principal",
	}, func(ctx context.Context, _ *emptyInput) (*principalOutput, error) {
		principal := httpapi.PrincipalFromContext(ctx).(testPrincipal)
		return &principalOutput{Body: principalBody{KeyID: principal.keyID}}, nil
	})
	request := newRequest(t, http.MethodGet, "/api/v1/principal", "")
	request.Header.Set("Authorization", "Bearer test")

	recorder := server.serve(request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	if body := decodeBody(t, recorder); body["key_id"] != "key_1" {
		t.Errorf("key_id = %v, want key_1", body["key_id"])
	}
	if len(authenticator.groups) != 1 || authenticator.groups[0] != httpapi.RouteGroupAdmin {
		t.Errorf("authenticated groups = %v, want [admin]", authenticator.groups)
	}
}

func TestAuthenticatorErrorsBecomeProblems(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		status        int
		code          string
		internalCount int
	}{
		{name: "coded error", err: thingNotFoundError{}, status: http.StatusNotFound, code: "not_found"},
		{name: "plain error", err: errors.New("session store unreachable"), status: http.StatusInternalServerError, code: "internal_error", internalCount: 1},
		{name: "scope forbidden", err: httpapi.ErrScopeForbidden, status: http.StatusForbidden, code: "scope_forbidden"},
		{name: "csrf invalid", err: httpapi.ErrCSRFInvalid, status: http.StatusForbidden, code: "csrf_invalid"},
		{name: "environment header invalid", err: httpapi.ErrEnvironmentHeaderInvalid, status: http.StatusUnprocessableEntity, code: "environment_header_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, &recordingAuthenticator{err: test.err})
			httpapi.Register(server.api, httpapi.RouteGroupRuntime, huma.Operation{
				OperationID: "get-thing",
				Method:      http.MethodGet,
				Path:        "/api/v1/things",
			}, func(context.Context, *emptyInput) (*thingOutput, error) {
				t.Error("handler ran after the authenticator failed")
				return &thingOutput{}, nil
			})
			request := newRequest(t, http.MethodGet, "/api/v1/things", "")
			request.Header.Set("Authorization", "Bearer test")

			recorder := server.serve(request)

			assertProblem(t, recorder, test.status, test.code)
			if records := server.records(t, logging.HTTPInternalError); len(records) != test.internalCount {
				t.Errorf("logged %d internal errors, want %d", len(records), test.internalCount)
			}
		})
	}
}

func TestEventStreamRoutesTakeTheEnvironmentFromTheQuery(t *testing.T) {
	tests := []struct {
		name               string
		target             string
		header             string
		wantStatus         int
		wantAuthentication []string
	}{
		{name: "query names the environment", target: "/api/v1/stream?environment=live", wantStatus: http.StatusOK, wantAuthentication: []string{"live"}},
		{name: "query replaces the header", target: "/api/v1/stream?environment=live", header: "test", wantStatus: http.StatusOK, wantAuthentication: []string{"live"}},
		{name: "missing environment", target: "/api/v1/stream", header: "test", wantStatus: http.StatusUnprocessableEntity},
		{name: "unknown environment", target: "/api/v1/stream?environment=LIVE", wantStatus: http.StatusUnprocessableEntity},
		{name: "repeated environment", target: "/api/v1/stream?environment=test&environment=live", wantStatus: http.StatusUnprocessableEntity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authenticator := &environmentAuthenticator{}
			server := newTestServer(t, authenticator)
			httpapi.RegisterEventStream(server.api, httpapi.RouteGroupDashboard, huma.Operation{
				OperationID: "stream-things",
				Method:      http.MethodGet,
				Path:        "/api/v1/stream",
			}, func(ctx context.Context, _ *emptyInput) (*thingOutput, error) {
				environment, err := httpapi.EnvironmentFromContext(ctx)
				return &thingOutput{Body: thingBody{Name: string(environment)}}, err
			})
			request := newRequest(t, http.MethodGet, test.target, "")
			if test.header != "" {
				request.Header.Set(httpapi.EnvironmentHeader, test.header)
			}

			recorder := server.serve(request)

			if diff := cmp.Diff(test.wantAuthentication, authenticator.environments); diff != "" {
				t.Errorf("environment headers the authenticator saw mismatch (-want +got):\n%s", diff)
			}
			if test.wantStatus == http.StatusOK {
				if recorder.Code != http.StatusOK || decodeBody(t, recorder)["name"] != "live" {
					t.Errorf("status = %d, body %s, want 200 in environment live", recorder.Code, recorder.Body.String())
				}
				return
			}
			problem := assertProblem(t, recorder, test.wantStatus, "validation_failed")
			want := []any{map[string]any{"location": "query.environment", "message": "expected test or live"}}
			if diff := cmp.Diff(want, problem["errors"]); diff != "" {
				t.Errorf("errors mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
