package members

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/members/queries"
)

const membersListing = "members"

type memberSortKey struct {
	CreatedAt time.Time `json:"created_at"`
	MemberID  uuid.UUID `json:"member_id"`
}

// ErrLastMember is the error for removing yourself or the last active member
// who can sign in, or for a removal by a member who is no longer active: 409
// last_member.
var ErrLastMember = httpapi.NewCodedError(http.StatusConflict, "last_member", "cannot remove yourself or the last member who can sign in")

// ErrMemberDisabled is the error for a password reset link for a removed
// member: 409 member_disabled.
var ErrMemberDisabled = httpapi.NewCodedError(http.StatusConflict, "member_disabled", "the member is disabled")

var membersCursor = httpapi.NewCursor[memberSortKey](membersListing)

// List returns up to limit members in creation order, starting after the
// member that cursor points to, or at the first member when cursor is empty.
// The second result is the cursor of the next page, empty on the last page.
// Members belong to no environment, but the cursor holds environment, the
// environment of the request. A cursor from another listing or environment
// or one that is malformed returns an error wrapping
// httpapi.ErrInvalidCursor. The caller checks limit with httpapi.ParseLimit.
func (service *Service) List(ctx context.Context, environment httpapi.Environment, cursor string, limit int) ([]Member, string, error) {
	parameters := queries.ListMembersParams{RowLimit: int64(limit) + 1}
	if cursor != "" {
		key, err := membersCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		parameters.AfterCreatedAt = &key.CreatedAt
		parameters.AfterMemberID = &key.MemberID
	}
	rows, err := service.queries.ListMembers(ctx, parameters)
	if err != nil {
		return nil, "", fmt.Errorf("list members: %w", err)
	}
	var nextCursor string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		nextCursor, err = membersCursor.Encode(environment, memberSortKey{CreatedAt: last.CreatedAt, MemberID: last.MemberID})
		if err != nil {
			return nil, "", err
		}
	}
	listed := make([]Member, 0, len(rows))
	for _, row := range rows {
		listed = append(listed, memberFromRow(row))
	}
	return listed, nextCursor, nil
}

// Add creates an active member without a password and an invite link that
// sets the password, in one transaction. createdBy is the member who adds
// them. It returns the member and the link URL,
// {PREBURN_PUBLIC_URL}/link#<token>, which expires after 24 hours. The
// database stores only the hash of the token, so the URL cannot be read
// again. Invalid input and a taken email fail as in Service.CreateMember.
func (service *Service) Add(ctx context.Context, email string, displayName string, createdBy uuid.UUID) (Member, string, error) {
	var member Member
	var linkURL string
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		var err error
		member, err = service.CreateMember(ctx, transaction, CreateMemberInput{Email: email, DisplayName: displayName})
		if err != nil {
			return err
		}
		linkURL, err = service.createLink(ctx, service.queries.WithTx(transaction), member.ID, LinkPurposeInvite, &createdBy)
		return err
	})
	if err != nil {
		return Member{}, "", err
	}
	return member, linkURL, nil
}

// Remove disables the member with memberID and ends all of their sessions,
// in one transaction that locks every active member, so concurrent removals
// always leave an active member who can sign in. actor is the member who
// removes them. Only active members with a password can sign in, so pending
// invitees do not count. Removing the actor or the last active member with a
// password returns ErrLastMember, and so does a removal by an actor who is no
// longer active. An unknown memberID returns httpapi.ErrNotFound. Removing a
// member who is already disabled succeeds.
func (service *Service) Remove(ctx context.Context, actor uuid.UUID, memberID uuid.UUID) error {
	if actor == memberID {
		return ErrLastMember
	}
	return database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		transactionQueries := service.queries.WithTx(transaction)
		activeMembers, err := transactionQueries.LockActiveMembers(ctx, actor)
		if err != nil {
			return fmt.Errorf("lock active members: %w", err)
		}
		if !activeMembers.ActorActive {
			return ErrLastMember
		}
		row, err := transactionQueries.SelectMemberByIDForUpdate(ctx, memberID)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select member %s for update: %w", memberID, err)
		}
		if row.Status == queries.RecordStatusActive && row.PasswordHash != nil && activeMembers.PasswordMemberCount <= 1 {
			return ErrLastMember
		}
		err = transactionQueries.UpdateMemberStatus(ctx, queries.UpdateMemberStatusParams{
			MemberID:  memberID,
			Status:    queries.RecordStatusDisabled,
			UpdatedAt: service.clock.Now(),
		})
		if err != nil {
			return fmt.Errorf("disable member %s: %w", memberID, err)
		}
		if err := transactionQueries.DeleteSessionsForMember(ctx, memberID); err != nil {
			return fmt.Errorf("delete sessions of member %s: %w", memberID, err)
		}
		return nil
	})
}

// CreateResetLink returns the URL of a new password reset link for the
// member with memberID, {PREBURN_PUBLIC_URL}/link#<token>, which expires
// after 24 hours. It expires every earlier unconsumed link of the member,
// invite links included, in the same transaction, which locks the member and
// then the links. createdBy is the member who asks for the link, or nil for
// the command line. A disabled member returns ErrMemberDisabled and an
// unknown memberID returns httpapi.ErrNotFound.
func (service *Service) CreateResetLink(ctx context.Context, memberID uuid.UUID, createdBy *uuid.UUID) (string, error) {
	var linkURL string
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		transactionQueries := service.queries.WithTx(transaction)
		row, err := transactionQueries.SelectMemberByIDForUpdate(ctx, memberID)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select member %s for update: %w", memberID, err)
		}
		if row.Status != queries.RecordStatusActive {
			return ErrMemberDisabled
		}
		err = transactionQueries.ExpireUnconsumedMemberLinks(ctx, queries.ExpireUnconsumedMemberLinksParams{MemberID: memberID, Now: service.clock.Now()})
		if err != nil {
			return fmt.Errorf("expire links of member %s: %w", memberID, err)
		}
		linkURL, err = service.createLink(ctx, transactionQueries, memberID, LinkPurposePasswordReset, createdBy)
		return err
	})
	if err != nil {
		return "", err
	}
	return linkURL, nil
}
