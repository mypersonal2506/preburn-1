package installation

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/installation/queries"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/secrets"
)

const setupPath = "setup"

// CompleteSetupInput is the setup token and the first member for
// Service.CompleteSetup.
type CompleteSetupInput struct {
	// Token is the token from the fragment of the latest setup link.
	Token string
	// Email is the email address of the first member, with the rules of
	// members.CreateMemberInput.Email.
	Email string
	// DisplayName is the display name of the first member, with the rules of
	// members.CreateMemberInput.DisplayName.
	DisplayName string
	// Password is the password of the first member, 12 to 256 characters.
	Password string
}

// Service reads and completes the setup of the installation. Create one with
// NewService. It is safe for concurrent use.
type Service struct {
	pool          *pgxpool.Pool
	queries       *queries.Queries
	members       *members.Service
	clock         clock.Clock
	configuration config.Config
}

// ErrSetupNotAvailable is the error for completing a setup that is already
// complete: 409 setup_not_available.
var ErrSetupNotAvailable = httpapi.NewCodedError(http.StatusConflict, "setup_not_available", "setup is complete")

// ErrSetupTokenInvalid is the error for a setup token that does not match the
// latest setup link: 403 setup_token_invalid.
var ErrSetupTokenInvalid = httpapi.NewCodedError(http.StatusForbidden, "setup_token_invalid", "setup token does not match the latest setup link")

// NewService returns a Service that keeps the installation in pool, creates
// members with memberService and reads time from timeSource. Setup links
// start with configuration.PublicURL, and its routes set Secure cookies when
// that URL is https and read client addresses through
// configuration.TrustedProxies. It only stores its arguments, so zero values
// serve route registration for the OpenAPI document.
func NewService(pool *pgxpool.Pool, memberService *members.Service, timeSource clock.Clock, configuration config.Config) *Service {
	return &Service{
		pool:          pool,
		queries:       queries.New(pool),
		members:       memberService,
		clock:         timeSource,
		configuration: configuration,
	}
}

// PrepareSetupLink returns a new setup link and true while setup is pending.
// It stores the SHA-256 hash of a new token in place of the previous one, so
// only the latest link completes setup. The link is
// {PREBURN_PUBLIC_URL}/setup#<token>. After setup it changes nothing and
// returns an empty link and false.
func (service *Service) PrepareSetupLink(ctx context.Context) (string, bool, error) {
	token := secrets.NewToken()
	updated, err := service.queries.SetSetupTokenHash(ctx, queries.SetSetupTokenHashParams{
		SetupTokenHash: secrets.HashToken(token),
		UpdatedAt:      service.clock.Now(),
	})
	if err != nil {
		return "", false, fmt.Errorf("set setup token hash: %w", err)
	}
	if updated == 0 {
		return "", false, nil
	}
	link := service.configuration.PublicURL.JoinPath(setupPath)
	link.Fragment = token
	return link.String(), true, nil
}

// Status reports whether setup is pending, which is until the first member
// is created.
func (service *Service) Status(ctx context.Context) (bool, error) {
	row, err := service.queries.SelectInstallation(ctx)
	if err != nil {
		return false, fmt.Errorf("select installation: %w", err)
	}
	return row.SetupCompletedAt == nil, nil
}

// CompleteSetup creates the first member from input and completes setup in
// one transaction that holds the installation row lock, so of concurrent
// calls exactly one completes setup. It returns ErrSetupNotAvailable when
// setup is complete and ErrSetupTokenInvalid when input.Token does not match
// the latest setup link, comparing hashes in constant time. An invalid member
// returns the errors of members.Service.CreateMember and leaves setup
// pending. Completing setup clears the stored token hash and records the
// time as the member's last login with members.Service.RecordLogin.
// CompleteSetup starts no session, which the caller starts once the
// transaction commits.
func (service *Service) CompleteSetup(ctx context.Context, input CompleteSetupInput) (members.Member, error) {
	var member members.Member
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		transactionQueries := service.queries.WithTx(transaction)
		row, err := transactionQueries.SelectInstallationForUpdate(ctx)
		if err != nil {
			return fmt.Errorf("select installation for update: %w", err)
		}
		if row.SetupCompletedAt != nil {
			return ErrSetupNotAvailable
		}
		if subtle.ConstantTimeCompare(secrets.HashToken(input.Token), row.SetupTokenHash) != 1 {
			return ErrSetupTokenInvalid
		}
		created, err := service.members.CreateMember(ctx, transaction, members.CreateMemberInput{
			Email:       input.Email,
			DisplayName: input.DisplayName,
			Password:    &input.Password,
		})
		if err != nil {
			return err
		}
		member, err = service.members.RecordLogin(ctx, transaction, created.ID)
		if err != nil {
			return err
		}
		return service.completeSetup(ctx, transactionQueries)
	})
	if err != nil {
		return members.Member{}, err
	}
	return member, nil
}

// CreateAdmin creates a member with a password at any time and completes
// setup when it is pending, in one transaction that holds the installation
// row lock like CompleteSetup. It needs no setup token. An invalid member
// returns the errors of members.Service.CreateMember and changes nothing.
func (service *Service) CreateAdmin(ctx context.Context, email string, displayName string, password string) (members.Member, error) {
	var member members.Member
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		transactionQueries := service.queries.WithTx(transaction)
		row, err := transactionQueries.SelectInstallationForUpdate(ctx)
		if err != nil {
			return fmt.Errorf("select installation for update: %w", err)
		}
		member, err = service.members.CreateMember(ctx, transaction, members.CreateMemberInput{
			Email:       email,
			DisplayName: displayName,
			Password:    &password,
		})
		if err != nil {
			return err
		}
		if row.SetupCompletedAt != nil {
			return nil
		}
		return service.completeSetup(ctx, transactionQueries)
	})
	if err != nil {
		return members.Member{}, err
	}
	return member, nil
}

func (service *Service) completeSetup(ctx context.Context, transactionQueries *queries.Queries) error {
	if err := transactionQueries.CompleteSetup(ctx, service.clock.Now()); err != nil {
		return fmt.Errorf("complete setup: %w", err)
	}
	return nil
}
