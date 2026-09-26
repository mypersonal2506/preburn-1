package decisions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/decisions/queries"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/ledger"
)

const (
	expiryKind      = "reservations_expire"
	expiryInterval  = 5 * time.Second
	expiryBatchSize = 500
)

// ExpiryArgs is the job of kind reservations_expire, which releases the
// reservations whose hold time has passed. It has no arguments and runs on
// the default queue.
type ExpiryArgs struct{}

// ExpiryWorker works reservations_expire jobs. Create one with
// NewExpiryWorker.
type ExpiryWorker struct {
	river.WorkerDefaults[ExpiryArgs]
	pool     *pgxpool.Pool
	queries  *queries.Queries
	jobs     *river.Client[pgx.Tx]
	counters *Counters
	clock    clock.Clock
	expired  prometheus.Counter
}

// NewExpiryWorker returns an ExpiryWorker that releases reservations in
// counters, marks their decisions in pool, inserts rollup refresh jobs with
// insertClient, reads time from timeSource and registers
// preburn_reservations_expired_total on registry. It returns an error when
// that metric is already registered there.
func NewExpiryWorker(pool *pgxpool.Pool, counters *Counters, insertClient *river.Client[pgx.Tx], timeSource clock.Clock, registry prometheus.Registerer) (*ExpiryWorker, error) {
	expired := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "preburn_reservations_expired_total",
		Help: "Reservations released because their hold time passed before a report or release.",
	})
	if err := registry.Register(expired); err != nil {
		return nil, fmt.Errorf("register expiry metrics: %w", err)
	}
	return &ExpiryWorker{pool: pool, queries: queries.New(pool), jobs: insertClient, counters: counters, clock: timeSource, expired: expired}, nil
}

// ExpiryPeriodicJob returns the periodic job that inserts a
// reservations_expire job every 5 seconds.
func ExpiryPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(jobs.Every(expiryInterval), func() (river.JobArgs, *river.InsertOpts) {
		return ExpiryArgs{}, nil
	}, nil)
}

// Kind returns reservations_expire.
func (ExpiryArgs) Kind() string {
	return expiryKind
}

// InsertOpts places reservations_expire jobs on the default queue.
func (ExpiryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueDefault}
}

// Work expires every reservation in reservations_expiring whose expiry is at
// or before the current time, in batches of 500 until a batch comes back
// short. For each batch it first marks the decisions that are still reserved
// as expired and, in the same transaction, inserts the rollup refresh job of
// each customer period they belong to, since a decision that expires was
// never reported. It then releases each reservation with status expired and
// adds one to preburn_reservations_expired_total per release that applied. A
// member whose reservation hash is missing or no longer reserved leaves the
// set without a release, and a reservation without a decision row, left by a
// check whose reply was lost, is released like any other. A failure returns
// an error, ErrCountersNotReady while the counters_ready marker is missing,
// and the retry finds the members of the failed batch again.
func (worker *ExpiryWorker) Work(ctx context.Context, _ *river.Job[ExpiryArgs]) error {
	return worker.expireDue(ctx, worker.clock.Now())
}

func (worker *ExpiryWorker) expireDue(ctx context.Context, now time.Time) error {
	for {
		decisionIDs, err := worker.counters.ExpiredReservations(ctx, now, expiryBatchSize)
		if err != nil {
			return err
		}
		if len(decisionIDs) > 0 {
			if err := worker.expireBatch(ctx, decisionIDs); err != nil {
				return err
			}
		}
		if len(decisionIDs) < expiryBatchSize {
			return nil
		}
	}
}

func (worker *ExpiryWorker) expireBatch(ctx context.Context, decisionIDs []uuid.UUID) error {
	err := database.InTransaction(ctx, worker.pool, func(ctx context.Context, transaction pgx.Tx) error {
		periods, err := worker.queries.WithTx(transaction).MarkDecisionsExpired(ctx, decisionIDs)
		if err != nil {
			return fmt.Errorf("mark decisions expired: %w", err)
		}
		refreshes := make([]river.InsertManyParams, 0, len(periods))
		for _, period := range periods {
			refreshes = append(refreshes, river.InsertManyParams{
				Args: ledger.NewRollupRefreshArgs(httpapi.Environment(period.Environment), period.CustomerID, period.PeriodStart),
			})
		}
		return insertRollupRefreshes(ctx, worker.jobs, transaction, refreshes)
	})
	if err != nil {
		return err
	}
	var finishedMembers []any
	for _, decisionID := range decisionIDs {
		result, err := worker.counters.Release(ctx, decisionID, ReservationStatusExpired)
		if err != nil {
			return fmt.Errorf("expire reservation %s: %w", decisionID, err)
		}
		if result.Applied {
			worker.expired.Inc()
		} else {
			finishedMembers = append(finishedMembers, decisionID.String())
		}
	}
	if len(finishedMembers) == 0 {
		return nil
	}
	// The release script leaves a reservation that is no longer reserved in
	// the set, so without this removal every later run would read it again.
	if err := worker.counters.cache.Redis().ZRem(ctx, worker.counters.cache.Key(reservationsExpiringKeyName), finishedMembers...).Err(); err != nil {
		return fmt.Errorf("remove finished reservations from %s: %w", reservationsExpiringKeyName, err)
	}
	return nil
}

func insertRollupRefreshes(ctx context.Context, insertClient *river.Client[pgx.Tx], transaction pgx.Tx, refreshes []river.InsertManyParams) error {
	if len(refreshes) == 0 {
		return nil
	}
	if _, err := insertClient.InsertManyTx(ctx, transaction, refreshes); err != nil {
		return fmt.Errorf("insert rollup refreshes: %w", err)
	}
	return nil
}
