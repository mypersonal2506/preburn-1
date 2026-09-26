package installation_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
)

const sessionMaxAge = int(30 * 24 * time.Hour / time.Second)

func TestSetupStartsSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		publicURL string
		secure    bool
	}{
		{publicURL: insecureURL, secure: false},
		{publicURL: secureURL, secure: true},
	}
	for _, test := range tests {
		t.Run(test.publicURL, func(t *testing.T) {
			t.Parallel()
			harness := newHarness(t, test.publicURL)
			token := harness.prepareSetupToken(t)

			recorder := harness.completeSetup(t, token)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
			}
			wantBody := map[string]any{
				"id":            harness.memberIdentifier(t, testEmail),
				"email":         testEmail,
				"display_name":  testDisplayName,
				"status":        "active",
				"has_password":  true,
				"last_login_at": testStart.Format(time.RFC3339),
				"created_at":    testStart.Format(time.RFC3339),
			}
			if diff := cmp.Diff(wantBody, decodeBody(t, recorder)); diff != "" {
				t.Errorf("body mismatch (-want +got):\n%s", diff)
			}
			cookies := responseCookies(t, recorder)
			assertCookie(t, cookies[sessionCookie], true, test.secure)
			assertCookie(t, cookies[csrfCookie], false, test.secure)
			currentMember := newRequest(t, http.MethodGet, "/api/v1/auth/me", "")
			currentMember.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookies[sessionCookie].Value}) //nolint:gosec // G124: a request cookie carries no attributes.
			currentMember.Header.Set(httpapi.EnvironmentHeader, environmentName)
			memberRecorder := harness.serve(currentMember)
			if memberRecorder.Code != http.StatusOK {
				t.Fatalf("GET /api/v1/auth/me with the setup session = %d, want 200, body %s", memberRecorder.Code, memberRecorder.Body.String())
			}
			if lastLoginAt := decodeBody(t, memberRecorder)["last_login_at"]; lastLoginAt != wantBody["last_login_at"] {
				t.Errorf("last_login_at of GET /api/v1/auth/me = %v, want %v", lastLoginAt, wantBody["last_login_at"])
			}
			if harness.setupRequired(t) {
				t.Error("setup status after setup = required, want not required")
			}
			storedHash, completedAt := harness.readInstallation(t)
			if storedHash != nil || completedAt == nil || !completedAt.Equal(testStart) {
				t.Errorf("installation setup_token_hash=%x setup_completed_at=%v, want null and %s", storedHash, completedAt, testStart)
			}
		})
	}
}

func TestSetupRejectsWrongToken(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.prepareSetupToken(t)

	recorder := harness.completeSetup(t, "not-the-setup-token")

	assertProblem(t, recorder, http.StatusForbidden, "setup_token_invalid")
	if cookies := recorder.Header().Values("Set-Cookie"); len(cookies) != 0 {
		t.Errorf("rejected setup set cookies %v", cookies)
	}
	if !harness.setupRequired(t) {
		t.Error("setup status after a wrong token = not required, want required")
	}
}

func TestSetupRejectsTokenBeforeLinkPrepared(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)

	assertProblem(t, harness.completeSetup(t, ""), http.StatusForbidden, "setup_token_invalid")
}

func TestSetupAfterSetupIsNotAvailable(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	token := harness.prepareSetupToken(t)
	if recorder := harness.completeSetup(t, token); recorder.Code != http.StatusOK {
		t.Fatalf("first setup status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}

	assertProblem(t, harness.completeSetup(t, token), http.StatusConflict, "setup_not_available")
}

func TestSetupWithInvalidMemberStaysPending(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	token := harness.prepareSetupToken(t)

	recorder := harness.serve(newRequest(t, http.MethodPost, "/api/v1/setup",
		`{"token":"`+token+`","email":"not an email","display_name":"Sam","password":"short"}`))

	assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed")
	if !harness.setupRequired(t) {
		t.Error("setup status after an invalid member = not required, want required")
	}
	if recorder := harness.completeSetup(t, token); recorder.Code != http.StatusOK {
		t.Errorf("setup with the same token = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
}

func assertCookie(t *testing.T, cookie *http.Cookie, httpOnly bool, secure bool) {
	t.Helper()
	if cookie == nil {
		t.Fatal("cookie missing")
	}
	if cookie.Value == "" || cookie.Path != "/" || cookie.HttpOnly != httpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != sessionMaxAge {
		t.Errorf("cookie %s = %+v, want a value, path /, http_only=%t, secure=%t, SameSite=Lax, max_age=%d", cookie.Name, cookie, httpOnly, secure, sessionMaxAge)
	}
}
