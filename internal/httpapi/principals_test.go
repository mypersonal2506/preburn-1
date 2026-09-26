package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
)

type fixedAuthenticator struct {
	principal httpapi.Principal
}

type keyPrincipal struct {
	environment httpapi.Environment
}

type environmentOutput struct {
	Body environmentBody
}

type environmentBody struct {
	Environment string `json:"environment"`
	MemberID    string `json:"member_id,omitempty"`
}

func (authenticator fixedAuthenticator) Authenticate(context.Context, *http.Request, httpapi.RouteGroup) (httpapi.Principal, error) {
	return authenticator.principal, nil
}

func (principal keyPrincipal) PrincipalEnvironment() httpapi.Environment {
	return principal.environment
}

func TestParseEnvironment(t *testing.T) {
	tests := []struct {
		value string
		want  httpapi.Environment
		err   error
	}{
		{value: "test", want: httpapi.EnvironmentTest},
		{value: "live", want: httpapi.EnvironmentLive},
		{value: "", err: httpapi.ErrUnknownEnvironment},
		{value: "LIVE", err: httpapi.ErrUnknownEnvironment},
		{value: " live", err: httpapi.ErrUnknownEnvironment},
		{value: "production", err: httpapi.ErrUnknownEnvironment},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			environment, err := httpapi.ParseEnvironment(test.value)

			if !errors.Is(err, test.err) {
				t.Fatalf("error = %v, want %v", err, test.err)
			}
			if environment != test.want {
				t.Errorf("environment = %q, want %q", environment, test.want)
			}
		})
	}
}

func TestEnvironmentFromHeader(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   httpapi.Environment
		err    error
	}{
		{name: "test", values: []string{"test"}, want: httpapi.EnvironmentTest},
		{name: "live", values: []string{"live"}, want: httpapi.EnvironmentLive},
		{name: "missing", err: httpapi.ErrEnvironmentHeaderInvalid},
		{name: "empty", values: []string{""}, err: httpapi.ErrEnvironmentHeaderInvalid},
		{name: "unknown", values: []string{"staging"}, err: httpapi.ErrEnvironmentHeaderInvalid},
		{name: "other case", values: []string{"Test"}, err: httpapi.ErrEnvironmentHeaderInvalid},
		{name: "repeated", values: []string{"test", "test"}, err: httpapi.ErrEnvironmentHeaderInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := http.Header{}
			for _, value := range test.values {
				header.Add(httpapi.EnvironmentHeader, value)
			}

			environment, err := httpapi.EnvironmentFromHeader(header)

			if !errors.Is(err, test.err) {
				t.Fatalf("error = %v, want %v", err, test.err)
			}
			if environment != test.want {
				t.Errorf("environment = %q, want %q", environment, test.want)
			}
		})
	}
}

func TestEnvironmentFromContextReadsEveryEnvironmentPrincipal(t *testing.T) {
	memberID := uuid.MustParse("01920000-0000-7000-8000-000000000001")
	tests := []struct {
		name      string
		principal httpapi.Principal
		want      environmentBody
	}{
		{
			name:      "member session",
			principal: httpapi.MemberPrincipal{MemberID: memberID, Environment: httpapi.EnvironmentLive},
			want:      environmentBody{Environment: "live", MemberID: memberID.String()},
		},
		{
			name:      "other environment principal",
			principal: keyPrincipal{environment: httpapi.EnvironmentTest},
			want:      environmentBody{Environment: "test"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, fixedAuthenticator{principal: test.principal})
			registerEnvironmentRoute(server, httpapi.RouteGroupAdmin)

			recorder := server.serve(newRequest(t, http.MethodGet, "/api/v1/environment", ""))

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
			}
			var body environmentBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if diff := cmp.Diff(test.want, body); diff != "" {
				t.Errorf("body mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEnvironmentFromContextFailsForPrincipalWithoutEnvironment(t *testing.T) {
	server := newTestServer(t, fixedAuthenticator{principal: testPrincipal{keyID: "key_1"}})
	registerEnvironmentRoute(server, httpapi.RouteGroupAdmin)

	recorder := server.serve(newRequest(t, http.MethodGet, "/api/v1/environment", ""))

	assertProblem(t, recorder, http.StatusInternalServerError, "internal_error")
}

func TestPrincipalAccessorsFailOnPublicRoutes(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	registerEnvironmentRoute(server, httpapi.RouteGroupPublic)

	recorder := server.serve(newRequest(t, http.MethodGet, "/api/v1/environment", ""))

	assertProblem(t, recorder, http.StatusInternalServerError, "internal_error")
}

func registerEnvironmentRoute(server *testServer, group httpapi.RouteGroup) {
	httpapi.Register(server.api, group, huma.Operation{
		OperationID: "get-environment",
		Method:      http.MethodGet,
		Path:        "/api/v1/environment",
	}, func(ctx context.Context, _ *emptyInput) (*environmentOutput, error) {
		environment, err := httpapi.EnvironmentFromContext(ctx)
		if err != nil {
			return nil, err
		}
		output := &environmentOutput{Body: environmentBody{Environment: string(environment)}}
		if member, err := httpapi.MemberPrincipalFromContext(ctx); err == nil {
			output.Body.MemberID = member.MemberID.String()
		}
		return output, nil
	})
}
