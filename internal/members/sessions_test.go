package members_test

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/secrets"
)

const sessionLifetime = 30 * 24 * time.Hour

type sessionRow struct {
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  string
	IPAddress  string
}

var clientAddress = netip.MustParseAddr("198.51.100.9")

func TestSessionSlidesExpiry(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, nil)
	sessions := harness.service.Sessions()
	token, err := sessions.Start(t.Context(), member.ID, "preburn-test/1.0", clientAddress)
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	started := sessionRow{LastSeenAt: testStart, ExpiresAt: testStart.Add(sessionLifetime), UserAgent: "preburn-test/1.0", IPAddress: clientAddress.String()}
	if diff := cmp.Diff(started, readSession(t, harness, token)); diff != "" {
		t.Fatalf("started session mismatch (-want +got):\n%s", diff)
	}

	harness.clock.Advance(10*time.Minute - time.Second)
	authenticated, err := sessions.Authenticate(t.Context(), token)
	if err != nil {
		t.Fatalf("authenticate within 10 minutes: %v", err)
	}
	if diff := cmp.Diff(member, authenticated); diff != "" {
		t.Errorf("authenticated member mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(started, readSession(t, harness, token)); diff != "" {
		t.Errorf("session written within 10 minutes (-want +got):\n%s", diff)
	}

	harness.clock.Advance(time.Second)
	if _, err := sessions.Authenticate(t.Context(), token); err != nil {
		t.Fatalf("authenticate after 10 minutes: %v", err)
	}
	touchedAt := testStart.Add(10 * time.Minute)
	touched := sessionRow{LastSeenAt: touchedAt, ExpiresAt: touchedAt.Add(sessionLifetime), UserAgent: "preburn-test/1.0", IPAddress: clientAddress.String()}
	if diff := cmp.Diff(touched, readSession(t, harness, token)); diff != "" {
		t.Errorf("touched session mismatch (-want +got):\n%s", diff)
	}

	harness.clock.Set(touchedAt.Add(sessionLifetime))
	if _, err := sessions.Authenticate(t.Context(), token); !errors.Is(err, httpapi.ErrAuthenticationRequired) {
		t.Errorf("authenticate at expiry error = %v, want ErrAuthenticationRequired", err)
	}
}

func TestSessionStoresSanitizedUserAgent(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, nil)
	userAgent := "agent\xff" + strings.Repeat("é", 400)

	token, err := harness.service.Sessions().Start(t.Context(), member.ID, userAgent, clientAddress)
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	stored := readSession(t, harness, token).UserAgent
	if want := "agent" + strings.Repeat("é", 253); stored != want {
		t.Errorf("user agent = %q (%d bytes), want %d bytes of valid text", stored, len(stored), len(want))
	}
}

func TestSessionEndAndEndOthers(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	sessions := harness.service.Sessions()
	member := harness.createMember(t, testEmail, nil)
	otherMember := harness.createMember(t, "jordan@example.com", nil)
	tokens := map[string]string{}
	for _, name := range []string{"ended", "kept", "other", "other member"} {
		owner := member
		if name == "other member" {
			owner = otherMember
		}
		token, err := sessions.Start(t.Context(), owner.ID, "", clientAddress)
		if err != nil {
			t.Fatalf("start session %s: %v", name, err)
		}
		tokens[name] = token
	}

	if err := sessions.End(t.Context(), tokens["ended"]); err != nil {
		t.Fatalf("end session: %v", err)
	}
	if err := sessions.EndOthers(t.Context(), member.ID, tokens["kept"]); err != nil {
		t.Fatalf("end other sessions: %v", err)
	}

	for name, wantValid := range map[string]bool{"ended": false, "kept": true, "other": false, "other member": true} {
		_, err := sessions.Authenticate(t.Context(), tokens[name])
		if valid := err == nil; valid != wantValid {
			t.Errorf("session %s valid=%t err=%v, want valid=%t", name, valid, err, wantValid)
		}
	}
}

func TestSessionOfDisabledMemberIsRejected(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, nil)
	token, err := harness.service.Sessions().Start(t.Context(), member.ID, "", clientAddress)
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if _, err := harness.pool.Exec(t.Context(), "UPDATE members SET status = 'disabled' WHERE member_id = $1", member.ID); err != nil {
		t.Fatalf("disable member: %v", err)
	}

	if _, err := harness.service.Sessions().Authenticate(t.Context(), token); !errors.Is(err, httpapi.ErrAuthenticationRequired) {
		t.Errorf("error = %v, want ErrAuthenticationRequired", err)
	}
}

func readSession(t *testing.T, harness *harness, token string) sessionRow {
	t.Helper()
	var row sessionRow
	err := harness.pool.QueryRow(t.Context(),
		"SELECT last_seen_at, expires_at, user_agent, host(ip_address) FROM member_sessions WHERE session_token_hash = $1",
		secrets.HashToken(token),
	).Scan(&row.LastSeenAt, &row.ExpiresAt, &row.UserAgent, &row.IPAddress)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	row.LastSeenAt = row.LastSeenAt.UTC()
	row.ExpiresAt = row.ExpiresAt.UTC()
	return row
}
