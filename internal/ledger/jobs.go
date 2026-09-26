package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/jobs"
)

const (
	rollupRefreshKind             = "rollup_refresh"
	usageEstimatesRefreshKind     = "usage_estimates_refresh"
	usageEstimatesRefreshInterval = time.Hour
)

// RollupRefreshArgs is the job of kind rollup_refresh, which runs
// RefreshRollups for one customer period after a usage report. Create one
// with NewRollupRefreshArgs and insert it with InsertTx in the transaction
// that writes the ledger entry. It runs on the ledger queue, and River
// inserts no second job with equal arguments while one is available, pending,
// scheduled, retryable or running.
type RollupRefreshArgs struct {
	// Environment is the environment of the customer.
	Environment httpapi.Environment `json:"environment"`
	// CustomerID is the customer whose rollups the job refreshes.
	CustomerID uuid.UUID `json:"customer_id"`
	// PeriodStart is the period start of the reported ledger entry, in UTC,
	// and the anchor the job passes to RefreshRollups.
	PeriodStart time.Time `json:"period_start"`
}

// RollupRefreshWorker works rollup_refresh jobs. Create one with
// NewRollupRefreshWorker.
type RollupRefreshWorker struct {
	river.WorkerDefaults[RollupRefreshArgs]
	pool *pgxpool.Pool
}

// UsageEstimatesRefreshArgs is the job of kind usage_estimates_refresh, which
// replaces the usage estimates of both environments. It has no arguments and
// runs on the ledger queue.
type UsageEstimatesRefreshArgs struct{}

// UsageEstimatesRefreshWorker works usage_estimates_refresh jobs. Create one
// with NewUsageEstimatesRefreshWorker.
type UsageEstimatesRefreshWorker struct {
	river.WorkerDefaults[UsageEstimatesRefreshArgs]
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewRollupRefreshArgs returns the rollup_refresh job of the customer period
// that starts at periodStart. It converts periodStart to UTC, because River
// compares encoded arguments and equal instants in other zones would encode
// differently.
func NewRollupRefreshArgs(environment httpapi.Environment, customerID uuid.UUID, periodStart time.Time) RollupRefreshArgs {
	return RollupRefreshArgs{Environment: environment, CustomerID: customerID, PeriodStart: periodStart.UTC()}
}

// NewRollupRefreshWorker returns a RollupRefreshWorker that refreshes rollups
// in pool.
func NewRollupRefreshWorker(pool *pgxpool.Pool) *RollupRefreshWorker {
	return &RollupRefreshWorker{pool: pool}
}

// NewUsageEstimatesRefreshWorker returns a UsageEstimatesRefreshWorker that
// writes estimates to pool and reads the current time from timeSource.
func NewUsageEstimatesRefreshWorker(pool *pgxpool.Pool, timeSource clock.Clock) *UsageEstimatesRefreshWorker {
	return &UsageEstimatesRefreshWorker{pool: pool, clock: timeSource}
}

// UsageEstimatesRefreshPeriodicJob returns the periodic job that inserts a
// usage_estimates_refresh job every hour.
func UsageEstimatesRefreshPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(jobs.Every(usageEstimatesRefreshInterval), func() (river.JobArgs, *river.InsertOpts) {
		return UsageEstimatesRefreshArgs{}, nil
	}, nil)
}

// Kind returns rollup_refresh.
func (RollupRefreshArgs) Kind() string {
	return rollupRefreshKind
}

// InsertOpts places rollup_refresh jobs on the ledger queue and makes them
// unique by arguments while available, pending, scheduled, retryable or
// running. River requires running in that set.
func (RollupRefreshArgs) InsertOpts() river.InsertOpts {
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

// Work marks the job completed and then runs RefreshRollups for the job's
// customer period, in one transaction. A report whose insert River skipped as
// a duplicate of this running job holds a lock on the job row until the
// report commits, so marking the job completed waits for that report and the
// refresh then reads its ledger entries. A report that inserts later waits
// for this transaction and then inserts a new job. A customer that does not
// exist in the job's environment cancels the job.
func (worker *RollupRefreshWorker) Work(ctx context.Context, job *river.Job[RollupRefreshArgs]) error {
	err := database.InTransaction(ctx, worker.pool, func(ctx context.Context, transaction pgx.Tx) error {
		// The job row lock taken here orders the refresh after reports deduplicated onto
		// this job. Completing the job after the refresh loses their ledger entries.
		if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, transaction, job); err != nil {
			return fmt.Errorf("complete rollup refresh job: %w", err)
		}
		return RefreshRollups(ctx, transaction, job.Args.Environment, job.Args.CustomerID, job.Args.PeriodStart)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return river.JobCancel(err)
	}
	return err
}

// Kind returns usage_estimates_refresh.
func (UsageEstimatesRefreshArgs) Kind() string {
	return usageEstimatesRefreshKind
}

// InsertOpts places usage_estimates_refresh jobs on the ledger queue.
func (UsageEstimatesRefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueLedger}
}

// Work replaces the usage estimates of the test and then the live
// environment, each in its own transaction. An estimate holds the 95th
// percentile quantity (percentile_cont, rounded up to a whole micro-unit) and
// the sample count of one feature, provider, model and meter over the ledger
// entries that occurred in the 30 days before now, correction entries
// excluded. Groups with fewer than 50 samples get no estimate, so an estimate
// whose group fell below 50 samples is removed.
func (worker *UsageEstimatesRefreshWorker) Work(ctx context.Context, _ *river.Job[UsageEstimatesRefreshArgs]) error {
	now := worker.clock.Now()
	for _, environment := range []httpapi.Environment{httpapi.EnvironmentTest, httpapi.EnvironmentLive} {
		if err := refreshUsageEstimates(ctx, worker.pool, environment, now); err != nil {
			return fmt.Errorf("refresh %s usage estimates: %w", environment, err)
		}
	}
	return nil
}
