package dashboard

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/dashboard/queries"
	"github.com/preburn/preburn/internal/httpapi"
)

// OnboardingService reads the state of the get started checklist. Create one
// with NewOnboardingService. It is safe for concurrent use.
type OnboardingService struct {
	queries *queries.Queries
}

// NewOnboardingService returns an OnboardingService that reads pool. It only
// stores its argument, so a nil pool serves route registration for the
// OpenAPI document.
func NewOnboardingService(pool *pgxpool.Pool) *OnboardingService {
	return &OnboardingService{queries: queries.New(pool)}
}

// Onboarding returns the checklist state of environment: whether it has an
// active API key, an active plan, an active policy and any revenue entry, and
// when its earliest stored decision was checked. Decision retention deletes
// old decisions, so that time moves forward once the first decisions are
// deleted.
func (service *OnboardingService) Onboarding(ctx context.Context, environment httpapi.Environment) (OnboardingResponse, error) {
	storeEnvironment := queries.Environment(environment)
	flags, err := service.queries.SelectOnboardingFlags(ctx, storeEnvironment)
	if err != nil {
		return OnboardingResponse{}, fmt.Errorf("select onboarding flags of environment %s: %w", environment, err)
	}
	onboarding := OnboardingResponse{
		HasAPIKey:  flags.HasApiKey,
		HasPlan:    flags.HasPlan,
		HasPolicy:  flags.HasPolicy,
		HasRevenue: flags.HasRevenue,
	}
	firstCheckAt, err := service.queries.SelectFirstDecisionCreatedAt(ctx, storeEnvironment)
	if errors.Is(err, pgx.ErrNoRows) {
		return onboarding, nil
	}
	if err != nil {
		return OnboardingResponse{}, fmt.Errorf("select first decision of environment %s: %w", environment, err)
	}
	firstCheckAt = firstCheckAt.UTC()
	onboarding.FirstCheckAt = &firstCheckAt
	return onboarding, nil
}
