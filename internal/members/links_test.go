package members_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	concurrentResetLinkRounds = 20
	concurrentConsumeRounds   = 10
	concurrentConsumers       = 4
	waitTimeout               = 10 * time.Second
	pollInterval              = 20 * time.Millisecond
)

func TestConsumeLinkRejectsInvalidPassword(t *testing.T) {
	t.Parallel()
	harness := newManagementHarness(t)
	actor := harness.createMember(t, testEmail, pointer(testPassword))
	_, linkURL, err := harness.service.Add(t.Context(), inviteeEmail, inviteeDisplayName, actor.ID)
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	token := linkToken(t, linkURL)

	recorder := consumeLink(t, harness, token, "short")

	assertLocations(t, assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed"), "body.password")
	if cookies := recorder.Header().Values("Set-Cookie"); len(cookies) != 0 {
		t.Errorf("rejected consume set cookies %v", cookies)
	}
	if consumedAt := readLink(t, harness, token).ConsumedAt; consumedAt != nil {
		t.Errorf("consumed_at = %s, want the link unconsumed", consumedAt)
	}
	if recorder := consumeLink(t, harness, token, newPassword); recorder.Code != http.StatusOK {
		t.Errorf("consume with a valid password status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestLinkOfRemovedMemberIsExpired(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	actor := harness.createMember(t, testEmail, pointer(testPassword))
	invitee, linkURL, err := harness.service.Add(t.Context(), inviteeEmail, inviteeDisplayName, actor.ID)
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := harness.service.Remove(t.Context(), actor.ID, invitee.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	token := linkToken(t, linkURL)

	if _, err := harness.service.InspectLink(t.Context(), token); !errors.Is(err, members.ErrLinkExpired) {
		t.Errorf("InspectLink error = %v, want ErrLinkExpired", err)
	}
	if _, err := harness.service.ConsumeLink(t.Context(), token, newPassword); !errors.Is(err, members.ErrLinkExpired) {
		t.Errorf("ConsumeLink error = %v, want ErrLinkExpired", err)
	}
	member, err := harness.service.Member(t.Context(), invitee.ID)
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	if member.Status != members.StatusDisabled || member.HasPassword {
		t.Errorf("member status=%s has_password=%t, want disabled without a password", member.Status, member.HasPassword)
	}
}

func TestUnknownLinkTokenIsExpired(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	token := secrets.NewToken()

	if _, err := harness.service.InspectLink(t.Context(), token); !errors.Is(err, members.ErrLinkExpired) {
		t.Errorf("InspectLink error = %v, want ErrLinkExpired", err)
	}
	if _, err := harness.service.ConsumeLink(t.Context(), token, newPassword); !errors.Is(err, members.ErrLinkExpired) {
		t.Errorf("ConsumeLink error = %v, want ErrLinkExpired", err)
	}
}

func TestResetLinkExpiresEarlierInviteLink(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	actor := harness.createMember(t, testEmail, pointer(testPassword))
	invitee, inviteURL, err := harness.service.Add(t.Context(), inviteeEmail, inviteeDisplayName, actor.ID)
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	resetURL, err := harness.service.CreateResetLink(t.Context(), invitee.ID, &actor.ID)
	if err != nil {
		t.Fatalf("create reset link: %v", err)
	}
	inviteToken := linkToken(t, inviteURL)

	if _, err := harness.service.InspectLink(t.Context(), inviteToken); !errors.Is(err, members.ErrLinkExpired) {
		t.Errorf("InspectLink of the invite error = %v, want ErrLinkExpired", err)
	}
	if _, err := harness.service.ConsumeLink(t.Context(), linkToken(t, resetURL), newPassword); err != nil {
		t.Fatalf("consume reset link: %v", err)
	}
	if _, err := harness.service.ConsumeLink(t.Context(), inviteToken, "another long passphrase"); !errors.Is(err, members.ErrLinkExpired) {
		t.Errorf("ConsumeLink of the invite error = %v, want ErrLinkExpired", err)
	}
}

func TestConsumingLinkExpiresOtherLinksOfMember(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		consumeInviteFirst bool
	}{
		{name: "invite then reset link", consumeInviteFirst: true},
		{name: "reset link then invite", consumeInviteFirst: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			harness := newHarness(t, insecureURL)
			member := harness.createMember(t, testEmail, pointer(testPassword))
			resetURL, err := harness.service.CreateResetLink(t.Context(), member.ID, nil)
			if err != nil {
				t.Fatalf("create reset link: %v", err)
			}
			inviteToken := secrets.NewToken()
			insertLink(t, harness, member.ID, secrets.HashToken(inviteToken), linkFixture{expiresAt: testStart.Add(linkLifetime)})
			tokens := []string{inviteToken, linkToken(t, resetURL)}
			if !test.consumeInviteFirst {
				slices.Reverse(tokens)
			}

			if _, err := harness.service.ConsumeLink(t.Context(), tokens[0], newPassword); err != nil {
				t.Fatalf("consume first link: %v", err)
			}
			_, err = harness.service.ConsumeLink(t.Context(), tokens[1], "another long passphrase")

			if !errors.Is(err, members.ErrLinkExpired) {
				t.Errorf("ConsumeLink of the other link error = %v, want ErrLinkExpired", err)
			}
		})
	}
}

func TestConsumeLinkLocksMemberBeforeLink(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, pointer(testPassword))
	resetURL, err := harness.service.CreateResetLink(t.Context(), member.ID, nil)
	if err != nil {
		t.Fatalf("create reset link: %v", err)
	}
	holder, err := harness.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
	if _, err := holder.Exec(t.Context(), "SELECT member_id FROM members WHERE member_id = $1 FOR UPDATE", member.ID); err != nil {
		t.Fatalf("lock member: %v", err)
	}
	consumed := make(chan error, 1)
	go func() {
		_, err := harness.service.ConsumeLink(t.Context(), linkToken(t, resetURL), newPassword)
		consumed <- err
	}()
	waitForLockWaiter(t, harness)

	_, expireErr := holder.Exec(t.Context(),
		"UPDATE member_links SET expires_at = $2 WHERE member_id = $1 AND consumed_at IS NULL AND expires_at > $2",
		member.ID, testStart)
	commitErr := holder.Commit(t.Context())

	consumeErr := receiveError(t, consumed)
	if expireErr != nil || commitErr != nil {
		t.Errorf("expire links while holding the member lock: %v, commit: %v", expireErr, commitErr)
	}
	if !errors.Is(consumeErr, members.ErrLinkExpired) {
		t.Errorf("ConsumeLink error = %v, want ErrLinkExpired after the holder expired the link", consumeErr)
	}
}

func TestConcurrentConsumeAndResetLinkNeverDeadlock(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	member := harness.createMember(t, testEmail, pointer(testPassword))
	for round := range concurrentResetLinkRounds {
		resetURL, err := harness.service.CreateResetLink(t.Context(), member.ID, nil)
		if err != nil {
			t.Fatalf("round %d create reset link: %v", round, err)
		}
		start := make(chan struct{})
		var consumeErr, createErr error
		var wait sync.WaitGroup
		wait.Go(func() {
			<-start
			_, consumeErr = harness.service.ConsumeLink(t.Context(), linkToken(t, resetURL), newPassword)
		})
		wait.Go(func() {
			<-start
			_, createErr = harness.service.CreateResetLink(t.Context(), member.ID, nil)
		})
		close(start)
		wait.Wait()

		if consumeErr != nil && !errors.Is(consumeErr, members.ErrLinkExpired) {
			t.Fatalf("round %d ConsumeLink error = %v, want nil or ErrLinkExpired", round, consumeErr)
		}
		if createErr != nil {
			t.Fatalf("round %d CreateResetLink error = %v, want nil", round, createErr)
		}
	}
}

func TestConcurrentConsumesOfOneLinkAdmitOne(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, insecureURL)
	actor := harness.createMember(t, testEmail, pointer(testPassword))
	for round := range concurrentConsumeRounds {
		_, linkURL, err := harness.service.Add(t.Context(), fmt.Sprintf("invitee%d@example.com", round), inviteeDisplayName, actor.ID)
		if err != nil {
			t.Fatalf("round %d add member: %v", round, err)
		}
		token := linkToken(t, linkURL)
		consumeErrors := make([]error, concurrentConsumers)
		start := make(chan struct{})
		var wait sync.WaitGroup
		for index := range consumeErrors {
			wait.Go(func() {
				<-start
				_, consumeErrors[index] = harness.service.ConsumeLink(t.Context(), token, fmt.Sprintf("long passphrase number %d", index))
			})
		}
		close(start)
		wait.Wait()

		succeeded := 0
		for _, err := range consumeErrors {
			if err == nil {
				succeeded++
			} else if !errors.Is(err, members.ErrLinkExpired) {
				t.Fatalf("round %d ConsumeLink error = %v, want nil or ErrLinkExpired", round, err)
			}
		}
		if succeeded != 1 {
			t.Fatalf("round %d: %d of %d consumes succeeded, want 1", round, succeeded, concurrentConsumers)
		}
	}
}

func waitForLockWaiter(t *testing.T, harness *harness) {
	t.Helper()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	deadline := time.After(waitTimeout)
	for {
		var waiting int
		err := harness.pool.QueryRow(t.Context(),
			"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'",
		).Scan(&waiting)
		if err != nil {
			t.Fatalf("read lock waits: %v", err)
		}
		if waiting == 1 {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("no query waited for a lock within %s", waitTimeout)
		}
	}
}

func receiveError(t *testing.T, results <-chan error) error {
	t.Helper()
	select {
	case err := <-results:
		return err
	case <-time.After(waitTimeout):
		t.Fatalf("no result within %s", waitTimeout)
		return nil
	}
}
