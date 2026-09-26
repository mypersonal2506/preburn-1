package decisions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/decisions/queries"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/logging"
)

const (
	retentionKind      = "decision_retention"
	retentionBatchSize = 1000
	retentionHour      = 3
	retentionMinute    = 0
	retentionTimeout   = time.Hour
	retentionDay       = 24 * time.Hour
)

// RetentionArgs is the job of kind decision_retention, which deletes the
// decisions older than PREBURN_DECISION_RETENTION_DAYS. It has no arguments
// and runs on the maintenance queue.
type RetentionArgs struct{}

// RetentionWorker works decision_retention jobs. Create one with
// NewRetentionWorker.
type RetentionWorker struct {
	river.WorkerDefaults[RetentionArgs]
	queries   *queries.Queries
	retention time.Duration
	clock     clock.Clock
	logger    *logging.Logger
}

// NewRetentionWorker returns a RetentionWorker that deletes from pool the
// decisions created more than retentionDays days before the time timeSource
// tells, and logs to logger. The configuration bounds retentionDays so the
// retention fits a time.Duration.
func NewRetentionWorker(pool *pgxpool.Pool, retentionDays int, timeSource clock.Clock, logger *logging.Logger) *RetentionWorker {
	return &RetentionWorker{
		queries:   queries.New(pool),
		retention: time.Duration(retentionDays) * retentionDay,
		clock:     timeSource,
		logger:    logger,
	}
}

// RetentionPeriodicJob returns the periodic job that inserts a
// decision_retention job every day at 03:00 UTC.
func RetentionPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(jobs.DailyAt(retentionHour, retentionMinute), func() (river.JobArgs, *river.InsertOpts) {
		return RetentionArgs{}, nil
	}, nil)
}

// Kind returns decision_retention.
func (RetentionArgs) Kind() string {
	return retentionKind
}

// InsertOpts places decision_retention jobs on the maintenance queue.
func (RetentionArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance}
}

// Timeout gives a decision_retention job one hour. A job that runs out keeps
// the batches it deleted, and its retry continues from there.
func (worker *RetentionWorker) Timeout(*river.Job[RetentionArgs]) time.Duration {
	return retentionTimeout
}

// Work deletes the decisions of the test and then the live environment
// whose created_at is before the retention cutoff, 1,000 at a time in id
// order, each batch starting after the last id the previous one deleted,
// until a batch comes back short. Ledger entries keep the ids of the
// decisions it deletes. It logs decisions.retention_completed with
// deleted_decisions.
func (worker *RetentionWorker) Work(ctx context.Context, _ *river.Job[RetentionArgs]) error {
	cutoff := worker.clock.Now().Add(-worker.retention)
	var deletedTotal int
	for _, environment := range []httpapi.Environment{httpapi.EnvironmentTest, httpapi.EnvironmentLive} {
		deleted, err := worker.deleteExpired(ctx, environment, cutoff)
		if err != nil {
			return fmt.Errorf("delete expired %s decisions: %w", environment, err)
		}
		deletedTotal += deleted
	}
	worker.logger.Info(ctx, logging.DecisionsRetentionCompleted, slog.Int("deleted_decisions", deletedTotal))
	return nil
}

func (worker *RetentionWorker) deleteExpired(ctx context.Context, environment httpapi.Environment, cutoff time.Time) (int, error) {
	var deletedTotal int
	afterDecisionID := uuid.Nil
	for {
		deletedIDs, err := worker.queries.DeleteExpiredDecisions(ctx, queries.DeleteExpiredDecisionsParams{
			Environment:     queries.Environment(environment),
			CreatedBefore:   cutoff,
			AfterDecisionID: afterDecisionID,
			BatchSize:       retentionBatchSize,
		})
		if err != nil {
			return 0, err
		}
		deletedTotal += len(deletedIDs)
		if len(deletedIDs) < retentionBatchSize {
			return deletedTotal, nil
		}
		afterDecisionID = deletedIDs[len(deletedIDs)-1]
	}
}
