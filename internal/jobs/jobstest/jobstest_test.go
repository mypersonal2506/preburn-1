package jobstest_test

import (
	"context"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/jobs/jobstest"
)

type recordingArgs struct {
	Customer string `json:"customer"`
}

type recordingWorker struct {
	river.WorkerDefaults[recordingArgs]
	customers []string
}

func (recordingArgs) Kind() string { return "recording" }

func (worker *recordingWorker) Work(_ context.Context, job *river.Job[recordingArgs]) error {
	worker.customers = append(worker.customers, job.Args.Customer)
	return nil
}

func TestNewWorkerWorksJobInTransaction(t *testing.T) {
	pool := databasetest.NewPool(t)
	transaction, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() {
		if err := transaction.Rollback(context.Background()); err != nil {
			t.Errorf("roll back: %v", err)
		}
	}()
	worker := &recordingWorker{}

	result, err := jobstest.NewWorker(t, worker).Work(t.Context(), t, transaction, recordingArgs{Customer: "cus_1"}, nil)
	if err != nil {
		t.Fatalf("Work: %v", err)
	}

	if result.EventKind != river.EventKindJobCompleted || result.Job.State != rivertype.JobStateCompleted {
		t.Errorf("work result %s with job state %s, want job_completed and completed", result.EventKind, result.Job.State)
	}
	if len(worker.customers) != 1 || worker.customers[0] != "cus_1" {
		t.Errorf("worker saw customers %v, want [cus_1]", worker.customers)
	}
}

func TestRequireInsertedFindsJobInsertedByInsertClient(t *testing.T) {
	pool := databasetest.NewPool(t)

	if _, err := jobstest.NewInsertClient(t, pool).Insert(t.Context(), recordingArgs{Customer: "cus_2"}, nil); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	if job := jobstest.RequireInserted(t, pool, recordingArgs{}, nil); job.Args.Customer != "cus_2" {
		t.Errorf("found job customer %q, want cus_2", job.Args.Customer)
	}
}
