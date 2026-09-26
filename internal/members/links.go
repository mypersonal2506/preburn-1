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
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/members/queries"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	linkLifetime = 24 * time.Hour
	linkPath     = "link"
)

// LinkPurpose is what a one-time member link lets its holder do. Both
// purposes set the member's password.
type LinkPurpose string

const (
	// LinkPurposeInvite links set the first password of a member added
	// through Service.Add.
	LinkPurposeInvite LinkPurpose = "invite"
	// LinkPurposePasswordReset links replace the password of a member.
	LinkPurposePasswordReset LinkPurpose = "password_reset"
)

// LinkDetails describes a usable link to the person who opened it, before
// they choose a password.
type LinkDetails struct {
	// Purpose tells whether the link accepts an invite or resets a password.
	Purpose LinkPurpose
	// Email is the email address of the link's member.
	Email string
	// DisplayName is the display name of the link's member.
	DisplayName string
}

// ErrLinkExpired is the error for a link token that is unknown, consumed,
// past its 24 hour lifetime, or belongs to a disabled member: 410
// link_expired.
var ErrLinkExpired = httpapi.NewCodedError(http.StatusGone, "link_expired", "the link has expired or was used")

// InspectLink returns the details of the usable link with token. A token
// that is not usable returns ErrLinkExpired.
func (service *Service) InspectLink(ctx context.Context, token string) (LinkDetails, error) {
	row, err := service.queries.SelectMemberLinkWithMember(ctx, secrets.HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return LinkDetails{}, ErrLinkExpired
	}
	if err != nil {
		return LinkDetails{}, fmt.Errorf("select member link: %w", err)
	}
	if !linkUsable(row.MemberLink, row.Member, service.clock.Now()) {
		return LinkDetails{}, ErrLinkExpired
	}
	return LinkDetails{
		Purpose:     LinkPurpose(row.MemberLink.Purpose),
		Email:       row.Member.Email,
		DisplayName: row.Member.DisplayName,
	}, nil
}

// ConsumeLink sets password as the password of the member of the usable
// link with token, marks the link consumed, expires every other unconsumed
// link of the member, ends every session of the member and records the time
// as the member's last login like RecordLogin, in one transaction that locks
// the member and then the link. It returns the member and starts no session,
// which the caller starts once the transaction commits. A password outside
// 12 to 256 characters returns a 422 validation_failed problem at
// body.password, and a token that is not usable returns ErrLinkExpired, both
// leaving the link unconsumed.
func (service *Service) ConsumeLink(ctx context.Context, token string, password string) (Member, error) {
	if !validPassword(password) {
		return Member{}, httpapi.NewValidationProblem(httpapi.ProblemError{Location: passwordLocation, Message: passwordRule})
	}
	found, err := service.queries.SelectMemberLinkWithMember(ctx, secrets.HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrLinkExpired
	}
	if err != nil {
		return Member{}, fmt.Errorf("select member link: %w", err)
	}
	var member Member
	err = database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		transactionQueries := service.queries.WithTx(transaction)
		// Lock the member before the link, the order CreateResetLink uses, or the two deadlock.
		memberRow, err := transactionQueries.SelectMemberByIDForUpdate(ctx, found.Member.MemberID)
		if err != nil {
			return fmt.Errorf("select member %s for update: %w", found.Member.MemberID, err)
		}
		link, err := transactionQueries.SelectMemberLinkForUpdate(ctx, found.MemberLink.MemberLinkID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkExpired
		}
		if err != nil {
			return fmt.Errorf("select member link %s for update: %w", found.MemberLink.MemberLinkID, err)
		}
		now := service.clock.Now()
		if !linkUsable(link, memberRow, now) {
			return ErrLinkExpired
		}
		passwordHash := secrets.HashPassword(password)
		err = transactionQueries.UpdateMemberPasswordHash(ctx, queries.UpdateMemberPasswordHashParams{
			MemberID:     memberRow.MemberID,
			PasswordHash: &passwordHash,
			UpdatedAt:    now,
		})
		if err != nil {
			return fmt.Errorf("set password of member %s: %w", memberRow.MemberID, err)
		}
		err = transactionQueries.ConsumeMemberLink(ctx, queries.ConsumeMemberLinkParams{MemberLinkID: link.MemberLinkID, ConsumedAt: now})
		if err != nil {
			return fmt.Errorf("consume member link %s: %w", link.MemberLinkID, err)
		}
		err = transactionQueries.ExpireUnconsumedMemberLinks(ctx, queries.ExpireUnconsumedMemberLinksParams{MemberID: memberRow.MemberID, Now: now})
		if err != nil {
			return fmt.Errorf("expire other links of member %s: %w", memberRow.MemberID, err)
		}
		if err := transactionQueries.DeleteSessionsForMember(ctx, memberRow.MemberID); err != nil {
			return fmt.Errorf("delete sessions of member %s: %w", memberRow.MemberID, err)
		}
		memberRow, err = service.recordLogin(ctx, transactionQueries, memberRow.MemberID)
		if err != nil {
			return err
		}
		member = memberFromRow(memberRow)
		return nil
	})
	if err != nil {
		return Member{}, err
	}
	return member, nil
}

func (service *Service) createLink(ctx context.Context, transactionQueries *queries.Queries, memberID uuid.UUID, purpose LinkPurpose, createdBy *uuid.UUID) (string, error) {
	token := secrets.NewToken()
	now := service.clock.Now()
	err := transactionQueries.InsertMemberLink(ctx, queries.InsertMemberLinkParams{
		MemberLinkID:      identifiers.New(),
		MemberID:          memberID,
		Purpose:           string(purpose),
		TokenHash:         secrets.HashToken(token),
		ExpiresAt:         now.Add(linkLifetime),
		CreatedByMemberID: createdBy,
		CreatedAt:         now,
	})
	if err != nil {
		return "", fmt.Errorf("insert %s link of member %s: %w", purpose, memberID, err)
	}
	linkURL := service.configuration.PublicURL.JoinPath(linkPath)
	linkURL.Fragment = token
	return linkURL.String(), nil
}

func linkUsable(link queries.MemberLink, member queries.Member, now time.Time) bool {
	return link.ConsumedAt == nil && link.ExpiresAt.After(now) && member.Status == queries.RecordStatusActive
}
