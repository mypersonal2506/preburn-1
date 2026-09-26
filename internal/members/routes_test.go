package members_test

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/crypto/argon2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	newPassword        = "a much longer passphrase"
	sessionMaxAge      = int(sessionLifetime / time.Second)
	currentHashPrefix  = "$argon2id$v=19$m=65536,t=3,p=2$"
	testDisplayName    = "Sam Rivera"
	defaultRemoteAddr  = "192.0.2.1"
	forwardedClientIP  = "198.51.100.9"
	proxyRemoteAddress = "10.0.0.2:51000"
)

func TestLoginSetsSessionCookies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		publicURL string
		secure    bool
	}{
		{publicURL: insecureURL, secure: false},
		{publicURL: "https://preburn.example.com", secure: true},
	}
	for _, test := range tests {
		t.Run(test.publicURL, func(t *testing.T) {
			harness := newHarness(t, test.publicURL)
			member := harness.createMember(t, testEmail, pointer(testPassword))

			recorder := harness.login(t, "SAM@example.com", testPassword)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
			}
			wantBody := map[string]any{
				"id":            identifiers.Encode(identifiers.PrefixMember, member.ID),
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
			browser := browser{sessionToken: cookies[sessionCookie].Value, csrfToken: cookies[csrfCookie].Value}
			if status := harness.sessionStatus(t, browser); status != http.StatusOK {
				t.Errorf("session status = %d, want 200", status)
			}
			if session := readSession(t, harness, browser.sessionToken); session.IPAddress != defaultRemoteAddr {
				t.Errorf("session ip_address = %s, want %s", session.IPAddress, defaultRemoteAddr)
			}
			var lastLoginAt time.Time
			if err := harness.pool.QueryRow(t.Context(), "SELECT last_login_at FROM members WHERE member_id = $1", member.ID).Scan(&lastLoginAt); err != nil {
				t.Fatalf("read last_login_at: %v", err)
			}
			if !lastLoginAt.Equal(testStart) {
				t.Errorf("last_login_at = %s, want %s", lastLoginAt, testStart)
			}
		})
	}
}

func TestLoginResponseLastLoginMatchesLaterReads(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))
	loginAt := testStart.Add(123456789 * time.Nanosecond)
	harness.clock.Set(loginAt)

	recorder := harness.login(t, testEmail, testPassword)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	cookies := responseCookies(t, recorder)
	signedIn := browser{sessionToken: cookies[sessionCookie].Value, csrfToken: cookies[csrfCookie].Value}
	currentMember := harness.serve(signedIn.request(t, http.MethodGet, "/api/v1/auth/me", ""))
	wantLastLogin := loginAt.Truncate(time.Microsecond).Format(time.RFC3339Nano)
	lastLogins := []any{decodeBody(t, recorder)["last_login_at"], decodeBody(t, currentMember)["last_login_at"]}
	if diff := cmp.Diff([]any{wantLastLogin, wantLastLogin}, lastLogins); diff != "" {
		t.Errorf("last_login_at of the login response and GET /api/v1/auth/me mismatch (-want +got):\n%s", diff)
	}
}

func TestLoginRecordsForwardedClientBehindTrustedProxy(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL, netip.MustParsePrefix("10.0.0.0/8"))
	harness.createMember(t, testEmail, pointer(testPassword))
	request := newRequest(t, http.MethodPost, "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, testEmail, testPassword))
	request.RemoteAddr = proxyRemoteAddress
	request.Header.Set("X-Forwarded-For", forwardedClientIP)
	request.Header.Set("User-Agent", "preburn-test/1.0")

	recorder := harness.serve(request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	session := readSession(t, harness, responseCookies(t, recorder)[sessionCookie].Value)
	if session.IPAddress != forwardedClientIP || session.UserAgent != "preburn-test/1.0" {
		t.Errorf("session ip_address=%s user_agent=%q, want %s and preburn-test/1.0", session.IPAddress, session.UserAgent, forwardedClientIP)
	}
}

func TestLoginFailuresReturnIdenticalProblems(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))
	harness.createMember(t, "jordan@example.com", nil)

	wrongPassword := harness.login(t, testEmail, "not the right password")
	unknownEmail := harness.login(t, "nobody@example.com", testPassword)
	invitePending := harness.login(t, "jordan@example.com", testPassword)
	invalidEmail := harness.login(t, "sam\x00@example.com", testPassword)

	assertProblem(t, wrongPassword, http.StatusUnauthorized, "login_failed")
	for name, recorder := range map[string]*httptest.ResponseRecorder{"unknown email": unknownEmail, "invite pending": invitePending, "email with a NUL character": invalidEmail} {
		if recorder.Code != wrongPassword.Code || recorder.Body.String() != wrongPassword.Body.String() {
			t.Errorf("%s answered %d %s, want the wrong password answer %d %s", name, recorder.Code, recorder.Body.String(), wrongPassword.Code, wrongPassword.Body.String())
		}
	}
	for _, recorder := range []*httptest.ResponseRecorder{wrongPassword, unknownEmail, invitePending, invalidEmail} {
		if cookies := recorder.Header().Values("Set-Cookie"); len(cookies) != 0 {
			t.Errorf("failed login set cookies %v", cookies)
		}
	}
}

func TestDisabledMemberCannotLogIn(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, pointer(testPassword))
	if _, err := harness.pool.Exec(t.Context(), "UPDATE members SET status = 'disabled' WHERE member_id = $1", member.ID); err != nil {
		t.Fatalf("disable member: %v", err)
	}

	recorder := harness.login(t, testEmail, testPassword)

	assertProblem(t, recorder, http.StatusUnauthorized, "login_failed")
}

func TestLoginRateLimitPerEmail(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))

	for attempt := 1; attempt <= 10; attempt++ {
		request := newRequest(t, http.MethodPost, "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"not the right password"}`, testEmail))
		request.RemoteAddr = fmt.Sprintf("203.0.113.%d:51000", attempt)
		assertProblem(t, harness.serve(request), http.StatusUnauthorized, "login_failed")
	}
	limited := harness.login(t, "Sam@Example.com", testPassword)

	assertProblem(t, limited, http.StatusTooManyRequests, "rate_limited")
	if retryAfter := limited.Header().Get("Retry-After"); retryAfter != "900" {
		t.Errorf("Retry-After = %q, want 900", retryAfter)
	}

	harness.clock.Advance(15 * time.Minute)
	if recorder := harness.login(t, testEmail, testPassword); recorder.Code != http.StatusOK {
		t.Errorf("login in the next window status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestLoginRateLimitPerClientIP(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))

	for attempt := 1; attempt <= 10; attempt++ {
		assertProblem(t, harness.login(t, fmt.Sprintf("nobody%d@example.com", attempt), testPassword), http.StatusUnauthorized, "login_failed")
	}
	limited := harness.login(t, testEmail, testPassword)

	assertProblem(t, limited, http.StatusTooManyRequests, "rate_limited")
	if retryAfter := limited.Header().Get("Retry-After"); retryAfter != "900" {
		t.Errorf("Retry-After = %q, want 900", retryAfter)
	}
}

func TestLogoutEndsSession(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))
	browser := harness.signIn(t)

	recorder := harness.serve(browser.request(t, http.MethodPost, "/api/v1/auth/logout", ""))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body %s", recorder.Code, recorder.Body.String())
	}
	cookies := responseCookies(t, recorder)
	for _, name := range []string{sessionCookie, csrfCookie} {
		if cookie := cookies[name]; cookie == nil || cookie.Value != "" || cookie.MaxAge >= 0 {
			t.Errorf("cookie %s = %v, want an expired empty cookie", name, cookie)
		}
	}
	assertProblem(t, harness.serve(browser.request(t, http.MethodGet, "/api/v1/auth/me", "")), http.StatusUnauthorized, "authentication_required")
}

func TestUnsafeMethodsRequireCSRFToken(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))
	browser := harness.signIn(t)
	tests := []struct {
		name    string
		prepare func(request *http.Request)
	}{
		{name: "missing header", prepare: func(request *http.Request) { request.Header.Del(csrfHeader) }},
		{name: "wrong header", prepare: func(request *http.Request) { request.Header.Set(csrfHeader, browser.csrfToken+"x") }},
		{name: "missing cookie", prepare: func(request *http.Request) {
			request.Header.Del("Cookie")
			addCookie(request, sessionCookie, browser.sessionToken)
		}},
		{name: "empty header and cookie", prepare: func(request *http.Request) {
			request.Header.Del("Cookie")
			addCookie(request, sessionCookie, browser.sessionToken)
			request.Header.Set(csrfHeader, "")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := browser.request(t, http.MethodPost, "/api/v1/auth/logout", "")
			test.prepare(request)

			assertProblem(t, harness.serve(request), http.StatusForbidden, "csrf_invalid")
		})
	}

	request := browser.request(t, http.MethodGet, "/api/v1/auth/me", "")
	request.Header.Del(csrfHeader)
	if recorder := harness.serve(request); recorder.Code != http.StatusOK {
		t.Errorf("GET without the header status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestSessionRequestsRequireEnvironmentHeader(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))
	browser := harness.signIn(t)

	for _, value := range []string{"", "production"} {
		request := browser.request(t, http.MethodGet, "/api/v1/auth/me", "")
		request.Header.Del(httpapi.EnvironmentHeader)
		if value != "" {
			request.Header.Set(httpapi.EnvironmentHeader, value)
		}

		assertProblem(t, harness.serve(request), http.StatusUnprocessableEntity, "environment_header_invalid")
	}
}

func TestSessionOnRuntimeRouteIsForbidden(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))
	browser := harness.signIn(t)

	assertProblem(t, harness.serve(browser.request(t, http.MethodGet, runtimeProbe, "")), http.StatusForbidden, "scope_forbidden")
	assertProblem(t, harness.serve(newRequest(t, http.MethodGet, runtimeProbe, "")), http.StatusUnauthorized, "authentication_required")
	assertProblem(t, harness.serve(newRequest(t, http.MethodGet, "/api/v1/auth/me", "")), http.StatusUnauthorized, "authentication_required")
}

func TestCurrentMemberRefreshesCookies(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, pointer(testPassword))
	browser := harness.signIn(t)

	withCSRF := harness.serve(browser.request(t, http.MethodGet, "/api/v1/auth/me", ""))
	withoutCSRF := browser.request(t, http.MethodGet, "/api/v1/auth/me", "")
	withoutCSRF.Header.Del("Cookie")
	addCookie(withoutCSRF, sessionCookie, browser.sessionToken)
	missingCSRF := harness.serve(withoutCSRF)

	for _, recorder := range []*httptest.ResponseRecorder{withCSRF, missingCSRF} {
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
		}
		if body := decodeBody(t, recorder); body["id"] != identifiers.Encode(identifiers.PrefixMember, member.ID) {
			t.Errorf("id = %v, want the signed-in member", body["id"])
		}
		cookies := responseCookies(t, recorder)
		assertCookie(t, cookies[sessionCookie], true, false)
		assertCookie(t, cookies[csrfCookie], false, false)
		if cookies[sessionCookie].Value != browser.sessionToken {
			t.Errorf("session cookie changed to %q", cookies[sessionCookie].Value)
		}
	}
	if value := responseCookies(t, withCSRF)[csrfCookie].Value; value != browser.csrfToken {
		t.Errorf("csrf cookie = %q, want the existing token", value)
	}
	if value := responseCookies(t, missingCSRF)[csrfCookie].Value; value == "" || value == browser.csrfToken {
		t.Errorf("csrf cookie = %q, want a new token", value)
	}
}

func TestUpdateDisplayName(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))
	browser := harness.signIn(t)

	recorder := harness.serve(browser.request(t, http.MethodPatch, "/api/v1/auth/me", `{"display_name":"Samantha Rivera"}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	if body := decodeBody(t, recorder); body["display_name"] != "Samantha Rivera" {
		t.Errorf("display_name = %v, want Samantha Rivera", body["display_name"])
	}
	if cookies := recorder.Header().Values("Set-Cookie"); len(cookies) != 0 {
		t.Errorf("display name change set cookies %v", cookies)
	}
	invalid := harness.serve(browser.request(t, http.MethodPatch, "/api/v1/auth/me", `{"display_name":""}`))
	problem := assertProblem(t, invalid, http.StatusUnprocessableEntity, "validation_failed")
	assertLocations(t, problem, "body.display_name")
}

func TestPasswordChangeRotatesSessions(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))
	changing := harness.signIn(t)
	other := harness.signIn(t)

	wrongCurrent := harness.serve(changing.request(t, http.MethodPatch, "/api/v1/auth/me", fmt.Sprintf(`{"password":{"current":"not the right password","new":%q}}`, newPassword)))
	tooShort := harness.serve(changing.request(t, http.MethodPatch, "/api/v1/auth/me", fmt.Sprintf(`{"password":{"current":%q,"new":"short"}}`, testPassword)))
	changed := harness.serve(changing.request(t, http.MethodPatch, "/api/v1/auth/me", fmt.Sprintf(`{"password":{"current":%q,"new":%q}}`, testPassword, newPassword)))

	assertLocations(t, assertProblem(t, wrongCurrent, http.StatusUnprocessableEntity, "validation_failed"), "body.password.current")
	assertLocations(t, assertProblem(t, tooShort, http.StatusUnprocessableEntity, "validation_failed"), "body.password.new")
	if changed.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", changed.Code, changed.Body.String())
	}
	cookies := responseCookies(t, changed)
	assertCookie(t, cookies[sessionCookie], true, false)
	assertCookie(t, cookies[csrfCookie], false, false)
	rotated := browser{sessionToken: cookies[sessionCookie].Value, csrfToken: cookies[csrfCookie].Value}
	if rotated.sessionToken == changing.sessionToken {
		t.Error("password change kept the session token")
	}
	for name, test := range map[string]struct {
		browser browser
		status  int
	}{
		"rotated session":  {browser: rotated, status: http.StatusOK},
		"changing session": {browser: changing, status: http.StatusUnauthorized},
		"other session":    {browser: other, status: http.StatusUnauthorized},
	} {
		if status := harness.sessionStatus(t, test.browser); status != test.status {
			t.Errorf("%s status = %d, want %d", name, status, test.status)
		}
	}
	assertProblem(t, harness.login(t, testEmail, testPassword), http.StatusUnauthorized, "login_failed")
	if recorder := harness.login(t, testEmail, newPassword); recorder.Code != http.StatusOK {
		t.Errorf("login with the new password status = %d, want 200", recorder.Code)
	}
}

func TestPasswordChangeRateLimitPerMember(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, pointer(testPassword))
	browser := harness.signIn(t)
	wrongCurrent := fmt.Sprintf(`{"password":{"current":"not the right password","new":%q}}`, newPassword)
	rightCurrent := fmt.Sprintf(`{"password":{"current":%q,"new":%q}}`, testPassword, newPassword)

	for attempt := 1; attempt <= 10; attempt++ {
		assertProblem(t, harness.serve(browser.request(t, http.MethodPatch, "/api/v1/auth/me", wrongCurrent)), http.StatusUnprocessableEntity, "validation_failed")
	}
	limited := harness.serve(browser.request(t, http.MethodPatch, "/api/v1/auth/me", wrongCurrent))
	limitedRight := harness.serve(browser.request(t, http.MethodPatch, "/api/v1/auth/me", rightCurrent))
	displayName := harness.serve(browser.request(t, http.MethodPatch, "/api/v1/auth/me", `{"display_name":"Samantha Rivera"}`))

	for _, recorder := range []*httptest.ResponseRecorder{limited, limitedRight} {
		assertProblem(t, recorder, http.StatusTooManyRequests, "rate_limited")
		if retryAfter := recorder.Header().Get("Retry-After"); retryAfter != "900" {
			t.Errorf("Retry-After = %q, want 900", retryAfter)
		}
	}
	if displayName.Code != http.StatusOK {
		t.Errorf("display name change status = %d, want 200, body %s", displayName.Code, displayName.Body.String())
	}

	harness.clock.Advance(15 * time.Minute)
	if recorder := harness.serve(browser.request(t, http.MethodPatch, "/api/v1/auth/me", rightCurrent)); recorder.Code != http.StatusOK {
		t.Errorf("password change in the next window status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestLoginUpgradesOlderPasswordHash(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, nil)
	olderHash := olderPasswordHash(testPassword)
	if _, err := harness.pool.Exec(t.Context(), "UPDATE members SET password_hash = $1 WHERE member_id = $2", olderHash, member.ID); err != nil {
		t.Fatalf("store older hash: %v", err)
	}

	if recorder := harness.login(t, testEmail, testPassword); recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}

	var storedHash string
	if err := harness.pool.QueryRow(t.Context(), "SELECT password_hash FROM members WHERE member_id = $1", member.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read password hash: %v", err)
	}
	if !strings.HasPrefix(storedHash, currentHashPrefix) {
		t.Fatalf("stored hash %q does not use the current parameters", storedHash)
	}
	matched, needsRehash, err := secrets.VerifyPassword(testPassword, storedHash)
	if err != nil || !matched || needsRehash {
		t.Errorf("upgraded hash matched=%t needs_rehash=%t err=%v, want a current match", matched, needsRehash, err)
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

func assertLocations(t *testing.T, problem map[string]any, want ...string) {
	t.Helper()
	var locations []string
	for _, fieldError := range problem["errors"].([]any) {
		locations = append(locations, fieldError.(map[string]any)["location"].(string))
	}
	if diff := cmp.Diff(want, locations); diff != "" {
		t.Errorf("locations mismatch (-want +got):\n%s", diff)
	}
}

func olderPasswordHash(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=2,p=1$%s$%s", 19*1024, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}
