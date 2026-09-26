package members_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/secrets"
)

const concurrentRemovalRounds = 25

func TestRemoveLastActiveMemberIsRejected(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	lastActive := harness.createMember(t, testEmail, pointer(testPassword))
	disabled := harness.createMember(t, inviteeEmail, pointer(testPassword))
	if _, err := harness.pool.Exec(t.Context(), "UPDATE members SET status = 'disabled' WHERE member_id = $1", disabled.ID); err != nil {
		t.Fatalf("disable member: %v", err)
	}

	err := harness.service.Remove(t.Context(), disabled.ID, lastActive.ID)

	if !errors.Is(err, members.ErrLastMember) {
		t.Fatalf("error = %v, want ErrLastMember", err)
	}
	member, err := harness.service.Member(t.Context(), lastActive.ID)
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	if member.Status != members.StatusActive {
		t.Errorf("status = %s, want active", member.Status)
	}
}

func TestRemovedActorCannotRemoveMembers(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	sam := harness.createMember(t, testEmail, pointer(testPassword))
	bob := harness.createMember(t, "bob@example.com", pointer(testPassword))
	harness.createMember(t, inviteeEmail, nil)
	if err := harness.service.Remove(t.Context(), bob.ID, sam.ID); err != nil {
		t.Fatalf("bob removes sam: %v", err)
	}

	err := harness.service.Remove(t.Context(), sam.ID, bob.ID)

	if !errors.Is(err, members.ErrLastMember) {
		t.Errorf("removed sam removes bob error = %v, want ErrLastMember", err)
	}
	if status := memberStatus(t, harness, bob.ID); status != members.StatusActive {
		t.Errorf("bob status = %s, want active", status)
	}
}

func TestRemoveKeepsOneMemberWhoCanSignIn(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	sam := harness.createMember(t, testEmail, pointer(testPassword))
	jordan := harness.createMember(t, inviteeEmail, nil)

	removeLastSignInErr := harness.service.Remove(t.Context(), jordan.ID, sam.ID)
	removeInviteeErr := harness.service.Remove(t.Context(), sam.ID, jordan.ID)

	if !errors.Is(removeLastSignInErr, members.ErrLastMember) {
		t.Errorf("pending invitee removes the last member with a password error = %v, want ErrLastMember", removeLastSignInErr)
	}
	if removeInviteeErr != nil {
		t.Errorf("last member with a password removes a pending invitee error = %v, want nil", removeInviteeErr)
	}
	if status := memberStatus(t, harness, sam.ID); status != members.StatusActive {
		t.Errorf("sam status = %s, want active", status)
	}
	if status := memberStatus(t, harness, jordan.ID); status != members.StatusDisabled {
		t.Errorf("jordan status = %s, want disabled", status)
	}
}

func TestConcurrentMutualRemovalsKeepOneMemberWhoCanSignIn(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	passwordHash := secrets.HashPassword(testPassword)
	for round := range concurrentRemovalRounds {
		first := harness.createMemberWithPasswordHash(t, fmt.Sprintf("first%d@example.com", round), passwordHash)
		second := harness.createMemberWithPasswordHash(t, fmt.Sprintf("second%d@example.com", round), passwordHash)
		invitee := harness.createMember(t, fmt.Sprintf("invitee%d@example.com", round), nil)
		removals := [][2]uuid.UUID{{first.ID, second.ID}, {second.ID, first.ID}}
		removalErrors := make([]error, len(removals))
		start := make(chan struct{})
		var wait sync.WaitGroup
		for index, removal := range removals {
			wait.Go(func() {
				<-start
				removalErrors[index] = harness.service.Remove(t.Context(), removal[0], removal[1])
			})
		}
		close(start)
		wait.Wait()

		var active []uuid.UUID
		for _, memberID := range []uuid.UUID{first.ID, second.ID} {
			if memberStatus(t, harness, memberID) == members.StatusActive {
				active = append(active, memberID)
			}
		}
		succeeded := 0
		for _, err := range removalErrors {
			if err == nil {
				succeeded++
			} else if !errors.Is(err, members.ErrLastMember) {
				t.Fatalf("round %d removal error = %v, want nil or ErrLastMember", round, err)
			}
		}
		if succeeded != 1 || len(active) != 1 {
			t.Fatalf("round %d: %d removals succeeded and %d members with a password stay active, want 1 and 1", round, succeeded, len(active))
		}
		_, err := harness.pool.Exec(t.Context(), "UPDATE members SET status = 'disabled' WHERE member_id = ANY($1)", []uuid.UUID{first.ID, second.ID, invitee.ID})
		if err != nil {
			t.Fatalf("disable round members: %v", err)
		}
	}
}

func TestRemoveUnknownMemberIsNotFound(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	actor := harness.createMember(t, testEmail, pointer(testPassword))

	if err := harness.service.Remove(t.Context(), actor.ID, identifiers.New()); !errors.Is(err, httpapi.ErrNotFound) {
		t.Errorf("Remove error = %v, want ErrNotFound", err)
	}
	if _, err := harness.service.CreateResetLink(t.Context(), identifiers.New(), &actor.ID); !errors.Is(err, httpapi.ErrNotFound) {
		t.Errorf("CreateResetLink error = %v, want ErrNotFound", err)
	}
}

func TestCreateResetLinkWithoutCreator(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, pointer(testPassword))

	linkURL, err := harness.service.CreateResetLink(t.Context(), member.ID, nil)

	if err != nil {
		t.Fatalf("create reset link: %v", err)
	}
	link := readLink(t, harness, linkToken(t, linkURL))
	if link.Purpose != "password_reset" || link.CreatedByMemberID != nil || !link.ExpiresAt.Equal(testStart.Add(linkLifetime)) {
		t.Errorf("stored link = %+v, want a password_reset link without creator expiring at %s", link, testStart.Add(linkLifetime))
	}
}

func (harness *harness) createMemberWithPasswordHash(t *testing.T, email string, passwordHash string) members.Member {
	t.Helper()
	member := harness.createMember(t, email, nil)
	if _, err := harness.pool.Exec(t.Context(), "UPDATE members SET password_hash = $1 WHERE member_id = $2", passwordHash, member.ID); err != nil {
		t.Fatalf("store password hash of %s: %v", email, err)
	}
	return member
}

func memberStatus(t *testing.T, harness *harness, memberID uuid.UUID) members.Status {
	t.Helper()
	member, err := harness.service.Member(t.Context(), memberID)
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	return member.Status
}
