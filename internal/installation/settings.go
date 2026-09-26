package installation

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/installation/queries"
	"github.com/preburn/preburn/internal/plans"
)

const (
	installationNameMaximumLength     = 80
	installationNameLocation          = "body.installation_name"
	stripeCustomerMetadataKeyLocation = "body.stripe_customer_metadata_key"
)

// Settings are the installation settings as one environment sees them: the
// installation name, which every environment shares, and the settings of the
// environment.
type Settings struct {
	// InstallationName is the name of the installation.
	InstallationName string
	// DefaultPlanID is the plan of the environment's customers that have no
	// plan, or nil for none.
	DefaultPlanID *uuid.UUID
	// StripeCustomerMetadataKey is the Stripe customer metadata key that holds
	// a customer's external id in the environment.
	StripeCustomerMetadataKey string
}

// SettingsUpdate is a change to the settings for SettingsService.Update. A
// nil field keeps the stored value.
type SettingsUpdate struct {
	// InstallationName replaces the installation name, 1 to 80 characters
	// without control characters.
	InstallationName *string
	// ReplaceDefaultPlan tells whether DefaultPlanID replaces the default plan
	// of the environment.
	ReplaceDefaultPlan bool
	// DefaultPlanID is the new default plan, an active plan of the
	// environment, or nil to remove the default plan. It applies only when
	// ReplaceDefaultPlan is true.
	DefaultPlanID *uuid.UUID
	// StripeCustomerMetadataKey replaces the metadata key of the environment.
	// It matches ^[a-z][a-z0-9_]{0,39}$.
	StripeCustomerMetadataKey *string
}

// SettingsService reads and changes the installation settings of each
// environment. Create one with NewSettingsService. It is safe for concurrent
// use.
type SettingsService struct {
	pool    *pgxpool.Pool
	queries *queries.Queries
	cache   *cache.Client
	clock   clock.Clock
}

var (
	stripeCustomerMetadataKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	installationNameRule             = fmt.Sprintf("expected 1 to %d characters without control characters", installationNameMaximumLength)
	stripeCustomerMetadataKeyRule    = "expected a key matching " + stripeCustomerMetadataKeyPattern.String()
)

// NewSettingsService returns a SettingsService that keeps the settings in
// pool, publishes the settings invalidation through cacheClient and reads
// time from timeSource. It only stores its arguments, so zero values serve
// route registration for the OpenAPI document.
func NewSettingsService(pool *pgxpool.Pool, cacheClient *cache.Client, timeSource clock.Clock) *SettingsService {
	return &SettingsService{pool: pool, queries: queries.New(pool), cache: cacheClient, clock: timeSource}
}

// Get returns the settings as environment sees them.
func (service *SettingsService) Get(ctx context.Context, environment httpapi.Environment) (Settings, error) {
	row, err := service.queries.SelectSettings(ctx, queries.Environment(environment))
	if err != nil {
		return Settings{}, fmt.Errorf("select settings of environment %s: %w", environment, err)
	}
	return Settings{
		InstallationName:          row.InstallationName,
		DefaultPlanID:             row.DefaultPlanID,
		StripeCustomerMetadataKey: row.StripeCustomerMetadataKey,
	}, nil
}

// Update applies update to the settings of environment in one transaction
// that holds the installation and environment rows, publishes the settings
// invalidation and returns the new settings. An invalid name or metadata key
// returns a 422 validation_failed problem at body.installation_name and
// body.stripe_customer_metadata_key. A default plan that is not an active
// plan of environment returns plans.ErrPlanNotFound. The plan row stays
// share-locked until the transaction ends, so a concurrent archive either
// waits or makes Update fail.
func (service *SettingsService) Update(ctx context.Context, environment httpapi.Environment, update SettingsUpdate) (Settings, error) {
	if problems := update.problems(); len(problems) > 0 {
		return Settings{}, httpapi.NewValidationProblem(problems...)
	}
	var settings Settings
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		transactionQueries := service.queries.WithTx(transaction)
		current, err := transactionQueries.SelectSettingsForUpdate(ctx, queries.Environment(environment))
		if err != nil {
			return fmt.Errorf("select settings of environment %s for update: %w", environment, err)
		}
		settings = Settings{
			InstallationName:          current.InstallationName,
			DefaultPlanID:             current.DefaultPlanID,
			StripeCustomerMetadataKey: current.StripeCustomerMetadataKey,
		}
		if update.InstallationName != nil {
			settings.InstallationName = *update.InstallationName
		}
		if update.StripeCustomerMetadataKey != nil {
			settings.StripeCustomerMetadataKey = *update.StripeCustomerMetadataKey
		}
		if update.ReplaceDefaultPlan {
			if update.DefaultPlanID != nil {
				if err := lockActivePlan(ctx, transactionQueries, environment, *update.DefaultPlanID); err != nil {
					return err
				}
			}
			settings.DefaultPlanID = update.DefaultPlanID
		}
		now := service.clock.Now()
		if err := transactionQueries.UpdateInstallationName(ctx, queries.UpdateInstallationNameParams{Name: settings.InstallationName, UpdatedAt: now}); err != nil {
			return fmt.Errorf("update installation name: %w", err)
		}
		err = transactionQueries.UpdateEnvironmentSettings(ctx, queries.UpdateEnvironmentSettingsParams{
			DefaultPlanID:             settings.DefaultPlanID,
			StripeCustomerMetadataKey: settings.StripeCustomerMetadataKey,
			UpdatedAt:                 now,
			Environment:               queries.Environment(environment),
		})
		if err != nil {
			return fmt.Errorf("update settings of environment %s: %w", environment, err)
		}
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	err = service.cache.PublishInvalidation(ctx, cache.Invalidation{Kind: cache.InvalidationKindSettings, Environment: string(environment)})
	if err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (update SettingsUpdate) problems() []httpapi.ProblemError {
	var problems []httpapi.ProblemError
	if update.InstallationName != nil && !validInstallationName(*update.InstallationName) {
		problems = append(problems, httpapi.ProblemError{Location: installationNameLocation, Message: installationNameRule})
	}
	if update.StripeCustomerMetadataKey != nil && !stripeCustomerMetadataKeyPattern.MatchString(*update.StripeCustomerMetadataKey) {
		problems = append(problems, httpapi.ProblemError{Location: stripeCustomerMetadataKeyLocation, Message: stripeCustomerMetadataKeyRule})
	}
	return problems
}

func lockActivePlan(ctx context.Context, transactionQueries *queries.Queries, environment httpapi.Environment, planID uuid.UUID) error {
	_, err := transactionQueries.SelectActivePlanForShare(ctx, queries.SelectActivePlanForShareParams{
		Environment: queries.Environment(environment),
		PlanID:      planID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return plans.ErrPlanNotFound
	}
	if err != nil {
		return fmt.Errorf("select active plan %s for share: %w", planID, err)
	}
	return nil
}

func validInstallationName(name string) bool {
	return strings.TrimSpace(name) != "" &&
		utf8.RuneCountInString(name) <= installationNameMaximumLength &&
		utf8.ValidString(name) &&
		!strings.ContainsFunc(name, unicode.IsControl)
}
