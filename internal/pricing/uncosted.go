package pricing

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing/queries"
)

const (
	uncostedRerateKind  = "uncosted_rerate"
	uncostedListingName = "pricing_uncosted"
)

// RerateWindow is how far before now uncosted_rerate re-rates uncosted
// ledger entries and ListUncosted counts them: the price history a RuleSet
// holds.
const RerateWindow = ruleSetHistoryWindow

// UncostedRerateArgs is the job of kind uncosted_rerate, which re-rates the
// uncosted ledger entries of one environment after an override change. The
// ledger package works it. It runs on the ledger queue, and River inserts no
// second job for an environment while one is available, pending, scheduled,
// retryable or running.
type UncostedRerateArgs struct {
	// Environment is the environment whose ledger entries the job re-rates.
	Environment httpapi.Environment `json:"environment"`
}

// UncostedUsage is the uncosted usage of one meter of one provider model:
// the requests whose ledger entry has no price for the meter and no
// correction.
type UncostedUsage struct {
	Provider     string    `json:"provider" doc:"Provider name the requests ran on."`
	Model        string    `json:"model" doc:"Model name the requests were reported with."`
	Meter        Meter     `json:"meter" doc:"Meter that no rule or override prices."`
	RequestCount int64     `json:"request_count" doc:"Requests of the last 90 days whose usage of the meter has no price and no correction."`
	LastSeenAt   time.Time `json:"last_seen_at" doc:"When the latest of those requests ran."`
}

type uncostedPosition struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Meter    Meter  `json:"meter"`
}

var uncostedCursor = httpapi.NewCursor[uncostedPosition](uncostedListingName)

// Kind returns uncosted_rerate.
func (UncostedRerateArgs) Kind() string {
	return uncostedRerateKind
}

// InsertOpts places uncosted_rerate jobs on the ledger queue and makes them
// unique by arguments while available, pending, scheduled, retryable or
// running. River requires running in that set.
func (UncostedRerateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: jobs.QueueLedger,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
			ByState: []rivertype.JobState{
				rivertype.JobStateAvailable,
				rivertype.JobStatePending,
				rivertype.JobStateRetryable,
				rivertype.JobStateRunning,
				rivertype.JobStateScheduled,
			},
		},
	}
}

// RateUncosted prices request as Rate does at request.OccurredAt, then prices
// each meter that nothing priced then with the overrides and rules of ruleSet
// in effect at now, so a price that starts after the request still prices
// it. The result is costed when every meter has a price at one of the two
// times, and its cost is the sum of its lines. It returns the errors of Rate.
func RateUncosted(request RatingRequest, ruleSet *RuleSet, now time.Time) (RatedRequest, error) {
	rated, err := Rate(request, ruleSet)
	if err != nil || rated.CostStatus == CostStatusCosted {
		return rated, err
	}
	missingUsage := map[Meter]money.Quantity{}
	for _, line := range rated.Lines {
		if line.Missing {
			missingUsage[line.Meter] = request.Usage[line.Meter]
		}
	}
	current := request
	current.Usage = missingUsage
	current.OccurredAt = now
	pricedNow, err := Rate(current, ruleSet)
	if err != nil {
		return RatedRequest{}, err
	}
	nowLines := make(map[Meter]RatedLine, len(pricedNow.Lines))
	for _, line := range pricedNow.Lines {
		nowLines[line.Meter] = line
	}
	for index, line := range rated.Lines {
		if line.Missing {
			rated.Lines[index] = nowLines[line.Meter]
		}
	}
	if pricedNow.CostStatus == CostStatusUncosted {
		return rated, nil
	}
	costs := make([]money.Amount, 0, len(rated.Lines))
	for _, line := range rated.Lines {
		costs = append(costs, line.Cost)
	}
	cost, err := money.Sum(costs)
	if err != nil {
		return RatedRequest{}, fmt.Errorf("sum line costs: %w", err)
	}
	rated.CostStatus = CostStatusCosted
	rated.Cost = &cost
	return rated, nil
}

// LoadRuleSet reads the RuleSet of environment from Postgres as the
// RuleSetCache loads it, without the cache, so it holds every override change
// committed before the call.
func (service *Service) LoadRuleSet(ctx context.Context, environment httpapi.Environment) (*RuleSet, error) {
	return service.loadRuleSet(ctx, environment)
}

// OverrideDigest returns a digest of every stored field of every override of
// environment. Creating, changing or ending an override changes it.
func (service *Service) OverrideDigest(ctx context.Context, environment httpapi.Environment) (string, error) {
	digest, err := service.queries.SelectPricingOverrideDigest(ctx, queries.Environment(environment))
	if err != nil {
		return "", fmt.Errorf("select override digest of environment %s: %w", environment, err)
	}
	return digest, nil
}

// ListUncosted returns one page of the uncosted usage of environment, one
// UncostedUsage per provider, model and missing meter, ordered by provider,
// model and meter, and the cursor of the next page, which is empty on the
// last page. It counts the uncosted ledger entries without a correction that
// occurred within RerateWindow before now, once for each meter their cost
// breakdown marks missing. A limit of 0 selects httpapi.ListLimitDefault,
// and a limit outside 1 to httpapi.ListLimitMaximum returns a 422
// validation_failed problem at query.limit. A cursor that ListUncosted did
// not return for environment fails with httpapi.ErrInvalidCursor.
func (service *Service) ListUncosted(ctx context.Context, environment httpapi.Environment, cursor string, limit int) ([]UncostedUsage, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	parameters := queries.ListUncostedUsageParams{
		Environment:   queries.Environment(environment),
		OccurredSince: service.clock.Now().Add(-RerateWindow),
		RowLimit:      int64(pageSize) + 1,
	}
	if cursor != "" {
		position, err := uncostedCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		afterMeter := string(position.Meter)
		parameters.AfterProvider = &position.Provider
		parameters.AfterModel = &position.Model
		parameters.AfterMeter = &afterMeter
	}
	rows, err := service.queries.ListUncostedUsage(ctx, parameters)
	if err != nil {
		return nil, "", fmt.Errorf("list uncosted usage of environment %s: %w", environment, err)
	}
	nextCursor := ""
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		last := rows[pageSize-1]
		nextCursor, err = uncostedCursor.Encode(environment, uncostedPosition{Provider: last.Provider, Model: last.Model, Meter: Meter(last.Meter)})
		if err != nil {
			return nil, "", err
		}
	}
	usage := make([]UncostedUsage, 0, len(rows))
	for _, row := range rows {
		usage = append(usage, UncostedUsage{
			Provider:     row.Provider,
			Model:        row.Model,
			Meter:        Meter(row.Meter),
			RequestCount: row.RequestCount,
			LastSeenAt:   row.LastSeenAt.UTC(),
		})
	}
	return usage, nextCursor, nil
}

func (service *Service) insertUncostedRerate(ctx context.Context, transaction pgx.Tx, environment httpapi.Environment) error {
	if _, err := service.jobs.InsertTx(ctx, transaction, UncostedRerateArgs{Environment: environment}, nil); err != nil {
		return fmt.Errorf("insert uncosted rerate of environment %s: %w", environment, err)
	}
	return nil
}
