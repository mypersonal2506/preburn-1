package members_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	bulkRowCount        = 2500
	linkRetention       = 7 * 24 * time.Hour
	sessionFixtureAgent = "preburn-test/1.0"
)

type linkFixture struct {
	expiresAt  time.Time
	consumedAt *time.Time
	deleted    bool
}

func TestCleanupJobDeclaresKindAndQueue(t *testing.T) {
	t.Parallel()
	arguments := members.CleanupArgs{}

	if kind := arguments.Kind(); kind != "member_cleanup" {
		t.Errorf("kind = %s, want member_cleanup", kind)
	}
	if queue := arguments.InsertOpts().Queue; queue != jobs.QueueMaintenance {
		t.Errorf("queue = %s, want %s", queue, jobs.QueueMaintenance)
	}
}

func TestCleanupJobDeletesExpiredSessionsAndOldLinks(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, pointer(testPassword))
	now := testStart
	cutoff := now.Add(-linkRetention)
	sessionExpiries := map[string]time.Time{
		"expired an hour ago": now.Add(-time.Hour),
		"expiring now":        now,
		"live":                now.Add(time.Second),
		"live for a month":    now.Add(30 * 24 * time.Hour),
	}
	sessionDeleted := map[string]bool{"expired an hour ago": true, "expiring now": true}
	var keptSessionHashes [][]byte
	for name, expiresAt := range sessionExpiries {
		tokenHash := secrets.HashToken(name)
		insertSession(t, harness, member.ID, tokenHash, expiresAt)
		if !sessionDeleted[name] {
			keptSessionHashes = append(keptSessionHashes, tokenHash)
		}
	}
	insertBulkExpiredSessions(t, harness, member.ID, now.Add(-time.Minute))
	links := map[string]linkFixture{
		"expired 8 days ago":       {expiresAt: now.Add(-8 * 24 * time.Hour), deleted: true},
		"expired 7 days ago":       {expiresAt: cutoff},
		"expired 6 days ago":       {expiresAt: now.Add(-6 * 24 * time.Hour)},
		"consumed 8 days ago":      {expiresAt: now.Add(time.Hour), consumedAt: pointer(now.Add(-8 * 24 * time.Hour)), deleted: true},
		"consumed 6 days ago":      {expiresAt: now.Add(-5 * 24 * time.Hour), consumedAt: pointer(now.Add(-6 * 24 * time.Hour))},
		"unconsumed and unexpired": {expiresAt: now.Add(time.Hour)},
	}
	var keptLinkIDs []uuid.UUID
	for name, link := range links {
		linkID := insertLink(t, harness, member.ID, secrets.HashToken(name), link)
		if !link.deleted {
			keptLinkIDs = append(keptLinkIDs, linkID)
		}
	}
	insertBulkOldLinks(t, harness, member.ID, now.Add(-10*24*time.Hour))
	var logOutput bytes.Buffer
	worker := members.NewCleanupWorker(harness.pool, harness.clock, logging.New(&logOutput, slog.LevelDebug))

	err := worker.Work(t.Context(), &river.Job[members.CleanupArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: members.CleanupArgs{}})

	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	sortHashes := cmpopts.SortSlices(func(left, right []byte) bool { return bytes.Compare(left, right) < 0 })
	if diff := cmp.Diff(keptSessionHashes, remainingSessionHashes(t, harness), sortHashes); diff != "" {
		t.Errorf("remaining sessions mismatch (-want +got):\n%s", diff)
	}
	sortIDs := cmpopts.SortSlices(func(left, right uuid.UUID) bool { return left.String() < right.String() })
	if diff := cmp.Diff(keptLinkIDs, remainingLinkIDs(t, harness), sortIDs); diff != "" {
		t.Errorf("remaining links mismatch (-want +got):\n%s", diff)
	}
	var logLine map[string]any
	if err := json.Unmarshal(logOutput.Bytes(), &logLine); err != nil {
		t.Fatalf("decode log line %q: %v", logOutput.String(), err)
	}
	wantLog := map[string]any{"msg": "members.cleanup_completed", "deleted_sessions": float64(bulkRowCount + 2), "deleted_links": float64(bulkRowCount + 2)}
	if diff := cmp.Diff(wantLog, logLine, cmpopts.IgnoreMapEntries(func(key string, _ any) bool { return key == "time" || key == "level" })); diff != "" {
		t.Errorf("log line mismatch (-want +got):\n%s", diff)
	}
}

func insertSession(t *testing.T, harness *harness, memberID uuid.UUID, tokenHash []byte, expiresAt time.Time) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO member_sessions (session_token_hash, member_id, expires_at, last_seen_at, user_agent, ip_address) VALUES ($1, $2, $3, $3, $4, '192.0.2.1')",
		tokenHash, memberID, expiresAt, sessionFixtureAgent,
	)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func insertBulkExpiredSessions(t *testing.T, harness *harness, memberID uuid.UUID, expiresAt time.Time) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO member_sessions (session_token_hash, member_id, expires_at, last_seen_at, user_agent, ip_address)
		SELECT sha256(convert_to('bulk session ' || number, 'UTF8')), $1, $2, $2, $3, '192.0.2.1'
		FROM generate_series(1, $4::integer) AS number`,
		memberID, expiresAt, sessionFixtureAgent, bulkRowCount,
	)
	if err != nil {
		t.Fatalf("insert bulk sessions: %v", err)
	}
}

func insertLink(t *testing.T, harness *harness, memberID uuid.UUID, tokenHash []byte, link linkFixture) uuid.UUID {
	t.Helper()
	linkID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO member_links (member_link_id, member_id, purpose, token_hash, expires_at, consumed_at) VALUES ($1, $2, 'invite', $3, $4, $5)",
		linkID, memberID, tokenHash, link.expiresAt, link.consumedAt,
	)
	if err != nil {
		t.Fatalf("insert link: %v", err)
	}
	return linkID
}

func insertBulkOldLinks(t *testing.T, harness *harness, memberID uuid.UUID, expiresAt time.Time) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO member_links (member_link_id, member_id, purpose, token_hash, expires_at)
		SELECT gen_random_uuid(), $1, 'password_reset', sha256(convert_to('bulk link ' || number, 'UTF8')), $2
		FROM generate_series(1, $3::integer) AS number`,
		memberID, expiresAt, bulkRowCount,
	)
	if err != nil {
		t.Fatalf("insert bulk links: %v", err)
	}
}

func remainingSessionHashes(t *testing.T, harness *harness) [][]byte {
	t.Helper()
	rows, err := harness.pool.Query(t.Context(), "SELECT session_token_hash FROM member_sessions")
	if err != nil {
		t.Fatalf("select sessions: %v", err)
	}
	var hashes [][]byte
	for rows.Next() {
		var hash []byte
		if err := rows.Scan(&hash); err != nil {
			t.Fatalf("scan session: %v", err)
		}
		hashes = append(hashes, hash)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	return hashes
}

func remainingLinkIDs(t *testing.T, harness *harness) []uuid.UUID {
	t.Helper()
	rows, err := harness.pool.Query(t.Context(), "SELECT member_link_id FROM member_links")
	if err != nil {
		t.Fatalf("select links: %v", err)
	}
	var linkIDs []uuid.UUID
	for rows.Next() {
		var linkID uuid.UUID
		if err := rows.Scan(&linkID); err != nil {
			t.Fatalf("scan link: %v", err)
		}
		linkIDs = append(linkIDs, linkID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read links: %v", err)
	}
	return linkIDs
}
