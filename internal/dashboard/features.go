package dashboard

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/dashboard/queries"
	"github.com/preburn/preburn/internal/httpapi"
)

const featureDecisionDays = 30

// FeatureSource is where a known feature appears.
type FeatureSource string

const (
	// FeatureSourceDecisions is a decision of the last 30 days.
	FeatureSourceDecisions FeatureSource = "decisions"
	// FeatureSourcePlanHoldTimes is the hold times of an active plan.
	FeatureSourcePlanHoldTimes FeatureSource = "plan_hold_times"
	// FeatureSourcePolicies is an active policy scoped to the feature.
	FeatureSourcePolicies FeatureSource = "policies"
	// FeatureSourceUsageEstimates is a usage estimate of the feature.
	FeatureSourceUsageEstimates FeatureSource = "usage_estimates"
)

// FeatureService reads the features an environment knows, for the feature
// pickers of the dashboard. Create one with NewFeatureService. It is safe for
// concurrent use.
type FeatureService struct {
	queries *queries.Queries
	clock   clock.Clock
}

// NewFeatureService returns a FeatureService that reads pool and takes the
// time from timeSource. It only stores its arguments, so zero values serve
// route registration for the OpenAPI document.
func NewFeatureService(pool *pgxpool.Pool, timeSource clock.Clock) *FeatureService {
	return &FeatureService{queries: queries.New(pool), clock: timeSource}
}

// Features returns every feature of environment that a usage estimate, an
// active policy, the hold times of an active plan or a decision of the last
// 30 days names, in feature name order, each with the sources that name it
// in source name order.
func (service *FeatureService) Features(ctx context.Context, environment httpapi.Environment) ([]KnownFeatureResponse, error) {
	rows, err := service.queries.ListFeatureSources(ctx, queries.ListFeatureSourcesParams{
		Environment: queries.Environment(environment),
		Since:       service.clock.Now().AddDate(0, 0, -featureDecisionDays),
	})
	if err != nil {
		return nil, fmt.Errorf("list feature sources of environment %s: %w", environment, err)
	}
	features := []KnownFeatureResponse{}
	for _, row := range rows {
		if len(features) == 0 || features[len(features)-1].Feature != row.Feature {
			features = append(features, KnownFeatureResponse{Feature: row.Feature, Sources: []FeatureSource{}})
		}
		known := &features[len(features)-1]
		known.Sources = append(known.Sources, FeatureSource(row.Source))
	}
	return features, nil
}
