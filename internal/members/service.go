package members

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/netip"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/members/queries"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	emailMaximumLength       = 254
	displayNameMaximumLength = 80
	passwordMinimumLength    = 12
	passwordMaximumLength    = 256

	emailLocation           = "body.email"
	displayNameLocation     = "body.display_name"
	passwordLocation        = "body.password"
	currentPasswordLocation = "body.password.current"
	newPasswordLocation     = "body.password.new"
)

// Status is the state of a member. Only active members sign in.
type Status string

const (
	// StatusActive members sign in and keep their sessions.
	StatusActive Status = "active"
	// StatusDisabled members are removed: they cannot sign in and their
	// sessions stop working.
	StatusDisabled Status = "disabled"
)

// Member is a person who signs in to the dashboard of the installation.
type Member struct {
	// ID is the member's UUID, exposed with the prefix mem.
	ID uuid.UUID
	// Email is the address the member signs in with, unique ignoring case.
	Email string
	// DisplayName is the name the dashboard shows.
	DisplayName string
	// Status tells whether the member can sign in.
	Status Status
	// HasPassword is false until an invited member sets a password.
	HasPassword bool
	// LastLoginAt is when the member last signed in with a password login,
	// setup, or an invite or reset link, in UTC, and nil when they never did.
	LastLoginAt *time.Time
	// CreatedAt is when the member was created, in UTC.
	CreatedAt time.Time
}

// CreateMemberInput is a new member for Service.CreateMember.
type CreateMemberInput struct {
	// Email is an address of at most 254 characters, without a display name
	// part or surrounding spaces.
	Email string
	// DisplayName is 1 to 80 characters with no control characters and not
	// only spaces.
	DisplayName string
	// Password is 12 to 256 characters, or nil for a member whose invite is
	// not accepted yet.
	Password *string
}

// AccountUpdate is a change members make to their own account through
// Service.UpdateAccount. Nil fields stay unchanged.
type AccountUpdate struct {
	// DisplayName replaces the display name, with the rules of
	// CreateMemberInput.DisplayName.
	DisplayName *string
	// Password changes the password, which starts a new session and ends
	// every other session of the member.
	Password *PasswordChange
	// UserAgent is the User-Agent of the client, stored with the new session
	// a password change starts.
	UserAgent string
	// ClientIP is the address of the client, stored with the new session a
	// password change starts.
	ClientIP netip.Addr
}

// PasswordChange replaces a member's password.
type PasswordChange struct {
	// Current must match the member's password.
	Current string
	// New is the new password, with the rules of CreateMemberInput.Password.
	New string
}

// Service manages members, their passwords, sessions and logins. Create one
// with NewService. It is safe for concurrent use.
type Service struct {
	pool          *pgxpool.Pool
	queries       *queries.Queries
	sessions      *SessionStore
	rateLimiter   *RateLimiter
	clock         clock.Clock
	configuration config.Config
}

// ErrEmailTaken is the error for a new member whose email another member has
// in any letter case: 409 member_email_taken.
var ErrEmailTaken = httpapi.NewCodedError(http.StatusConflict, "member_email_taken", "a member with this email exists")

var (
	emailRule       = fmt.Sprintf("expected an email address of at most %d characters", emailMaximumLength)
	displayNameRule = fmt.Sprintf("expected 1 to %d characters without control characters", displayNameMaximumLength)
	passwordRule    = fmt.Sprintf("expected %d to %d characters", passwordMinimumLength, passwordMaximumLength)
)

// NewService returns a Service that stores members and sessions in pool,
// counts login attempts in cacheClient and reads time from timeSource. Its
// routes set Secure cookies when configuration.PublicURL is https and read
// client addresses through configuration.TrustedProxies. It only stores its
// arguments, so zero values serve route registration for the OpenAPI
// document.
func NewService(pool *pgxpool.Pool, cacheClient *cache.Client, timeSource clock.Clock, configuration config.Config) *Service {
	return &Service{
		pool:          pool,
		queries:       queries.New(pool),
		sessions:      NewSessionStore(pool, timeSource),
		rateLimiter:   NewRateLimiter(cacheClient, timeSource),
		clock:         timeSource,
		configuration: configuration,
	}
}

// Sessions returns the SessionStore of the service's members.
func (service *Service) Sessions() *SessionStore {
	return service.sessions
}

// CreateMember adds an active member inside transaction, which the caller
// commits. An invalid input returns a 422 validation_failed problem that
// names every invalid field, at body.email, body.display_name and
// body.password. An email another member has in any letter case returns
// ErrEmailTaken and leaves transaction usable.
func (service *Service) CreateMember(ctx context.Context, transaction pgx.Tx, input CreateMemberInput) (Member, error) {
	if problems := input.problems(); len(problems) > 0 {
		return Member{}, httpapi.NewValidationProblem(problems...)
	}
	var passwordHash *string
	if input.Password != nil {
		hash := secrets.HashPassword(*input.Password)
		passwordHash = &hash
	}
	row, err := service.queries.WithTx(transaction).InsertMember(ctx, queries.InsertMemberParams{
		MemberID:     identifiers.New(),
		Email:        input.Email,
		DisplayName:  input.DisplayName,
		PasswordHash: passwordHash,
		CreatedAt:    service.clock.Now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrEmailTaken
	}
	if err != nil {
		return Member{}, fmt.Errorf("insert member: %w", err)
	}
	return memberFromRow(row), nil
}

// Member returns the member with memberID, or httpapi.ErrNotFound when there
// is none.
func (service *Service) Member(ctx context.Context, memberID uuid.UUID) (Member, error) {
	row, err := service.queries.SelectMemberByID(ctx, memberID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, httpapi.ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("select member %s: %w", memberID, err)
	}
	return memberFromRow(row), nil
}

// MemberByEmail returns the member whose email is email in any letter case,
// or httpapi.ErrNotFound when there is none.
func (service *Service) MemberByEmail(ctx context.Context, email string) (Member, error) {
	row, err := service.queries.SelectMemberByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, httpapi.ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("select member by email: %w", err)
	}
	return memberFromRow(row), nil
}

// UpdateAccount applies update to the member with memberID in one
// transaction and returns the updated member. A password change also starts
// a session and ends every other session of the member, and its token is
// returned. The token is empty when the password stays unchanged. Invalid
// fields return a 422 validation_failed problem at body.display_name or
// body.password.new, and a current password that does not match returns one
// at body.password.current.
func (service *Service) UpdateAccount(ctx context.Context, memberID uuid.UUID, update AccountUpdate) (Member, string, error) {
	if problems := update.problems(); len(problems) > 0 {
		return Member{}, "", httpapi.NewValidationProblem(problems...)
	}
	var member Member
	var sessionToken string
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		transactionQueries := service.queries.WithTx(transaction)
		row, err := transactionQueries.SelectMemberByIDForUpdate(ctx, memberID)
		if err != nil {
			return fmt.Errorf("select member %s for update: %w", memberID, err)
		}
		if update.DisplayName != nil {
			row, err = transactionQueries.UpdateMemberDisplayName(ctx, queries.UpdateMemberDisplayNameParams{
				MemberID:    memberID,
				DisplayName: *update.DisplayName,
				UpdatedAt:   service.clock.Now(),
			})
			if err != nil {
				return fmt.Errorf("update display name of member %s: %w", memberID, err)
			}
		}
		if update.Password != nil {
			sessionToken, err = service.changePassword(ctx, transactionQueries, row, update)
			if err != nil {
				return err
			}
		}
		member = memberFromRow(row)
		return nil
	})
	if err != nil {
		return Member{}, "", err
	}
	return member, sessionToken, nil
}

func (service *Service) changePassword(ctx context.Context, transactionQueries *queries.Queries, row queries.Member, update AccountUpdate) (string, error) {
	if row.PasswordHash == nil {
		return "", wrongCurrentPassword()
	}
	matched, _, err := secrets.VerifyPassword(update.Password.Current, *row.PasswordHash)
	if err != nil {
		return "", fmt.Errorf("verify password of member %s: %w", row.MemberID, err)
	}
	if !matched {
		return "", wrongCurrentPassword()
	}
	passwordHash := secrets.HashPassword(update.Password.New)
	err = transactionQueries.UpdateMemberPasswordHash(ctx, queries.UpdateMemberPasswordHashParams{
		MemberID:     row.MemberID,
		PasswordHash: &passwordHash,
		UpdatedAt:    service.clock.Now(),
	})
	if err != nil {
		return "", fmt.Errorf("update password of member %s: %w", row.MemberID, err)
	}
	sessionToken, err := service.sessions.start(ctx, transactionQueries, row.MemberID, update.UserAgent, update.ClientIP)
	if err != nil {
		return "", err
	}
	if err := service.sessions.endOthers(ctx, transactionQueries, row.MemberID, sessionToken); err != nil {
		return "", err
	}
	return sessionToken, nil
}

func (input CreateMemberInput) problems() []httpapi.ProblemError {
	var problems []httpapi.ProblemError
	if !validEmail(input.Email) {
		problems = append(problems, httpapi.ProblemError{Location: emailLocation, Message: emailRule})
	}
	if !validDisplayName(input.DisplayName) {
		problems = append(problems, httpapi.ProblemError{Location: displayNameLocation, Message: displayNameRule})
	}
	if input.Password != nil && !validPassword(*input.Password) {
		problems = append(problems, httpapi.ProblemError{Location: passwordLocation, Message: passwordRule})
	}
	return problems
}

func (update AccountUpdate) problems() []httpapi.ProblemError {
	var problems []httpapi.ProblemError
	if update.DisplayName != nil && !validDisplayName(*update.DisplayName) {
		problems = append(problems, httpapi.ProblemError{Location: displayNameLocation, Message: displayNameRule})
	}
	if update.Password != nil && !validPassword(update.Password.New) {
		problems = append(problems, httpapi.ProblemError{Location: newPasswordLocation, Message: passwordRule})
	}
	return problems
}

func wrongCurrentPassword() error {
	return httpapi.NewValidationProblem(httpapi.ProblemError{Location: currentPasswordLocation, Message: "does not match the current password"})
}

func validEmail(email string) bool {
	if utf8.RuneCountInString(email) > emailMaximumLength {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email
}

func validDisplayName(displayName string) bool {
	return strings.TrimSpace(displayName) != "" &&
		utf8.RuneCountInString(displayName) <= displayNameMaximumLength &&
		utf8.ValidString(displayName) &&
		!strings.ContainsFunc(displayName, unicode.IsControl)
}

func validPassword(password string) bool {
	length := utf8.RuneCountInString(password)
	return length >= passwordMinimumLength && length <= passwordMaximumLength
}

func memberFromRow(row queries.Member) Member {
	member := Member{
		ID:          row.MemberID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Status:      Status(row.Status),
		HasPassword: row.PasswordHash != nil,
		CreatedAt:   row.CreatedAt.UTC(),
	}
	if row.LastLoginAt != nil {
		member.LastLoginAt = new(row.LastLoginAt.UTC())
	}
	return member
}
