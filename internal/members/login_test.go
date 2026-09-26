package members_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/preburn/preburn/internal/secrets"
)

func TestLoginRacingPasswordResetFails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		storedHash string
	}{
		{name: "current hash parameters", storedHash: secrets.HashPassword(testPassword)},
		{name: "older hash parameters", storedHash: olderPasswordHash(testPassword)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			hook := &queryHook{queryName: "SelectMemberByEmail"}
			harness := newHookedHarness(t, hook)
			member := harness.createMember(t, testEmail, nil)
			if _, err := harness.pool.Exec(t.Context(), "UPDATE members SET password_hash = $1 WHERE member_id = $2", test.storedHash, member.ID); err != nil {
				t.Fatalf("store password hash: %v", err)
			}
			resetURL, err := harness.service.CreateResetLink(t.Context(), member.ID, nil)
			if err != nil {
				t.Fatalf("create reset link: %v", err)
			}
			hook.run = func(ctx context.Context) {
				if _, err := harness.service.ConsumeLink(context.WithoutCancel(ctx), linkToken(t, resetURL), newPassword); err != nil {
					t.Errorf("consume reset link during login: %v", err)
				}
			}
			hook.armed.Store(true)

			recorder := harness.login(t, testEmail, testPassword)

			var storedHash string
			if err := harness.pool.QueryRow(t.Context(), "SELECT password_hash FROM members WHERE member_id = $1", member.ID).Scan(&storedHash); err != nil {
				t.Fatalf("read password hash: %v", err)
			}
			oldMatches, _, oldErr := secrets.VerifyPassword(testPassword, storedHash)
			newMatches, _, newErr := secrets.VerifyPassword(newPassword, storedHash)
			if oldErr != nil || newErr != nil || oldMatches || !newMatches {
				t.Errorf("after the reset old password matches=%t err=%v, new password matches=%t err=%v, want only the new password", oldMatches, oldErr, newMatches, newErr)
			}
			var sessionCount int
			if err := harness.pool.QueryRow(t.Context(), "SELECT count(*) FROM member_sessions WHERE member_id = $1", member.ID).Scan(&sessionCount); err != nil {
				t.Fatalf("count sessions: %v", err)
			}
			if sessionCount != 0 {
				t.Errorf("sessions after the reset = %d, want 0", sessionCount)
			}
			assertProblem(t, recorder, http.StatusUnauthorized, "login_failed")
		})
	}
}
