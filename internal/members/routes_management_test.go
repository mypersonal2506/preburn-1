package members_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	membersPath        = "/api/v1/members"
	inspectLinkPath    = "/api/v1/auth/links/inspect"
	consumeLinkPath    = "/api/v1/auth/links/consume"
	linkURLPrefix      = insecureURL + "/link#"
	inviteeEmail       = "jordan@example.com"
	inviteeDisplayName = "Jordan Lee"
	linkLifetime       = 24 * time.Hour
)

type storedLink struct {
	MemberID          uuid.UUID
	Purpose           string
	ExpiresAt         time.Time
	ConsumedAt        *time.Time
	CreatedByMemberID *uuid.UUID
}

func TestAddMemberInviteLinkSetsPasswordOnce(t *testing.T) {
	t.Parallel()
	harness := newManagementHarness(t)
	actor := harness.createMember(t, testEmail, pointer(testPassword))
	actorBrowser := harness.signIn(t)

	recorder := harness.serve(actorBrowser.request(t, http.MethodPost, membersPath, fmt.Sprintf(`{"email":%q,"display_name":%q}`, inviteeEmail, inviteeDisplayName)))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	invitee := body["member"].(map[string]any)
	wantInvitee := map[string]any{
		"id":            invitee["id"],
		"email":         inviteeEmail,
		"display_name":  inviteeDisplayName,
		"status":        "active",
		"has_password":  false,
		"last_login_at": nil,
		"created_at":    testStart.Format(time.RFC3339),
	}
	if diff := cmp.Diff(wantInvitee, invitee); diff != "" {
		t.Errorf("member mismatch (-want +got):\n%s", diff)
	}
	inviteeID, err := identifiers.Decode(identifiers.PrefixMember, invitee["id"].(string))
	if err != nil {
		t.Fatalf("decode member id: %v", err)
	}
	token := linkToken(t, body["link_url"].(string))
	wantLink := storedLink{MemberID: inviteeID, Purpose: "invite", ExpiresAt: testStart.Add(linkLifetime), CreatedByMemberID: &actor.ID}
	if diff := cmp.Diff(wantLink, readLink(t, harness, token)); diff != "" {
		t.Errorf("stored link mismatch (-want +got):\n%s", diff)
	}

	inspected := harness.serve(newRequest(t, http.MethodPost, inspectLinkPath, fmt.Sprintf(`{"token":%q}`, token)))

	if inspected.Code != http.StatusOK {
		t.Fatalf("inspect status = %d, want 200, body %s", inspected.Code, inspected.Body.String())
	}
	wantDetails := map[string]any{"purpose": "invite", "email": inviteeEmail, "display_name": inviteeDisplayName}
	if diff := cmp.Diff(wantDetails, decodeBody(t, inspected)); diff != "" {
		t.Errorf("link details mismatch (-want +got):\n%s", diff)
	}

	consumed := consumeLink(t, harness, token, newPassword)

	if consumed.Code != http.StatusOK {
		t.Fatalf("consume status = %d, want 200, body %s", consumed.Code, consumed.Body.String())
	}
	wantInvitee["has_password"] = true
	wantInvitee["last_login_at"] = testStart.Format(time.RFC3339)
	if diff := cmp.Diff(wantInvitee, decodeBody(t, consumed)); diff != "" {
		t.Errorf("consumed member mismatch (-want +got):\n%s", diff)
	}
	cookies := responseCookies(t, consumed)
	assertCookie(t, cookies[sessionCookie], true, false)
	assertCookie(t, cookies[csrfCookie], false, false)
	inviteeBrowser := browser{sessionToken: cookies[sessionCookie].Value, csrfToken: cookies[csrfCookie].Value}
	if status := harness.sessionStatus(t, inviteeBrowser); status != http.StatusOK {
		t.Errorf("invitee session status = %d, want 200", status)
	}
	if consumedAt := readLink(t, harness, token).ConsumedAt; consumedAt == nil || !consumedAt.Equal(testStart) {
		t.Errorf("consumed_at = %v, want %s", consumedAt, testStart)
	}
	if login := harness.login(t, inviteeEmail, newPassword); login.Code != http.StatusOK {
		t.Errorf("invitee login status = %d, want 200, body %s", login.Code, login.Body.String())
	}
	assertProblem(t, consumeLink(t, harness, token, "another long passphrase"), http.StatusGone, "link_expired")
	assertProblem(t, inspectLink(t, harness, token), http.StatusGone, "link_expired")
}

func TestAddMemberRejectsInvalidAndTakenEmails(t *testing.T) {
	t.Parallel()
	harness := newManagementHarness(t)
	harness.createMember(t, testEmail, pointer(testPassword))
	browser := harness.signIn(t)

	taken := harness.serve(browser.request(t, http.MethodPost, membersPath, fmt.Sprintf(`{"email":"SAM@example.com","display_name":%q}`, inviteeDisplayName)))
	invalid := harness.serve(browser.request(t, http.MethodPost, membersPath, `{"email":"not an email","display_name":""}`))

	assertProblem(t, taken, http.StatusConflict, "member_email_taken")
	assertLocations(t, assertProblem(t, invalid, http.StatusUnprocessableEntity, "validation_failed"), "body.email", "body.display_name")
	var linkCount int
	if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM member_links").Scan(&linkCount); err != nil {
		t.Fatalf("count links: %v", err)
	}
	if linkCount != 0 {
		t.Errorf("links = %d, want 0", linkCount)
	}
}

func TestLinkExpiresAfter24Hours(t *testing.T) {
	t.Parallel()
	harness := newManagementHarness(t)
	actor := harness.createMember(t, testEmail, pointer(testPassword))
	_, linkURL, err := harness.service.Add(t.Context(), inviteeEmail, inviteeDisplayName, actor.ID)
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	token := linkToken(t, linkURL)

	harness.clock.Advance(23 * time.Hour)
	if recorder := inspectLink(t, harness, token); recorder.Code != http.StatusOK {
		t.Errorf("inspect after 23 hours status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	harness.clock.Advance(2 * time.Hour)

	assertProblem(t, inspectLink(t, harness, token), http.StatusGone, "link_expired")
	assertProblem(t, consumeLink(t, harness, token, newPassword), http.StatusGone, "link_expired")
	if login := harness.login(t, inviteeEmail, newPassword); login.Code != http.StatusUnauthorized {
		t.Errorf("invitee login status = %d, want 401", login.Code)
	}
}

func TestConsumeLinkRecordsLastLogin(t *testing.T) {
	t.Parallel()
	loginAt := testStart.Add(5*time.Minute + 123456789*time.Nanosecond)
	wantLastLogin := loginAt.Truncate(time.Microsecond).Format(time.RFC3339Nano)
	cases := []struct {
		name string
		link func(t *testing.T, harness *harness, actor browser) *httptest.ResponseRecorder
	}{
		{
			name: "invite",
			link: func(t *testing.T, harness *harness, actor browser) *httptest.ResponseRecorder {
				t.Helper()
				return harness.serve(actor.request(t, http.MethodPost, membersPath, fmt.Sprintf(`{"email":%q,"display_name":%q}`, inviteeEmail, inviteeDisplayName)))
			},
		},
		{
			name: "password reset",
			link: func(t *testing.T, harness *harness, actor browser) *httptest.ResponseRecorder {
				t.Helper()
				target := harness.createMember(t, inviteeEmail, pointer(testPassword))
				return harness.serve(actor.request(t, http.MethodPost, memberPath(target.ID)+"/reset-link", ""))
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			harness := newManagementHarness(t)
			harness.createMember(t, testEmail, pointer(testPassword))
			created := testCase.link(t, harness, harness.signIn(t))
			if created.Code != http.StatusCreated {
				t.Fatalf("link status = %d, want 201, body %s", created.Code, created.Body.String())
			}
			harness.clock.Set(loginAt)

			consumed := consumeLink(t, harness, linkToken(t, decodeBody(t, created)["link_url"].(string)), newPassword)

			if consumed.Code != http.StatusOK {
				t.Fatalf("consume status = %d, want 200, body %s", consumed.Code, consumed.Body.String())
			}
			cookies := responseCookies(t, consumed)
			signedIn := browser{sessionToken: cookies[sessionCookie].Value, csrfToken: cookies[csrfCookie].Value}
			currentMember := harness.serve(signedIn.request(t, http.MethodGet, "/api/v1/auth/me", ""))
			lastLogins := []any{decodeBody(t, consumed)["last_login_at"], decodeBody(t, currentMember)["last_login_at"]}
			if diff := cmp.Diff([]any{wantLastLogin, wantLastLogin}, lastLogins); diff != "" {
				t.Errorf("last_login_at of the consume response and GET /api/v1/auth/me mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResetLinkReplacesEarlierResetLink(t *testing.T) {
	t.Parallel()
	harness := newManagementHarness(t)
	harness.createMember(t, testEmail, pointer(testPassword))
	target := harness.createMember(t, inviteeEmail, pointer(testPassword))
	actorBrowser := harness.signIn(t)
	targetBrowser := signInAs(t, harness, inviteeEmail, testPassword)
	resetPath := memberPath(target.ID) + "/reset-link"

	first := harness.serve(actorBrowser.request(t, http.MethodPost, resetPath, ""))
	second := harness.serve(actorBrowser.request(t, http.MethodPost, resetPath, ""))

	for _, recorder := range []*httptest.ResponseRecorder{first, second} {
		if recorder.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201, body %s", recorder.Code, recorder.Body.String())
		}
	}
	firstToken := linkToken(t, decodeBody(t, first)["link_url"].(string))
	secondToken := linkToken(t, decodeBody(t, second)["link_url"].(string))
	assertProblem(t, inspectLink(t, harness, firstToken), http.StatusGone, "link_expired")
	inspected := inspectLink(t, harness, secondToken)
	if inspected.Code != http.StatusOK {
		t.Fatalf("inspect status = %d, want 200, body %s", inspected.Code, inspected.Body.String())
	}
	if purpose := decodeBody(t, inspected)["purpose"]; purpose != "password_reset" {
		t.Errorf("purpose = %v, want password_reset", purpose)
	}

	if recorder := consumeLink(t, harness, secondToken, newPassword); recorder.Code != http.StatusOK {
		t.Fatalf("consume status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}

	if status := harness.sessionStatus(t, targetBrowser); status != http.StatusUnauthorized {
		t.Errorf("session from before the reset status = %d, want 401", status)
	}
	assertProblem(t, harness.login(t, inviteeEmail, testPassword), http.StatusUnauthorized, "login_failed")
	if login := harness.login(t, inviteeEmail, newPassword); login.Code != http.StatusOK {
		t.Errorf("login with the new password status = %d, want 200", login.Code)
	}
	if status := harness.sessionStatus(t, actorBrowser); status != http.StatusOK {
		t.Errorf("actor session status = %d, want 200", status)
	}
}

func TestRemoveMember(t *testing.T) {
	t.Parallel()
	harness := newManagementHarness(t)
	actor := harness.createMember(t, testEmail, pointer(testPassword))
	target := harness.createMember(t, inviteeEmail, pointer(testPassword))
	actorBrowser := harness.signIn(t)
	targetBrowser := signInAs(t, harness, inviteeEmail, testPassword)

	removeSelf := harness.serve(actorBrowser.request(t, http.MethodDelete, memberPath(actor.ID), ""))
	removeTarget := harness.serve(actorBrowser.request(t, http.MethodDelete, memberPath(target.ID), ""))

	assertProblem(t, removeSelf, http.StatusConflict, "last_member")
	if removeTarget.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body %s", removeTarget.Code, removeTarget.Body.String())
	}
	if status := harness.sessionStatus(t, targetBrowser); status != http.StatusUnauthorized {
		t.Errorf("removed member session status = %d, want 401", status)
	}
	var sessionCount int
	if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM member_sessions WHERE member_id = $1", target.ID).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionCount != 0 {
		t.Errorf("removed member sessions = %d, want 0", sessionCount)
	}
	assertProblem(t, harness.login(t, inviteeEmail, testPassword), http.StatusUnauthorized, "login_failed")
	removed, err := harness.service.Member(t.Context(), target.ID)
	if err != nil {
		t.Fatalf("load removed member: %v", err)
	}
	if removed.Status != members.StatusDisabled {
		t.Errorf("removed member status = %s, want disabled", removed.Status)
	}
	if status := harness.sessionStatus(t, actorBrowser); status != http.StatusOK {
		t.Errorf("actor session status = %d, want 200", status)
	}
}

func TestMemberRoutesAnswerNotFoundForUnknownMembers(t *testing.T) {
	t.Parallel()
	harness := newManagementHarness(t)
	harness.createMember(t, testEmail, pointer(testPassword))
	browser := harness.signIn(t)
	unknown := identifiers.Encode(identifiers.PrefixMember, identifiers.New())
	otherPrefix := identifiers.Encode(identifiers.PrefixAPIKey, identifiers.New())

	for _, memberID := range []string{unknown, otherPrefix, "mem_malformed"} {
		assertProblem(t, harness.serve(browser.request(t, http.MethodDelete, membersPath+"/"+memberID, "")), http.StatusNotFound, "not_found")
		assertProblem(t, harness.serve(browser.request(t, http.MethodPost, membersPath+"/"+memberID+"/reset-link", "")), http.StatusNotFound, "not_found")
	}
}

func TestResetLinkForDisabledMemberIsRejected(t *testing.T) {
	t.Parallel()
	harness := newManagementHarness(t)
	actor := harness.createMember(t, testEmail, pointer(testPassword))
	target := harness.createMember(t, inviteeEmail, pointer(testPassword))
	browser := harness.signIn(t)
	if err := harness.service.Remove(t.Context(), actor.ID, target.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	recorder := harness.serve(browser.request(t, http.MethodPost, memberPath(target.ID)+"/reset-link", ""))

	assertProblem(t, recorder, http.StatusConflict, "member_disabled")
}

func TestListMembersPages(t *testing.T) {
	t.Parallel()
	harness := newManagementHarness(t)
	var wantIDs []string
	for _, email := range []string{testEmail, inviteeEmail, "priya@example.com"} {
		member := harness.createMember(t, email, pointer(testPassword))
		wantIDs = append(wantIDs, identifiers.Encode(identifiers.PrefixMember, member.ID))
		harness.clock.Advance(time.Minute)
	}
	browser := harness.signIn(t)

	first := harness.serve(browser.request(t, http.MethodGet, membersPath+"?limit=2", ""))
	firstBody := decodeBody(t, first)
	nextCursor, hasNext := firstBody["next_cursor"].(string)
	if first.Code != http.StatusOK || !hasNext {
		t.Fatalf("first page status = %d, want 200 with a next cursor, body %s", first.Code, first.Body.String())
	}
	second := harness.serve(browser.request(t, http.MethodGet, membersPath+"?limit=2&cursor="+nextCursor, ""))
	secondBody := decodeBody(t, second)
	liveRequest := browser.request(t, http.MethodGet, membersPath+"?limit=2&cursor="+nextCursor, "")
	liveRequest.Header.Set(httpapi.EnvironmentHeader, string(httpapi.EnvironmentLive))
	assertProblem(t, harness.serve(liveRequest), http.StatusUnprocessableEntity, "invalid_cursor")
	defaultPage := decodeBody(t, harness.serve(browser.request(t, http.MethodGet, membersPath, "")))

	if second.Code != http.StatusOK || secondBody["next_cursor"] != nil {
		t.Fatalf("second page status = %d, want 200 without a next cursor, body %s", second.Code, second.Body.String())
	}
	pagedIDs := append(memberIDs(t, firstBody), memberIDs(t, secondBody)...)
	if diff := cmp.Diff(wantIDs, pagedIDs); diff != "" {
		t.Errorf("paged member ids mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantIDs, memberIDs(t, defaultPage)); diff != "" {
		t.Errorf("default page member ids mismatch (-want +got):\n%s", diff)
	}
	if defaultPage["next_cursor"] != nil {
		t.Errorf("default page next_cursor = %v, want null", defaultPage["next_cursor"])
	}
	firstMember := firstBody["items"].([]any)[0].(map[string]any)
	wantFirst := map[string]any{
		"id":            wantIDs[0],
		"email":         testEmail,
		"display_name":  testDisplayName,
		"status":        "active",
		"has_password":  true,
		"last_login_at": testStart.Add(3 * time.Minute).Format(time.RFC3339),
		"created_at":    testStart.Format(time.RFC3339),
	}
	if diff := cmp.Diff(wantFirst, firstMember); diff != "" {
		t.Errorf("first member mismatch (-want +got):\n%s", diff)
	}
	if lastLoginAt, present := firstBody["items"].([]any)[1].(map[string]any)["last_login_at"]; !present || lastLoginAt != nil {
		t.Errorf("last_login_at of a member who never logged in = %v, present %t, want null", lastLoginAt, present)
	}
	assertProblem(t, harness.serve(browser.request(t, http.MethodGet, membersPath+"?cursor=not-a-cursor", "")), http.StatusUnprocessableEntity, "invalid_cursor")
	assertProblem(t, harness.serve(browser.request(t, http.MethodGet, membersPath+"?limit=101", "")), http.StatusUnprocessableEntity, "validation_failed")
}

func TestMemberManagementRoutesRequireSession(t *testing.T) {
	t.Parallel()
	harness := newManagementHarness(t)

	for _, request := range []*http.Request{
		newRequest(t, http.MethodGet, membersPath, ""),
		newRequest(t, http.MethodPost, membersPath, fmt.Sprintf(`{"email":%q,"display_name":%q}`, inviteeEmail, inviteeDisplayName)),
		newRequest(t, http.MethodDelete, memberPath(identifiers.New()), ""),
		newRequest(t, http.MethodPost, memberPath(identifiers.New())+"/reset-link", ""),
	} {
		assertProblem(t, harness.serve(request), http.StatusUnauthorized, "authentication_required")
	}
}

func newManagementHarness(t *testing.T) *harness {
	t.Helper()
	harness := newHarness(t, insecureURL)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logging.New(t.Output(), slog.LevelDebug), members.NewSessionAuthenticator(harness.service.Sessions()))
	members.RegisterRoutes(api, harness.service)
	members.RegisterManagementRoutes(api, harness.service)
	harness.handler = mux
	return harness
}

func signInAs(t *testing.T, harness *harness, email string, password string) browser {
	t.Helper()
	recorder := harness.login(t, email, password)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login %s status = %d, want 200, body %s", email, recorder.Code, recorder.Body.String())
	}
	cookies := responseCookies(t, recorder)
	return browser{sessionToken: cookies[sessionCookie].Value, csrfToken: cookies[csrfCookie].Value}
}

func inspectLink(t *testing.T, harness *harness, token string) *httptest.ResponseRecorder {
	t.Helper()
	return harness.serve(newRequest(t, http.MethodPost, inspectLinkPath, fmt.Sprintf(`{"token":%q}`, token)))
}

func consumeLink(t *testing.T, harness *harness, token string, password string) *httptest.ResponseRecorder {
	t.Helper()
	return harness.serve(newRequest(t, http.MethodPost, consumeLinkPath, fmt.Sprintf(`{"token":%q,"password":%q}`, token, password)))
}

func linkToken(t *testing.T, linkURL string) string {
	t.Helper()
	token, found := strings.CutPrefix(linkURL, linkURLPrefix)
	if !found || token == "" {
		t.Fatalf("link URL %q does not start with %s followed by a token", linkURL, linkURLPrefix)
	}
	return token
}

func readLink(t *testing.T, harness *harness, token string) storedLink {
	t.Helper()
	var link storedLink
	err := harness.pool.QueryRow(t.Context(),
		"SELECT member_id, purpose, expires_at, consumed_at, created_by_member_id FROM member_links WHERE token_hash = $1",
		secrets.HashToken(token),
	).Scan(&link.MemberID, &link.Purpose, &link.ExpiresAt, &link.ConsumedAt, &link.CreatedByMemberID)
	if err != nil {
		t.Fatalf("read link: %v", err)
	}
	link.ExpiresAt = link.ExpiresAt.UTC()
	return link
}

func memberPath(memberID uuid.UUID) string {
	return membersPath + "/" + identifiers.Encode(identifiers.PrefixMember, memberID)
}

func memberIDs(t *testing.T, page map[string]any) []string {
	t.Helper()
	var pageIDs []string
	for _, item := range page["items"].([]any) {
		pageIDs = append(pageIDs, item.(map[string]any)["id"].(string))
	}
	return pageIDs
}
