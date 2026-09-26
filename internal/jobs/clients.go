package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/logging"
)

const riverLibraryName = "river"

type errorHandler struct {
	logger *logging.Logger
}

// NewInsertClient returns a River client on pool that inserts jobs and works
// none, for the api process. River's warnings and errors go to logger as
// logging.library_message events.
func NewInsertClient(pool *pgxpool.Pool, logger *logging.Logger) (*river.Client[pgx.Tx], error) {
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: logger.Library(riverLibraryName)})
	if err != nil {
		return nil, fmt.Errorf("create river insert client: %w", err)
	}
	return client, nil
}

// NewWorkerClient returns a River client on pool, for the worker process. It
// works every queue with that queue's worker count using workers, and runs
// periodicJobs while it is the elected leader. Each failed attempt logs
// jobs.failed. It registers preburn_jobs_completed_total{kind,state} and
// preburn_river_queue_available{queue} on registry and returns an error when
// either is already registered there.
//
// Before returning it subscribes to the client's job events and starts a
// goroutine that counts finished attempts and samples the available jobs per
// queue every 15 seconds. River closes the subscription when the client
// stops, which ends the goroutine, so start the client once and always stop
// it.
func NewWorkerClient(pool *pgxpool.Pool, workers *river.Workers, periodicJobs []*river.PeriodicJob, logger *logging.Logger, registry prometheus.Registerer) (*river.Client[pgx.Tx], error) {
	observer, err := newObserver(pool, logger, registry)
	if err != nil {
		return nil, err
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		ErrorHandler: &errorHandler{logger: logger},
		Logger:       logger.Library(riverLibraryName),
		PeriodicJobs: periodicJobs,
		Queues:       queueConfigurations(),
		Workers:      workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create river worker client: %w", err)
	}
	events, _ := client.Subscribe(river.EventKindJobCompleted, river.EventKindJobFailed, river.EventKindJobCancelled)
	sampleTicker := time.NewTicker(queueSampleInterval)
	go func() {
		defer sampleTicker.Stop()
		observer.run(events, sampleTicker.C)
	}()
	return client, nil
}

// HandleError logs jobs.failed for an attempt that returned err and leaves the
// job on its retry schedule.
func (handler *errorHandler) HandleError(ctx context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
	handler.logger.Error(ctx, logging.JobsFailed,
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.Int("attempt", job.Attempt),
		slog.String("error", err.Error()),
	)
	return nil
}

// HandlePanic logs jobs.failed with the panic value as the error and the
// goroutine stack for an attempt that panicked, and leaves the job on its
// retry schedule.
func (handler *errorHandler) HandlePanic(ctx context.Context, job *rivertype.JobRow, panicValue any, trace string) *river.ErrorHandlerResult {
	handler.logger.Error(ctx, logging.JobsFailed,
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.Int("attempt", job.Attempt),
		slog.String("error", fmt.Sprint(panicValue)),
		slog.String("stack", trace),
	)
	return nil
}
