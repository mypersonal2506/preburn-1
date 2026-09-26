package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/installation"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	adminEmail    = "sam@example.com"
	adminName     = "Sam Rivera"
	adminPassword = "correct horse battery"
	removedEmail  = "priya@example.com"
)

type storedAPIKey struct {
	Environment       string
	Scope             string
	Name              string
	CreatedByMemberID *uuid.UUID
}

var (
	memberIDLinePattern  = regexp.MustCompile(`^mem_[0-9a-z]{26}\n$`)
	secretLinePattern    = regexp.MustCompile(`^pb_live_admin_[0-9A-Za-z]{32}\n$`)
	setupLinkLinePattern = regexp.MustCompile(`^http://localhost:8080/setup#[0-9A-Za-z_-]{43}\n$`)
	resetLinkLinePattern = regexp.MustCompile(`^http://localhost:8080/link#([0-9A-Za-z_-]{43})\n$`)
)

func TestAdminCreateReadsPasswordFromStandardInput(t *testing.T) {
	pool := databasetest.NewPool(t)
	setEnvironment(t, pool.Config().ConnString())
	var output bytes.Buffer

	err := executeWithInput(t.Context(), adminPassword+"\n", &output, "admin", "create", "--email", adminEmail, "--name", adminName, "--password-stdin")
	if err != nil {
		t.Fatalf("admin create: %v", err)
	}

	if !memberIDLinePattern.MatchString(output.String()) {
		t.Fatalf("output = %q, want one member id line", output.String())
	}
	var memberID uuid.UUID
	var displayName, passwordHash string
	var setupCompleted bool
	err = pool.QueryRow(t.Context(), `
		SELECT members.member_id, members.display_name, members.password_hash, installation.setup_completed_at IS NOT NULL
		FROM members, installation
		WHERE members.email = $1`, adminEmail).Scan(&memberID, &displayName, &passwordHash, &setupCompleted)
	if err != nil {
		t.Fatalf("read member: %v", err)
	}
	if printed := strings.TrimSuffix(output.String(), "\n"); printed != identifiers.Encode(identifiers.PrefixMember, memberID) {
		t.Errorf("printed id %s, want the id of member %s", printed, memberID)
	}
	if displayName != adminName {
		t.Errorf("display name = %q, want %q", displayName, adminName)
	}
	if matched, _, err := secrets.VerifyPassword(adminPassword, passwordHash); err != nil || !matched {
		t.Errorf("stored hash matched=%t err=%v, want a hash of the password without its newline", matched, err)
	}
	if !setupCompleted {
		t.Error("setup is still pending, want it completed")
	}
}

func TestAdminAPIKeyCreatePrintsOnlyTheSecret(t *testing.T) {
	pool := databasetest.NewPool(t)
	setEnvironment(t, pool.Config().ConnString())
	var output bytes.Buffer

	err := executeWithInput(t.Context(), "", &output, "admin", "api-key", "create", "--environment", "live", "--scope", "admin", "--name", "Deploy pipeline")
	if err != nil {
		t.Fatalf("admin api-key create: %v", err)
	}

	if !secretLinePattern.MatchString(output.String()) {
		t.Fatalf("output = %q, want one live admin secret line", output.String())
	}
	var stored storedAPIKey
	err = pool.QueryRow(t.Context(), "SELECT environment::text, scope, name, created_by_member_id FROM api_keys WHERE secret_hash = $1",
		secrets.HashToken(strings.TrimSuffix(output.String(), "\n"))).Scan(&stored.Environment, &stored.Scope, &stored.Name, &stored.CreatedByMemberID)
	if err != nil {
		t.Fatalf("read key by the hash of the printed secret: %v", err)
	}
	if diff := cmp.Diff(storedAPIKey{Environment: "live", Scope: "admin", Name: "Deploy pipeline"}, stored); diff != "" {
		t.Errorf("stored key mismatch (-want +got):\n%s", diff)
	}
}

func TestAdminSetupLinkPrintsLinkWhileSetupIsPending(t *testing.T) {
	setEnvironment(t, databasetest.NewPool(t).Config().ConnString())
	var pendingOutput bytes.Buffer

	if err := executeWithInput(t.Context(), "", &pendingOutput, "admin", "setup-link"); err != nil {
		t.Fatalf("admin setup-link: %v", err)
	}
	createAdmin(t)
	var completeOutput bytes.Buffer
	err := executeWithInput(t.Context(), "", &completeOutput, "admin", "setup-link")

	if !setupLinkLinePattern.MatchString(pendingOutput.String()) {
		t.Errorf("output while pending = %q, want one setup link line", pendingOutput.String())
	}
	if code := exitCode(err); code != exitInvalidInput || !errors.Is(err, installation.ErrSetupNotAvailable) {
		t.Errorf("after setup exit code = %d, error %v, want %d and installation.ErrSetupNotAvailable", code, err, exitInvalidInput)
	}
	if completeOutput.Len() != 0 {
		t.Errorf("output after setup = %q, want none", completeOutput.String())
	}
}

func TestAdminResetPasswordPrintsResetLink(t *testing.T) {
	pool := databasetest.NewPool(t)
	setEnvironment(t, pool.Config().ConnString())
	createAdmin(t)
	var output bytes.Buffer

	if err := executeWithInput(t.Context(), "", &output, "admin", "reset-password", "--email", "Sam@Example.COM"); err != nil {
		t.Fatalf("admin reset-password: %v", err)
	}

	match := resetLinkLinePattern.FindStringSubmatch(output.String())
	if match == nil {
		t.Fatalf("output = %q, want one link line", output.String())
	}
	var purpose, email string
	err := pool.QueryRow(t.Context(), `
		SELECT member_links.purpose, members.email
		FROM member_links JOIN members USING (member_id)
		WHERE member_links.token_hash = $1 AND member_links.consumed_at IS NULL`, secrets.HashToken(match[1])).Scan(&purpose, &email)
	if err != nil {
		t.Fatalf("read link by the hash of the printed token: %v", err)
	}
	if purpose != "password_reset" || email != adminEmail {
		t.Errorf("link purpose %q of member %q, want password_reset of %q", purpose, email, adminEmail)
	}
}

func TestAdminCommandsRejectInvalidInput(t *testing.T) {
	pool := databasetest.NewPool(t)
	setEnvironment(t, pool.Config().ConnString())
	createAdmin(t)
	if err := executeWithInput(t.Context(), adminPassword, io.Discard, "admin", "create", "--email", removedEmail, "--name", adminName, "--password-stdin"); err != nil {
		t.Fatalf("admin create %s: %v", removedEmail, err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE members SET status = 'disabled' WHERE email = $1", removedEmail); err != nil {
		t.Fatalf("disable %s: %v", removedEmail, err)
	}
	tests := []struct {
		name      string
		input     string
		arguments []string
		message   string
	}{
		{name: "create without email", input: adminPassword, arguments: []string{"admin", "create", "--name", adminName, "--password-stdin"}, message: "--email is required"},
		{name: "create without name", input: adminPassword, arguments: []string{"admin", "create", "--email", adminEmail, "--password-stdin"}, message: "--name is required"},
		{name: "create without a terminal", arguments: []string{"admin", "create", "--email", adminEmail, "--name", adminName}, message: "--password-stdin"},
		{name: "create with a short password", input: "short\n", arguments: []string{"admin", "create", "--email", adminEmail, "--name", adminName, "--password-stdin"}, message: "password: expected 12 to 256 characters"},
		{name: "create with an invalid email", input: adminPassword, arguments: []string{"admin", "create", "--email", "sam", "--name", adminName, "--password-stdin"}, message: "email: expected an email address"},
		{name: "reset-password without email", arguments: []string{"admin", "reset-password"}, message: "--email is required"},
		{name: "create with a taken email", input: adminPassword, arguments: []string{"admin", "create", "--email", "SAM@example.com", "--name", adminName, "--password-stdin"}, message: "a member with this email exists"},
		{name: "reset-password for an unknown email", arguments: []string{"admin", "reset-password", "--email", "jordan@example.com"}, message: "no member has this email"},
		{name: "reset-password for a removed member", arguments: []string{"admin", "reset-password", "--email", removedEmail}, message: "the member is disabled"},
		{name: "setup-link after setup", arguments: []string{"admin", "setup-link"}, message: "setup is complete"},
		{name: "api-key create without flags", arguments: []string{"admin", "api-key", "create"}, message: "--environment is required"},
		{name: "api-key create with an unknown environment", arguments: []string{"admin", "api-key", "create", "--environment", "staging", "--scope", "admin", "--name", "CI"}, message: "environment must be test or live"},
		{name: "api-key create with an unknown scope", arguments: []string{"admin", "api-key", "create", "--environment", "live", "--scope", "owner", "--name", "CI"}, message: "scope: expected runtime or admin"},
		{name: "unknown admin subcommand", arguments: []string{"admin", "unknown"}, message: "unknown command"},
		{name: "unknown api-key subcommand", arguments: []string{"admin", "api-key", "unknown"}, message: "unknown command"},
		{name: "unknown admin flag", arguments: []string{"admin", "--unknown"}, message: "unknown flag"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer

			err := executeWithInput(t.Context(), test.input, &output, test.arguments...)

			if code := exitCode(err); code != exitInvalidInput {
				t.Errorf("exit code = %d, want %d, error %v", code, exitInvalidInput, err)
			}
			if err != nil && !strings.Contains(err.Error(), test.message) {
				t.Errorf("error = %v, want it to contain %q", err, test.message)
			}
			if output.Len() != 0 {
				t.Errorf("output = %q, want none", output.String())
			}
		})
	}
}

func TestAdminServerFailuresExitWithFailure(t *testing.T) {
	unavailable := httpapi.NewCodedError(http.StatusServiceUnavailable, "database_unavailable", "the database is unavailable")

	err := invalidInputFromRejection(fmt.Errorf("create member: %w", unavailable))

	if code := exitCode(err); code != exitFailure || !errors.Is(err, unavailable) {
		t.Errorf("exit code = %d, error %v, want %d wrapping the coded error", code, err, exitFailure)
	}
}

func TestAdminPrintsHelp(t *testing.T) {
	var output bytes.Buffer

	if err := executeWithInput(t.Context(), "", &output, "admin"); err != nil {
		t.Fatalf("admin: %v", err)
	}

	for _, subcommand := range []string{"create", "setup-link", "reset-password", "api-key"} {
		if !strings.Contains(output.String(), subcommand) {
			t.Errorf("help does not list %s:\n%s", subcommand, output.String())
		}
	}
}

func createAdmin(t *testing.T) {
	t.Helper()
	err := executeWithInput(t.Context(), adminPassword, io.Discard, "admin", "create", "--email", adminEmail, "--name", adminName, "--password-stdin")
	if err != nil {
		t.Fatalf("admin create: %v", err)
	}
}

func executeWithInput(ctx context.Context, input string, output io.Writer, arguments ...string) error {
	root := newRootCommand()
	root.SetIn(strings.NewReader(input))
	root.SetOut(output)
	root.SetErr(io.Discard)
	root.SetArgs(arguments)
	return root.ExecuteContext(ctx)
}
