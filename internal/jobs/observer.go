package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/logging"
)

const (
	queueSampleInterval = 15 * time.Second
	queueSampleTimeout  = 10 * time.Second
	availableJobsQuery  = "SELECT queue, count(*) FROM river_job WHERE state = 'available' AND queue = ANY($1) GROUP BY queue"
)

type observer struct {
	pool       *pgxpool.Pool
	logger     *logging.Logger
	queueNames []string
	completed  *prometheus.CounterVec
	available  *prometheus.GaugeVec
}

func newObserver(pool *pgxpool.Pool, logger *logging.Logger, registry prometheus.Registerer) (*observer, error) {
	observer := &observer{
		pool:       pool,
		logger:     logger,
		queueNames: slices.Sorted(maps.Keys(queueConfigurations())),
		completed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "preburn_jobs_completed_total",
			Help: "Finished job attempts by job kind and outcome: completed, retryable, discarded or cancelled.",
		}, []string{"kind", "state"}),
		available: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "preburn_river_queue_available",
			Help: "Jobs available to work by queue, sampled every 15 seconds.",
		}, []string{"queue"}),
	}
	for _, collector := range []prometheus.Collector{observer.completed, observer.available} {
		if err := registry.Register(collector); err != nil {
			return nil, fmt.Errorf("register job metrics: %w", err)
		}
	}
	return observer, nil
}

func (observer *observer) run(events <-chan *river.Event, sampleTicks <-chan time.Time) {
	for {
		select {
		case event, open := <-events:
			if !open {
				return
			}
			observer.completed.WithLabelValues(event.Job.Kind, attemptOutcome(event)).Inc()
		case <-sampleTicks:
			observer.sample()
		}
	}
}

func (observer *observer) sample() {
	ctx, cancel := context.WithTimeout(context.Background(), queueSampleTimeout)
	defer cancel()
	if err := observer.sampleQueues(ctx); err != nil {
		observer.logger.Error(ctx, logging.JobsQueueSampleFailed, slog.String("error", err.Error()))
	}
}

func (observer *observer) sampleQueues(ctx context.Context) error {
	rows, err := observer.pool.Query(ctx, availableJobsQuery, observer.queueNames)
	if err != nil {
		return fmt.Errorf("count available jobs: %w", err)
	}
	available := make(map[string]int64, len(observer.queueNames))
	var queue string
	var count int64
	if _, err := pgx.ForEachRow(rows, []any{&queue, &count}, func() error {
		available[queue] = count
		return nil
	}); err != nil {
		return fmt.Errorf("read available job counts: %w", err)
	}
	for _, name := range observer.queueNames {
		observer.available.WithLabelValues(name).Set(float64(available[name]))
	}
	return nil
}

func attemptOutcome(event *river.Event) string {
	if event.Kind == river.EventKindJobFailed && event.Job.State != rivertype.JobStateDiscarded {
		return string(rivertype.JobStateRetryable)
	}
	return string(event.Job.State)
}
