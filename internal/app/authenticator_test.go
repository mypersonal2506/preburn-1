package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/members"
)

const (
	authorizationHeader = "Authorization"
	runtimeProbePath    = "/api/v1/runtime-probe"
	currentMemberPath   = "/api/v1/auth/me"
	proxyAuthorization  = "Basic b3BlcmF0b3I6cHJveHkgcGFzc3dvcmQ="
)

func TestAuthenticatorRoutesBearerKeysBeforeSessions(t *testing.T) {
	application, _ := newTestApp(t, RoleAPI)
	member, err := application.Installation.CreateAdmin(t.Context(), testEmail, testDisplayName, testPassword)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	sessionToken, err := application.Members.Sessions().Start(t.Context(), member.ID, "test", netip.MustParseAddr("127.0.0.1"))
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	_, secret, err := application.APIKeys.Create(t.Context(), httpapi.EnvironmentTest, testKeyName, apikeys.ScopeRuntime, nil)
	if err != nil {
		t.Fatalf("create runtime key: %v", err)
	}
	mux := http.NewServeMux()
	api := newAPI(mux, application)
	httpapi.Register(api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID:   "runtime-probe",
		Method:        http.MethodGet,
		Path:          runtimeProbePath,
		DefaultStatus: http.StatusNoContent,
	}, func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})

	tests := []struct {
		name          string
		path          string
		authorization []string
		session       bool
		status        int
		code          string
	}{
		{name: "session on a dashboard route", path: currentMemberPath, session: true, status: http.StatusOK},
		{name: "no credentials on a dashboard route", path: currentMemberPath, status: http.StatusUnauthorized, code: "authentication_required"},
		{name: "bearer key with a session on a dashboard route", path: currentMemberPath, authorization: []string{"Bearer " + secret}, session: true, status: http.StatusForbidden, code: "scope_forbidden"},
		{name: "malformed bearer key with a session", path: currentMemberPath, authorization: []string{"Bearer unknown"}, session: true, status: http.StatusUnauthorized, code: "authentication_required"},
		{name: "lowercase bearer scheme with a session", path: currentMemberPath, authorization: []string{"bearer unknown"}, session: true, status: http.StatusUnauthorized, code: "authentication_required"},
		{name: "bearer scheme without a token with a session", path: currentMemberPath, authorization: []string{"Bearer"}, session: true, status: http.StatusUnauthorized, code: "authentication_required"},
		{name: "basic and bearer headers with a session", path: currentMemberPath, authorization: []string{proxyAuthorization, "Bearer " + secret}, session: true, status: http.StatusUnauthorized, code: "authentication_required"},
		{name: "proxy basic credentials with a session on a dashboard route", path: currentMemberPath, authorization: []string{proxyAuthorization}, session: true, status: http.StatusOK},
		{name: "proxy basic credentials without a session", path: currentMemberPath, authorization: []string{proxyAuthorization}, status: http.StatusUnauthorized, code: "authentication_required"},
		{name: "proxy basic credentials with a session on a runtime route", path: runtimeProbePath, authorization: []string{proxyAuthorization}, session: true, status: http.StatusForbidden, code: "scope_forbidden"},
		{name: "empty authorization header with a session", path: currentMemberPath, authorization: []string{""}, session: true, status: http.StatusOK},
		{name: "session on a runtime route", path: runtimeProbePath, session: true, status: http.StatusForbidden, code: "scope_forbidden"},
		{name: "no credentials on a runtime route", path: runtimeProbePath, status: http.StatusUnauthorized, code: "authentication_required"},
		{name: "bearer key with a session on a runtime route", path: runtimeProbePath, authorization: []string{"Bearer " + secret}, session: true, status: http.StatusNoContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, nil)
			if test.authorization != nil {
				request.Header[authorizationHeader] = test.authorization
			}
			if test.session {
				request.AddCookie(&http.Cookie{Name: members.SessionCookieName, Value: sessionToken}) //nolint:gosec // G124: a request cookie carries no attributes.
				request.Header.Set(httpapi.EnvironmentHeader, string(httpapi.EnvironmentTest))
			}
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d, body %s", recorder.Code, test.status, recorder.Body.String())
			}
			if test.code == "" {
				return
			}
			var problem struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode problem %q: %v", recorder.Body.String(), err)
			}
			if problem.Code != test.code {
				t.Errorf("code = %q, want %q", problem.Code, test.code)
			}
		})
	}
}
