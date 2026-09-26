package jobstest

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"

	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/logging"
)

// NewInsertClient returns a client from jobs.NewInsertClient on pool. It fails
// the test when the client cannot be created.
func NewInsertClient(t testing.TB, pool *pgxpool.Pool) *river.Client[pgx.Tx] {
	t.Helper()
	client, err := jobs.NewInsertClient(pool, testLogger(t))
	if err != nil {
		t.Fatalf("create insert client: %v", err)
	}
	return client
}

// StartWorkerClient starts a client from jobs.NewWorkerClient on pool that
// works jobs with workers, runs no periodic jobs and registers its metrics on
// registry. A cleanup registered on t stops it. It fails the test when the
// client cannot be created or started.
func StartWorkerClient(t testing.TB, pool *pgxpool.Pool, workers *river.Workers, registry prometheus.Registerer) *river.Client[pgx.Tx] {
	t.Helper()
	client, err := jobs.NewWorkerClient(pool, workers, nil, testLogger(t), registry)
	if err != nil {
		t.Fatalf("create worker client: %v", err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatalf("start worker client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Stop(context.Background()); err != nil {
			t.Errorf("stop worker client: %v", err)
		}
	})
	return client
}

// RequireInserted fails the test unless pool's database holds exactly one job
// of expected's kind, matching options when options is not nil, and returns
// that job. It compares the kind, not the arguments. It wraps
// rivertest.RequireInserted.
func RequireInserted[TArgs river.JobArgs](t testing.TB, pool *pgxpool.Pool, expected TArgs, options *rivertest.RequireInsertedOpts) *river.Job[TArgs] {
	t.Helper()
	return rivertest.RequireInserted(t.Context(), t, riverpgxv5.New(pool), expected, options)
}

// NewWorker returns a rivertest.Worker for worker. Its Work method inserts a
// job and works it inside the transaction the test passes, so rolling that
// transaction back removes every trace of the job.
func NewWorker[TArgs river.JobArgs](t testing.TB, worker river.Worker[TArgs]) *rivertest.Worker[TArgs, pgx.Tx] {
	t.Helper()
	return rivertest.NewWorker(t, riverpgxv5.New(nil), &river.Config{Logger: testLogger(t).Library("river")}, worker)
}

func testLogger(t testing.TB) *logging.Logger {
	return logging.New(t.Output(), slog.LevelDebug)
}
