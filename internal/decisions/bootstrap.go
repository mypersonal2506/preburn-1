package decisions

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/decisions/queries"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/logging"
)

const (
	rebuildLockName             = "counters_rebuild"
	countersReadinessCheckName  = "counters"
	rebuildDurationBucketStart  = 0.1
	rebuildDurationBucketFactor = 2
	rebuildDurationBucketCount  = 12
)

// CounterBootstrap rebuilds the Redis counters and reservations from Postgres
// when the counters_ready marker is missing, as after a Redis restart without
// persistence. Create one with NewCounterBootstrap. It is safe for concurrent
// use.
type CounterBootstrap struct {
	pool      *pgxpool.Pool
	queries   *queries.Queries
	jobs      *river.Client[pgx.Tx]
	counters  *Counters
	clock     clock.Clock
	logger    *logging.Logger
	durations prometheus.Histogram
	requests  chan struct{}
}

// NewCounterBootstrap returns a CounterBootstrap that rebuilds counters from
// the decisions and ledger entries in pool, inserts rollup refresh jobs with
// insertClient, reads time from timeSource, logs failed requested rebuilds to
// logger and registers preburn_counters_rebuild_duration_seconds on registry.
// It returns an error when that metric is already registered there.
func NewCounterBootstrap(pool *pgxpool.Pool, counters *Counters, insertClient *river.Client[pgx.Tx], timeSource clock.Clock, logger *logging.Logger, registry prometheus.Registerer) (*CounterBootstrap, error) {
	durations := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "preburn_counters_rebuild_duration_seconds",
		Help:    "Duration in seconds of rebuilding the Redis counters from Postgres.",
		Buckets: prometheus.ExponentialBuckets(rebuildDurationBucketStart, rebuildDurationBucketFactor, rebuildDurationBucketCount),
	})
	if err := registry.Register(durations); err != nil {
		return nil, fmt.Errorf("register counter bootstrap metrics: %w", err)
	}
	return &CounterBootstrap{
		pool:      pool,
		queries:   queries.New(pool),
		jobs:      insertClient,
		counters:  counters,
		clock:     timeSource,
		logger:    logger,
		durations: durations,
		requests:  make(chan struct{}, 1),
	}, nil
}

// CountersReadinessCheck returns the readiness check named counters. It fails
// with ErrCountersNotReady until the counters_ready marker exists.
func CountersReadinessCheck(counters *Counters) httpapi.ReadinessCheck {
	return httpapi.ReadinessCheck{
		Name: countersReadinessCheckName,
		Check: func(ctx context.Context) error {
			ready, err := counters.IsReady(ctx)
			if err != nil {
				return err
			}
			if !ready {
				return ErrCountersNotReady
			}
			return nil
		},
	}
}

// EnsureCountersReady returns nil once the counters_ready marker exists. When
// it is missing, EnsureCountersReady takes the counters_rebuild advisory
// lock and checks again, so one caller across every process rebuilds while
// the others wait on the lock and then find the marker. The rebuild writes
// every live counter from Postgres, like counters_reconcile computes it, and
// drops the counter's pending changes. A live counter belongs to a customer
// period that contains the current time or holds a reservation that has not
// expired. The rebuild then marks reserved decisions whose expiry has passed
// as expired, with the rollup refresh job of each of their customer periods,
// because no reservation is left to expire them, recreates the reservation
// hash, open reservation and reservations_expiring entry of every other
// reserved decision, sets the marker to the rebuild time and records the
// duration in preburn_counters_rebuild_duration_seconds. Every counter script
// refuses to run while the marker is missing, so nothing lands in a counter
// during the rebuild. A caller whose rebuild fails releases the lock without
// the marker, and the next caller rebuilds.
func (bootstrap *CounterBootstrap) EnsureCountersReady(ctx context.Context) error {
	ready, err := bootstrap.counters.IsReady(ctx)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	release, err := database.AcquireAdvisoryLock(ctx, bootstrap.pool, rebuildLockName)
	if err != nil {
		return err
	}
	defer release()
	ready, err = bootstrap.counters.IsReady(ctx)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	started := time.Now()
	if err := bootstrap.rebuild(ctx); err != nil {
		return err
	}
	bootstrap.durations.Observe(time.Since(started).Seconds())
	return nil
}

// RequestRebuild asks ServeRebuildRequests to run EnsureCountersReady and
// returns at once. Requests made while one waits or runs are merged into it,
// so a process runs one rebuild at a time.
func (bootstrap *CounterBootstrap) RequestRebuild() {
	select {
	case bootstrap.requests <- struct{}{}:
	default:
	}
}

// ServeRebuildRequests runs EnsureCountersReady for each RequestRebuild, one
// at a time, until ctx ends. A failed run logs
// decisions.counters_rebuild_failed and the next request tries again.
func (bootstrap *CounterBootstrap) ServeRebuildRequests(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-bootstrap.requests:
		}
		if err := bootstrap.EnsureCountersReady(ctx); err != nil && ctx.Err() == nil {
			bootstrap.logger.Error(ctx, logging.DecisionsCountersRebuildFailed, slog.String("error", err.Error()))
		}
	}
}

func (bootstrap *CounterBootstrap) rebuild(ctx context.Context) error {
	now := bootstrap.clock.Now()
	for _, environment := range []httpapi.Environment{httpapi.EnvironmentTest, httpapi.EnvironmentLive} {
		if err := bootstrap.rebuildCounters(ctx, environment, now); err != nil {
			return err
		}
		if err := bootstrap.restoreReservations(ctx, environment, now); err != nil {
			return err
		}
	}
	return bootstrap.counters.SetReady(ctx, bootstrap.clock.Now())
}

func (bootstrap *CounterBootstrap) rebuildCounters(ctx context.Context, environment httpapi.Environment, now time.Time) error {
	liveCounters, err := listLiveCounters(ctx, bootstrap.queries, bootstrap.counters, environment, now)
	if err != nil {
		return err
	}
	for batch := range slices.Chunk(liveCounters, counterBatchSize) {
		expected, err := expectedCounterFields(ctx, bootstrap.queries, bootstrap.counters, environment, batch, reservedAfter(now))
		if err != nil {
			return err
		}
		for _, counter := range batch {
			if err := bootstrap.counters.Overwrite(ctx, counter.key, expected[counter.key], counter.periodEnd); err != nil {
				return err
			}
		}
	}
	return nil
}

func (bootstrap *CounterBootstrap) restoreReservations(ctx context.Context, environment httpapi.Environment, now time.Time) error {
	err := database.InTransaction(ctx, bootstrap.pool, func(ctx context.Context, transaction pgx.Tx) error {
		periods, err := bootstrap.queries.WithTx(transaction).MarkLapsedReservationsExpired(ctx, queries.MarkLapsedReservationsExpiredParams{
			Environment:   queries.Environment(environment),
			ReservedAfter: reservedAfter(now),
		})
		if err != nil {
			return fmt.Errorf("mark lapsed reservations expired: %w", err)
		}
		refreshes := make([]river.InsertManyParams, 0, len(periods))
		for _, period := range periods {
			refreshes = append(refreshes, river.InsertManyParams{
				Args: ledger.NewRollupRefreshArgs(environment, period.CustomerID, period.PeriodStart),
			})
		}
		return insertRollupRefreshes(ctx, bootstrap.jobs, transaction, refreshes)
	})
	if err != nil {
		return err
	}
	reservations, err := bootstrap.queries.ListLiveReservations(ctx, queries.ListLiveReservationsParams{
		Environment:   queries.Environment(environment),
		ReservedAfter: reservedAfter(now),
	})
	if err != nil {
		return fmt.Errorf("list live reservations: %w", err)
	}
	for _, reservation := range reservations {
		err := bootstrap.counters.RestoreReservation(ctx, ReserveRequest{
			Environment: environment,
			CustomerID:  reservation.CustomerID,
			PeriodStart: reservation.PeriodStart,
			PeriodEnd:   reservation.PeriodEnd,
			DecisionID:  reservation.DecisionID,
			Feature:     reservation.Feature,
			Amount:      reservation.ReservedNanos,
			ExpiresAt:   reservation.ExpiresAt,
		})
		if err != nil {
			return err
		}
	}
	return nil
}
