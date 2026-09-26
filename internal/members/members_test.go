package members_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/secrets"
)

func TestCreateMember(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)

	member := harness.createMember(t, testEmail, pointer(testPassword))
	invited := harness.createMember(t, "jordan@example.com", nil)

	want := []members.Member{
		{Email: testEmail, DisplayName: testDisplayName, Status: members.StatusActive, HasPassword: true, CreatedAt: testStart},
		{Email: "jordan@example.com", DisplayName: testDisplayName, Status: members.StatusActive, HasPassword: false, CreatedAt: testStart},
	}
	if diff := cmp.Diff(want, []members.Member{member, invited}, cmpopts.IgnoreFields(members.Member{}, "ID")); diff != "" {
		t.Errorf("members mismatch (-want +got):\n%s", diff)
	}
	if member.ID == uuid.Nil || member.ID == invited.ID {
		t.Errorf("member ids = %s and %s, want two distinct ids", member.ID, invited.ID)
	}
	var passwordHash string
	if err := harness.pool.QueryRow(t.Context(), "SELECT password_hash FROM members WHERE member_id = $1", member.ID).Scan(&passwordHash); err != nil {
		t.Fatalf("read password hash: %v", err)
	}
	if matched, _, err := secrets.VerifyPassword(testPassword, passwordHash); err != nil || !matched {
		t.Errorf("stored hash matched=%t err=%v, want a hash of the password", matched, err)
	}
	loaded, err := harness.service.Member(t.Context(), member.ID)
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	if diff := cmp.Diff(member, loaded); diff != "" {
		t.Errorf("loaded member mismatch (-want +got):\n%s", diff)
	}
}

func TestMemberByEmailIgnoresLetterCase(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, pointer(testPassword))

	found, err := harness.service.MemberByEmail(t.Context(), "Sam@Example.COM")
	if err != nil {
		t.Fatalf("member by email: %v", err)
	}
	_, unknownErr := harness.service.MemberByEmail(t.Context(), "jordan@example.com")

	if diff := cmp.Diff(member, found); diff != "" {
		t.Errorf("member mismatch (-want +got):\n%s", diff)
	}
	if !errors.Is(unknownErr, httpapi.ErrNotFound) {
		t.Errorf("unknown email error = %v, want httpapi.ErrNotFound", unknownErr)
	}
}

func TestCreateMemberRejectsEmailTakenInAnotherCase(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	harness.createMember(t, testEmail, nil)

	var duplicateErr error
	err := database.InTransaction(t.Context(), harness.pool, func(ctx context.Context, transaction pgx.Tx) error {
		_, duplicateErr = harness.service.CreateMember(ctx, transaction, members.CreateMemberInput{Email: "Sam@Example.COM", DisplayName: "Sam"})
		_, err := harness.service.CreateMember(ctx, transaction, members.CreateMemberInput{Email: "jordan@example.com", DisplayName: "Jordan"})
		return err
	})

	if err != nil {
		t.Fatalf("transaction after the duplicate: %v", err)
	}
	if !errors.Is(duplicateErr, members.ErrEmailTaken) {
		t.Fatalf("error = %v, want ErrEmailTaken", duplicateErr)
	}
	coded, isCoded := errors.AsType[httpapi.CodedError](duplicateErr)
	if !isCoded || coded.ProblemStatus() != http.StatusConflict || coded.ProblemCode() != "member_email_taken" {
		t.Errorf("coded error = %v, want 409 member_email_taken", duplicateErr)
	}
}

func TestCreateMemberValidatesInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     members.CreateMemberInput
		locations []string
	}{
		{name: "short password", input: members.CreateMemberInput{Email: testEmail, DisplayName: "Sam", Password: pointer(strings.Repeat("a", 11))}, locations: []string{"body.password"}},
		{name: "long password", input: members.CreateMemberInput{Email: testEmail, DisplayName: "Sam", Password: pointer(strings.Repeat("a", 257))}, locations: []string{"body.password"}},
		{name: "not an email", input: members.CreateMemberInput{Email: "sam.example.com", DisplayName: "Sam"}, locations: []string{"body.email"}},
		{name: "email with a name", input: members.CreateMemberInput{Email: "Sam <sam@example.com>", DisplayName: "Sam"}, locations: []string{"body.email"}},
		{name: "email with spaces", input: members.CreateMemberInput{Email: " sam@example.com", DisplayName: "Sam"}, locations: []string{"body.email"}},
		{name: "long email", input: members.CreateMemberInput{Email: strings.Repeat("a", 243) + "@example.com", DisplayName: "Sam"}, locations: []string{"body.email"}},
		{name: "empty display name", input: members.CreateMemberInput{Email: testEmail, DisplayName: ""}, locations: []string{"body.display_name"}},
		{name: "blank display name", input: members.CreateMemberInput{Email: testEmail, DisplayName: "   "}, locations: []string{"body.display_name"}},
		{name: "long display name", input: members.CreateMemberInput{Email: testEmail, DisplayName: strings.Repeat("é", 81)}, locations: []string{"body.display_name"}},
		{name: "control character", input: members.CreateMemberInput{Email: testEmail, DisplayName: "Sam\x00"}, locations: []string{"body.display_name"}},
		{name: "every field", input: members.CreateMemberInput{Email: "", DisplayName: "", Password: pointer("short")}, locations: []string{"body.email", "body.display_name", "body.password"}},
	}
	harness := newHarness(t, insecureURL)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := database.InTransaction(t.Context(), harness.pool, func(ctx context.Context, transaction pgx.Tx) error {
				_, err := harness.service.CreateMember(ctx, transaction, test.input)
				return err
			})

			problem, isProblem := errors.AsType[*httpapi.Problem](err)
			if !isProblem || problem.Status != http.StatusUnprocessableEntity || problem.Code != "validation_failed" {
				t.Fatalf("error = %v, want a 422 validation_failed problem", err)
			}
			var locations []string
			for _, fieldError := range problem.Errors {
				locations = append(locations, fieldError.Location)
			}
			if diff := cmp.Diff(test.locations, locations); diff != "" {
				t.Errorf("locations mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCreateMemberAcceptsLimits(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	err := database.InTransaction(t.Context(), harness.pool, func(ctx context.Context, transaction pgx.Tx) error {
		_, err := harness.service.CreateMember(ctx, transaction, members.CreateMemberInput{
			Email:       strings.Repeat("a", 242) + "@example.com",
			DisplayName: strings.Repeat("é", 80),
			Password:    pointer(strings.Repeat("é", 12)),
		})
		return err
	})
	if err != nil {
		t.Fatalf("create member at the limits: %v", err)
	}
}
