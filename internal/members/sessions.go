package members

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/members/queries"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	sessionLifetime       = 30 * 24 * time.Hour
	sessionTouchInterval  = 10 * time.Minute
	userAgentMaximumBytes = 512
)

// SessionStore keeps the browser sessions of members in Postgres. A session
// is identified by a random token that only the member's browser holds, and
// the store keeps its SHA-256 hash. Create one with NewSessionStore. It is
// safe for concurrent use.
type SessionStore struct {
	queries *queries.Queries
	clock   clock.Clock
}

// NewSessionStore returns a SessionStore over pool that reads time from
// timeSource.
func NewSessionStore(pool *pgxpool.Pool, timeSource clock.Clock) *SessionStore {
	return &SessionStore{queries: queries.New(pool), clock: timeSource}
}

// Start starts a session for the member with memberID and returns its token.
// The session expires 30 days after its last use. It records userAgent, cut
// to 512 bytes of valid UTF-8, and clientIP.
func (store *SessionStore) Start(ctx context.Context, memberID uuid.UUID, userAgent string, clientIP netip.Addr) (string, error) {
	return store.start(ctx, store.queries, memberID, userAgent, clientIP)
}

// Authenticate returns the member whose session token is token. An unknown
// or expired token, or a member who is not active, returns
// httpapi.ErrAuthenticationRequired. When the session was last seen 10 or
// more minutes ago, Authenticate records now as its last use and moves its
// expiry to 30 days from now, so an active session never expires and the
// store writes at most once every 10 minutes per session.
func (store *SessionStore) Authenticate(ctx context.Context, token string) (Member, error) {
	tokenHash := secrets.HashToken(token)
	now := store.clock.Now()
	row, err := store.selectLive(ctx, tokenHash, now)
	if err != nil {
		return Member{}, err
	}
	if now.Sub(row.LastSeenAt) >= sessionTouchInterval {
		err := store.queries.TouchSession(ctx, queries.TouchSessionParams{
			SessionTokenHash: tokenHash,
			LastSeenAt:       now,
			ExpiresAt:        now.Add(sessionLifetime),
		})
		if err != nil {
			return Member{}, fmt.Errorf("touch session: %w", err)
		}
	}
	return memberFromRow(row.Member), nil
}

// Check returns httpapi.ErrAuthenticationRequired when token names no live
// session of an active member, like Authenticate, but never records a use,
// so checking a session never extends it.
func (store *SessionStore) Check(ctx context.Context, token string) error {
	_, err := store.selectLive(ctx, secrets.HashToken(token), store.clock.Now())
	return err
}

// End deletes the session with token. Ending an unknown session does
// nothing.
func (store *SessionStore) End(ctx context.Context, token string) error {
	if err := store.queries.DeleteSession(ctx, secrets.HashToken(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// EndOthers deletes every session of the member with memberID except the one
// with keepToken.
func (store *SessionStore) EndOthers(ctx context.Context, memberID uuid.UUID, keepToken string) error {
	return store.endOthers(ctx, store.queries, memberID, keepToken)
}

func (store *SessionStore) selectLive(ctx context.Context, tokenHash []byte, now time.Time) (queries.SelectSessionWithMemberRow, error) {
	row, err := store.queries.SelectSessionWithMember(ctx, queries.SelectSessionWithMemberParams{SessionTokenHash: tokenHash, Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return queries.SelectSessionWithMemberRow{}, httpapi.ErrAuthenticationRequired
	}
	if err != nil {
		return queries.SelectSessionWithMemberRow{}, fmt.Errorf("select session: %w", err)
	}
	return row, nil
}

func (store *SessionStore) start(ctx context.Context, sessionQueries *queries.Queries, memberID uuid.UUID, userAgent string, clientIP netip.Addr) (string, error) {
	token := secrets.NewToken()
	now := store.clock.Now()
	err := sessionQueries.InsertSession(ctx, queries.InsertSessionParams{
		SessionTokenHash: secrets.HashToken(token),
		MemberID:         memberID,
		ExpiresAt:        now.Add(sessionLifetime),
		LastSeenAt:       now,
		UserAgent:        storedUserAgent(userAgent),
		IpAddress:        clientIP,
		CreatedAt:        now,
	})
	if err != nil {
		return "", fmt.Errorf("insert session of member %s: %w", memberID, err)
	}
	return token, nil
}

func (store *SessionStore) endOthers(ctx context.Context, sessionQueries *queries.Queries, memberID uuid.UUID, keepToken string) error {
	err := sessionQueries.DeleteSessionsForMemberExcept(ctx, queries.DeleteSessionsForMemberExceptParams{
		MemberID:             memberID,
		KeepSessionTokenHash: secrets.HashToken(keepToken),
	})
	if err != nil {
		return fmt.Errorf("delete other sessions of member %s: %w", memberID, err)
	}
	return nil
}

func storedUserAgent(userAgent string) string {
	if len(userAgent) > userAgentMaximumBytes {
		userAgent = userAgent[:userAgentMaximumBytes]
	}
	return strings.ToValidUTF8(userAgent, "")
}
