package members

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/members/queries"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	loginAttemptsPerWindow = 10
	loginAttemptWindow     = 15 * time.Minute
	loginEmailLimit        = "login_email"
	loginClientIPLimit     = "login_client_ip"
)

// ErrLoginFailed is the error for a login with an unknown email, a wrong
// password, or a member who cannot sign in: 401 login_failed. Every such
// login gets the same problem.
var ErrLoginFailed = httpapi.NewCodedError(http.StatusUnauthorized, "login_failed", "email or password is wrong")

// Login checks email and password and starts a session for the member they
// belong to. It counts the attempt first against two limits of 10 attempts
// per 15 minutes, one per email address ignoring case and one per clientIP,
// and returns an *httpapi.RateLimitedError when either is exceeded. An email
// that is not a valid address, an unknown email, a member without a password
// and a wrong password return ErrLoginFailed after the same password hashing
// work, so timing does not tell them apart. A disabled member also returns
// ErrLoginFailed.
//
// The session starts in a transaction that locks the member and checks again
// that the member is active and still has the password hash that matched.
// A login that races a password change, a consumed link or a removal of the
// member therefore returns ErrLoginFailed and starts no session. The same
// transaction records the time of the login like RecordLogin and rehashes a
// password hashed with older parameters. Login returns the member and the
// token of the session, which records userAgent and clientIP.
func (service *Service) Login(ctx context.Context, email string, password string, userAgent string, clientIP netip.Addr) (Member, string, error) {
	if err := service.countLoginAttempt(ctx, loginEmailLimit, strings.ToLower(email)); err != nil {
		return Member{}, "", err
	}
	if err := service.countLoginAttempt(ctx, loginClientIPLimit, clientIP.String()); err != nil {
		return Member{}, "", err
	}
	if !validEmail(email) {
		return failLogin(password)
	}
	row, err := service.queries.SelectMemberByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return failLogin(password)
	}
	if err != nil {
		return Member{}, "", fmt.Errorf("select member by email: %w", err)
	}
	if row.PasswordHash == nil {
		return failLogin(password)
	}
	matched, needsRehash, err := secrets.VerifyPassword(password, *row.PasswordHash)
	if err != nil {
		return Member{}, "", fmt.Errorf("verify password of member %s: %w", row.MemberID, err)
	}
	if !matched || row.Status != queries.RecordStatusActive {
		return Member{}, "", ErrLoginFailed
	}
	var rehashed *string
	if needsRehash {
		passwordHash := secrets.HashPassword(password)
		rehashed = &passwordHash
	}
	return service.startLoginSession(ctx, row, rehashed, userAgent, clientIP)
}

// RecordLogin records the current time, cut to the microseconds Postgres
// keeps, as the last login of the member with memberID inside transaction,
// which the caller commits, and returns the member with it. Setup calls it
// for the first member, whose session starts once the transaction commits.
func (service *Service) RecordLogin(ctx context.Context, transaction pgx.Tx, memberID uuid.UUID) (Member, error) {
	row, err := service.recordLogin(ctx, service.queries.WithTx(transaction), memberID)
	if err != nil {
		return Member{}, err
	}
	return memberFromRow(row), nil
}

func (service *Service) startLoginSession(ctx context.Context, verified queries.Member, rehashed *string, userAgent string, clientIP netip.Addr) (Member, string, error) {
	var member Member
	var sessionToken string
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		transactionQueries := service.queries.WithTx(transaction)
		row, err := transactionQueries.SelectSignInMemberForUpdate(ctx, queries.SelectSignInMemberForUpdateParams{
			MemberID:     verified.MemberID,
			PasswordHash: verified.PasswordHash,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLoginFailed
		}
		if err != nil {
			return fmt.Errorf("select member %s for update: %w", verified.MemberID, err)
		}
		if rehashed != nil {
			err := transactionQueries.UpdateMemberPasswordHash(ctx, queries.UpdateMemberPasswordHashParams{MemberID: row.MemberID, PasswordHash: rehashed, UpdatedAt: service.clock.Now()})
			if err != nil {
				return fmt.Errorf("rehash password of member %s: %w", row.MemberID, err)
			}
		}
		row, err = service.recordLogin(ctx, transactionQueries, row.MemberID)
		if err != nil {
			return err
		}
		sessionToken, err = service.sessions.start(ctx, transactionQueries, row.MemberID, userAgent, clientIP)
		if err != nil {
			return err
		}
		member = memberFromRow(row)
		return nil
	})
	if err != nil {
		return Member{}, "", err
	}
	return member, sessionToken, nil
}

func (service *Service) recordLogin(ctx context.Context, transactionQueries *queries.Queries, memberID uuid.UUID) (queries.Member, error) {
	// Postgres keeps microseconds, so a finer time in the response would differ from every later read.
	loginAt := service.clock.Now().Truncate(time.Microsecond)
	row, err := transactionQueries.UpdateMemberLastLogin(ctx, queries.UpdateMemberLastLoginParams{MemberID: memberID, LastLoginAt: &loginAt})
	if err != nil {
		return queries.Member{}, fmt.Errorf("record login of member %s: %w", memberID, err)
	}
	return row, nil
}

func (service *Service) countLoginAttempt(ctx context.Context, limitName string, subject string) error {
	allowed, retryAfter, err := service.rateLimiter.Allow(ctx, limitName, subject, loginAttemptsPerWindow, loginAttemptWindow)
	if err != nil {
		return err
	}
	if !allowed {
		return &httpapi.RateLimitedError{RetryAfter: retryAfter}
	}
	return nil
}

func failLogin(password string) (Member, string, error) {
	secrets.DummyVerify(password)
	return Member{}, "", ErrLoginFailed
}
