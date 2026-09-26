package members

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/members/queries"
)

const (
	cleanupKind          = "member_cleanup"
	cleanupBatchSize     = 1000
	cleanupLinkRetention = 7 * 24 * time.Hour
	cleanupHour          = 4
	cleanupMinute        = 0
)

// CleanupArgs is the job of kind member_cleanup, which deletes expired
// sessions and old member links. It has no arguments and runs on the
// maintenance queue.
type CleanupArgs struct{}

// CleanupWorker works member_cleanup jobs. Create one with NewCleanupWorker.
type CleanupWorker struct {
	river.WorkerDefaults[CleanupArgs]
	queries *queries.Queries
	clock   clock.Clock
	logger  *logging.Logger
}

// NewCleanupWorker returns a CleanupWorker that deletes from pool, reads
// time from timeSource and logs to logger.
func NewCleanupWorker(pool *pgxpool.Pool, timeSource clock.Clock, logger *logging.Logger) *CleanupWorker {
	return &CleanupWorker{queries: queries.New(pool), clock: timeSource, logger: logger}
}

// CleanupPeriodicJob returns the periodic job that inserts a member_cleanup
// job every day at 04:00 UTC.
func CleanupPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(jobs.DailyAt(cleanupHour, cleanupMinute), func() (river.JobArgs, *river.InsertOpts) {
		return CleanupArgs{}, nil
	}, nil)
}

// Kind returns member_cleanup.
func (CleanupArgs) Kind() string {
	return cleanupKind
}

// InsertOpts places member_cleanup jobs on the maintenance queue.
func (CleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance}
}

// Work deletes the sessions that have expired and the member links that
// expired or were consumed more than 7 days ago, selecting each batch of up
// to 1,000 rows by primary key until a batch comes back short. It logs
// members.cleanup_completed with deleted_sessions and deleted_links.
func (worker *CleanupWorker) Work(ctx context.Context, _ *river.Job[CleanupArgs]) error {
	now := worker.clock.Now()
	deletedSessions, err := deleteInBatches(ctx, func(ctx context.Context) (int64, error) {
		return worker.queries.DeleteExpiredSessions(ctx, queries.DeleteExpiredSessionsParams{Now: now, BatchSize: cleanupBatchSize})
	})
	if err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	deletedLinks, err := deleteInBatches(ctx, func(ctx context.Context) (int64, error) {
		return worker.queries.DeleteOldMemberLinks(ctx, queries.DeleteOldMemberLinksParams{Cutoff: now.Add(-cleanupLinkRetention), BatchSize: cleanupBatchSize})
	})
	if err != nil {
		return fmt.Errorf("delete old member links: %w", err)
	}
	worker.logger.Info(ctx, logging.MembersCleanupCompleted, slog.Int64("deleted_sessions", deletedSessions), slog.Int64("deleted_links", deletedLinks))
	return nil
}

func deleteInBatches(ctx context.Context, deleteBatch func(ctx context.Context) (int64, error)) (int64, error) {
	var deletedTotal int64
	for {
		deleted, err := deleteBatch(ctx)
		if err != nil {
			return deletedTotal, err
		}
		deletedTotal += deleted
		if deleted < cleanupBatchSize {
			return deletedTotal, nil
		}
	}
}
