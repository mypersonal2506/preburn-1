package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/logging"
)

const (
	workerStopTimeout   = 30 * time.Second
	workerCancelTimeout = 15 * time.Second
)

// Work runs the workers and periodic jobs of RegisterJobs on a River worker
// client until ctx ends. It first makes the counters ready with
// CounterBootstrap, which rebuilds them when the counters_ready marker is
// missing or waits for the process that does, and returns nil when ctx ends
// during that wait. It logs worker.started once the client runs. When ctx
// ends it stops fetching jobs and waits up to 30 seconds for running jobs,
// then cancels the jobs still running and waits up to 15 more seconds, and
// logs worker.stopped. Work registers the job metrics on Registry, so it runs
// once per App.
func (application *App) Work(ctx context.Context) error {
	workers, periodicJobs, err := RegisterJobs(application)
	if err != nil {
		return err
	}
	if err := application.CounterBootstrap.EnsureCountersReady(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("ensure counters ready: %w", err)
	}
	client, err := jobs.NewWorkerClient(application.Pool, workers, periodicJobs, application.Logger, application.Registry) //nolint:contextcheck // its observer goroutine outlives ctx and ends when River stops the client.
	if err != nil {
		return err
	}
	if err := client.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("start river worker client: %w", err)
	}
	application.Logger.Info(ctx, logging.WorkerStarted)
	<-ctx.Done()
	stopContext, cancelStop := context.WithTimeout(context.WithoutCancel(ctx), workerStopTimeout)
	defer cancelStop()
	if err := client.Stop(stopContext); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("stop river worker client: %w", err)
		}
		cancelContext, cancelJobs := context.WithTimeout(context.WithoutCancel(ctx), workerCancelTimeout)
		defer cancelJobs()
		if err := client.StopAndCancel(cancelContext); err != nil {
			return fmt.Errorf("cancel running river jobs: %w", err)
		}
	}
	application.Logger.Info(ctx, logging.WorkerStopped)
	return nil
}
