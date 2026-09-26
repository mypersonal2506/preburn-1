package installation_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/installation"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	concurrentSetupDeadline     = 30 * time.Second
	concurrentSetupPollInterval = 10 * time.Millisecond
	concurrentSetupCalls        = 2
)

var setupTokenPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{43}$`)

func TestFreshInstallationRequiresSetup(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)

	setupRequired, err := harness.service.Status(t.Context())

	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !setupRequired {
		t.Error("status of a fresh installation = setup not required, want required")
	}
	if !harness.setupRequired(t) {
		t.Error("GET /api/v1/setup/status of a fresh installation = setup not required, want required")
	}
}

func TestPrepareSetupLinkPutsTokenInFragment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		publicURL string
		wantBase  string
	}{
		{publicURL: insecureURL, wantBase: "http://localhost:8080/setup#"},
		{publicURL: secureURL + "/preburn/", wantBase: secureURL + "/preburn/setup#"},
	}
	for _, test := range tests {
		t.Run(test.publicURL, func(t *testing.T) {
			t.Parallel()
			harness := newHarness(t, test.publicURL)

			link, pending, err := harness.service.PrepareSetupLink(t.Context())

			if err != nil {
				t.Fatalf("prepare setup link: %v", err)
			}
			if !pending {
				t.Fatal("pending = false, want true")
			}
			token := setupToken(t, link)
			if link != test.wantBase+token || !setupTokenPattern.MatchString(token) {
				t.Errorf("link = %q, want %s followed by a 43 character base64url token", link, test.wantBase)
			}
			if storedHash, _ := harness.readInstallation(t); !cmp.Equal(storedHash, secrets.HashToken(token)) {
				t.Errorf("stored setup_token_hash = %x, want the SHA-256 of the token", storedHash)
			}
		})
	}
}

func TestPrepareSetupLinkRotatesToken(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	firstToken := harness.prepareSetupToken(t)
	secondToken := harness.prepareSetupToken(t)

	_, err := harness.service.CompleteSetup(t.Context(), setupInput(firstToken, testEmail))

	if !errors.Is(err, installation.ErrSetupTokenInvalid) {
		t.Fatalf("complete setup with the first token: err = %v, want ErrSetupTokenInvalid", err)
	}
	if _, err := harness.service.CompleteSetup(t.Context(), setupInput(secondToken, testEmail)); err != nil {
		t.Fatalf("complete setup with the second token: %v", err)
	}
}

func TestPrepareSetupLinkAfterSetup(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	if _, err := harness.service.CompleteSetup(t.Context(), setupInput(harness.prepareSetupToken(t), testEmail)); err != nil {
		t.Fatalf("complete setup: %v", err)
	}

	link, pending, err := harness.service.PrepareSetupLink(t.Context())

	if err != nil || pending || link != "" {
		t.Errorf("prepare setup link after setup = %q, %t, %v, want an empty link, false and no error", link, pending, err)
	}
	if storedHash, _ := harness.readInstallation(t); storedHash != nil {
		t.Errorf("setup_token_hash after setup = %x, want null", storedHash)
	}
}

func TestCreateAdminCompletesPendingSetup(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	token := harness.prepareSetupToken(t)

	member, err := harness.service.CreateAdmin(t.Context(), testEmail, testDisplayName, testPassword)

	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if member.Email != testEmail || member.DisplayName != testDisplayName || !member.HasPassword || member.Status != members.StatusActive {
		t.Errorf("member = %+v, want an active member %s named %s with a password", member, testEmail, testDisplayName)
	}
	storedHash, completedAt := harness.readInstallation(t)
	if storedHash != nil || completedAt == nil || !completedAt.Equal(testStart) {
		t.Errorf("installation setup_token_hash=%x setup_completed_at=%v, want null and %s", storedHash, completedAt, testStart)
	}
	if setupRequired, err := harness.service.Status(t.Context()); err != nil || setupRequired {
		t.Errorf("status after create admin = %t, %v, want false and no error", setupRequired, err)
	}
	_, err = harness.service.CompleteSetup(t.Context(), setupInput(token, "jordan@example.com"))
	if !errors.Is(err, installation.ErrSetupNotAvailable) {
		t.Errorf("complete setup after create admin: err = %v, want ErrSetupNotAvailable", err)
	}
}

func TestCreateAdminAfterSetup(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	if _, err := harness.service.CompleteSetup(t.Context(), setupInput(harness.prepareSetupToken(t), testEmail)); err != nil {
		t.Fatalf("complete setup: %v", err)
	}

	member, err := harness.service.CreateAdmin(t.Context(), "jordan@example.com", "Jordan Lee", testPassword)

	if err != nil {
		t.Fatalf("create admin after setup: %v", err)
	}
	if member.Email != "jordan@example.com" || !member.HasPassword {
		t.Errorf("member = %+v, want jordan@example.com with a password", member)
	}
	if _, completedAt := harness.readInstallation(t); completedAt == nil || !completedAt.Equal(testStart) {
		t.Errorf("setup_completed_at = %v, want %s unchanged", completedAt, testStart)
	}
	if _, err := harness.service.CreateAdmin(t.Context(), "Jordan@Example.com", "Jordan Lee", testPassword); !errors.Is(err, members.ErrEmailTaken) {
		t.Errorf("create admin with a taken email: err = %v, want members.ErrEmailTaken", err)
	}
}

func TestConcurrentCompleteSetupAdmitsOne(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), concurrentSetupDeadline)
	defer cancel()
	harness := newHarness(t, insecureURL)
	token := harness.prepareSetupToken(t)
	lockHolder, err := harness.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	defer func() {
		if err := lockHolder.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("roll back lock holder: %v", err)
		}
	}()
	if _, err := lockHolder.Exec(ctx, "SELECT FROM installation FOR UPDATE"); err != nil {
		t.Fatalf("lock installation row: %v", err)
	}
	results := make(chan error, concurrentSetupCalls)
	for call := range concurrentSetupCalls {
		go func() {
			_, err := harness.service.CompleteSetup(ctx, setupInput(token, fmt.Sprintf("member%d@example.com", call)))
			results <- err
		}()
	}

	waitForLockWaiters(ctx, t, harness.pool, concurrentSetupCalls)
	if err := lockHolder.Rollback(ctx); err != nil {
		t.Fatalf("release installation row: %v", err)
	}

	var succeeded, rejected int
	for range concurrentSetupCalls {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, installation.ErrSetupNotAvailable):
			rejected++
		default:
			t.Errorf("complete setup: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Errorf("succeeded=%d rejected=%d, want one each", succeeded, rejected)
	}
	var memberCount int
	if err := harness.pool.QueryRow(ctx, "SELECT count(*) FROM members").Scan(&memberCount); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if memberCount != 1 {
		t.Errorf("members = %d, want 1", memberCount)
	}
}

func setupInput(token string, email string) installation.CompleteSetupInput {
	return installation.CompleteSetupInput{Token: token, Email: email, DisplayName: testDisplayName, Password: testPassword}
}

func waitForLockWaiters(ctx context.Context, t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	ticker := time.NewTicker(concurrentSetupPollInterval)
	defer ticker.Stop()
	for {
		var waiting int
		err := pool.QueryRow(ctx, `SELECT count(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
				AND wait_event_type = 'Lock'`).Scan(&waiting)
		if err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if waiting == want {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("waiting on the installation row = %d, want %d", waiting, want)
		}
	}
}
