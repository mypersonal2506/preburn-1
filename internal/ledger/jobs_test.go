package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/jobs/jobstest"
	"github.com/preburn/preburn/internal/ledger"
)

func TestRollupRefreshArgsDeclareKindQueueAndUniqueness(t *testing.T) {
	t.Parallel()
	arguments := ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, identifiers.New(), day(time.August, 1))

	if kind := arguments.Kind(); kind != "rollup_refresh" {
		t.Errorf("kind = %s, want rollup_refresh", kind)
	}
	options := arguments.InsertOpts()
	if options.Queue != jobs.QueueLedger {
		t.Errorf("queue = %s, want %s", options.Queue, jobs.QueueLedger)
	}
	wantUnique := river.UniqueOpts{
		ByArgs: true,
		ByState: []rivertype.JobState{
			rivertype.JobStateAvailable,
			rivertype.JobStatePending,
			rivertype.JobStateRetryable,
			rivertype.JobStateRunning,
			rivertype.JobStateScheduled,
		},
	}
	sortStates := cmpopts.SortSlices(func(left, right rivertype.JobState) bool { return left < right })
	if diff := cmp.Diff(wantUnique, options.UniqueOpts, sortStates); diff != "" {
		t.Errorf("unique options mismatch (-want +got):\n%s", diff)
	}
}

func TestUsageEstimatesRefreshArgsDeclareKindAndQueue(t *testing.T) {
	t.Parallel()
	arguments := ledger.UsageEstimatesRefreshArgs{}

	if kind := arguments.Kind(); kind != "usage_estimates_refresh" {
		t.Errorf("kind = %s, want usage_estimates_refresh", kind)
	}
	if queue := arguments.InsertOpts().Queue; queue != jobs.QueueLedger {
		t.Errorf("queue = %s, want %s", queue, jobs.QueueLedger)
	}
}

func TestRollupRefreshInsertCollapsesPendingRefreshesOfOneCustomerPeriod(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	client := jobstest.NewInsertClient(t, pool)
	customerID := identifiers.New()
	arguments := ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, customerID, day(time.August, 1))
	first := insertJob(t, client, arguments)

	sameInstantElsewhere := ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, customerID, day(time.August, 1).In(time.FixedZone("UTC+2", 2*60*60)))
	if second := insertJob(t, client, sameInstantElsewhere); !second.UniqueSkippedAsDuplicate || second.Job.ID != first.Job.ID {
		t.Errorf("second insert duplicate = %t job %d, want duplicate of job %d", second.UniqueSkippedAsDuplicate, second.Job.ID, first.Job.ID)
	}
	for _, other := range []ledger.RollupRefreshArgs{
		ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, customerID, day(time.August, 2)),
		ledger.NewRollupRefreshArgs(httpapi.EnvironmentLive, customerID, day(time.August, 1)),
		ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, identifiers.New(), day(time.August, 1)),
	} {
		if inserted := insertJob(t, client, other); inserted.UniqueSkippedAsDuplicate {
			t.Errorf("insert of %+v was a duplicate, want a new job", other)
		}
	}
	for _, state := range []rivertype.JobState{rivertype.JobStateRunning, rivertype.JobStateRetryable} {
		setJobState(t, pool, first.Job.ID, state)
		if inserted := insertJob(t, client, arguments); !inserted.UniqueSkippedAsDuplicate {
			t.Errorf("insert while the first job is %s was not a duplicate", state)
		}
	}
	setJobState(t, pool, first.Job.ID, rivertype.JobStateCompleted)
	if inserted := insertJob(t, client, arguments); inserted.UniqueSkippedAsDuplicate {
		t.Error("insert after the first job completed was a duplicate, want a new job")
	}
}

func TestRollupRefreshWorkerRefreshesRollups(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	insertSubscription(t, pool, customerID, dollars(30), day(time.August, 1), day(time.September, 1))
	insertLedgerEntry(t, pool, costedEntry(customerID, day(time.August, 1), day(time.September, 1), dollars(4)))

	finished := workWithWorkerClient(t, pool, ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, customerID, day(time.August, 1)))

	if finished.State != rivertype.JobStateCompleted {
		t.Errorf("job state = %s, want completed", finished.State)
	}
	want := []rollup{{PeriodStart: day(time.August, 1), PeriodEnd: day(time.September, 1), RevenueNet: dollars(30), Cost: dollars(4)}}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
}

func TestRollupRefreshWorkerCancelsJobOfUnknownCustomer(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)

	finished := workWithWorkerClient(t, pool, ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, identifiers.New(), day(time.August, 1)))

	if finished.State != rivertype.JobStateCancelled {
		t.Errorf("job state = %s, want cancelled", finished.State)
	}
}

func TestRollupRefreshWorkerWaitsForReportDeduplicatedOntoRunningJob(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	pool := databasetest.NewPool(t)
	client := jobstest.NewInsertClient(t, pool)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	periodStart, periodEnd := day(time.August, 1), day(time.September, 1)
	insertLedgerEntry(t, pool, costedEntry(customerID, periodStart, periodEnd, dollars(1)))
	arguments := ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, customerID, periodStart)
	job := insertRunningJob(t, pool, client, arguments)
	report := beginTransaction(ctx, t, pool)
	insertLedgerEntry(t, report, costedEntry(customerID, periodStart, periodEnd, dollars(2)))
	if inserted, err := client.InsertTx(ctx, report, arguments, nil); err != nil || !inserted.UniqueSkippedAsDuplicate {
		t.Fatalf("report insert duplicate = %t err %v, want a duplicate of the running job", inserted != nil && inserted.UniqueSkippedAsDuplicate, err)
	}
	worked := make(chan error, 1)
	go func() {
		worked <- ledger.NewRollupRefreshWorker(pool).Work(rivertest.WorkContext(ctx, client), job)
	}()

	waitForLockWaiters(ctx, t, pool, 1)
	if err := report.Commit(ctx); err != nil {
		t.Fatalf("commit report: %v", err)
	}

	if err := receive(ctx, t, worked); err != nil {
		t.Fatalf("Work: %v", err)
	}
	want := []rollup{{PeriodStart: periodStart, PeriodEnd: periodEnd, Cost: dollars(3)}}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
	if state := jobState(t, pool, job.ID); state != rivertype.JobStateCompleted {
		t.Errorf("job state = %s, want completed", state)
	}
}

func TestRollupRefreshWorkerLetsLaterReportInsertNewJob(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	pool := databasetest.NewPool(t)
	client := jobstest.NewInsertClient(t, pool)
	customerID := insertCustomer(t, pool, httpapi.EnvironmentTest)
	periodStart, periodEnd := day(time.August, 1), day(time.September, 1)
	insertLedgerEntry(t, pool, costedEntry(customerID, periodStart, periodEnd, dollars(1)))
	arguments := ledger.NewRollupRefreshArgs(httpapi.EnvironmentTest, customerID, periodStart)
	job := insertRunningJob(t, pool, client, arguments)
	customerHolder := beginTransaction(ctx, t, pool)
	if _, err := customerHolder.Exec(ctx, "SELECT FROM customers WHERE customer_id = $1 FOR NO KEY UPDATE", customerID); err != nil {
		t.Fatalf("lock customer: %v", err)
	}
	report := beginTransaction(ctx, t, pool)
	insertLedgerEntry(t, report, costedEntry(customerID, periodStart, periodEnd, dollars(2)))
	worked := make(chan error, 1)
	go func() {
		worked <- ledger.NewRollupRefreshWorker(pool).Work(rivertest.WorkContext(ctx, client), job)
	}()
	waitForLockWaiters(ctx, t, pool, 1)
	reportInserted := make(chan *rivertype.JobInsertResult, 1)
	go func() {
		inserted, err := client.InsertTx(ctx, report, arguments, nil)
		if err != nil {
			t.Errorf("report insert: %v", err)
		}
		reportInserted <- inserted
	}()
	waitForLockWaiters(ctx, t, pool, 2)

	if err := customerHolder.Rollback(ctx); err != nil {
		t.Fatalf("release customer: %v", err)
	}

	if err := receive(ctx, t, worked); err != nil {
		t.Fatalf("Work: %v", err)
	}
	inserted := receive(ctx, t, reportInserted)
	if inserted == nil || inserted.UniqueSkippedAsDuplicate || inserted.Job.ID == job.ID {
		t.Fatalf("report insert = %+v, want a new job", inserted)
	}
	if err := report.Commit(ctx); err != nil {
		t.Fatalf("commit report: %v", err)
	}
	want := []rollup{{PeriodStart: periodStart, PeriodEnd: periodEnd, Cost: dollars(1)}}
	if diff := cmp.Diff(want, listRollups(t, pool, customerID)); diff != "" {
		t.Errorf("rollups mismatch (-want +got):\n%s", diff)
	}
	if state := jobState(t, pool, inserted.Job.ID); state != rivertype.JobStateAvailable {
		t.Errorf("new job state = %s, want available", state)
	}
}

func insertJob(t *testing.T, client *river.Client[pgx.Tx], arguments ledger.RollupRefreshArgs) *rivertype.JobInsertResult {
	t.Helper()
	inserted, err := client.Insert(t.Context(), arguments, nil)
	if err != nil {
		t.Fatalf("insert rollup refresh job: %v", err)
	}
	return inserted
}

func insertRunningJob(t *testing.T, pool *pgxpool.Pool, client *river.Client[pgx.Tx], arguments ledger.RollupRefreshArgs) *river.Job[ledger.RollupRefreshArgs] {
	t.Helper()
	inserted := insertJob(t, client, arguments)
	setJobState(t, pool, inserted.Job.ID, rivertype.JobStateRunning)
	row := *inserted.Job
	row.State = rivertype.JobStateRunning
	row.Attempt = 1
	return &river.Job[ledger.RollupRefreshArgs]{JobRow: &row, Args: arguments}
}

func setJobState(t *testing.T, pool *pgxpool.Pool, jobID int64, state rivertype.JobState) {
	t.Helper()
	_, err := pool.Exec(t.Context(),
		`UPDATE river_job
		SET state = $2::text::river_job_state,
			attempt = 1,
			attempted_at = now(),
			finalized_at = CASE WHEN $2::text IN ('cancelled', 'completed', 'discarded') THEN now() END
		WHERE id = $1`,
		jobID, string(state),
	)
	if err != nil {
		t.Fatalf("set job %d state %s: %v", jobID, state, err)
	}
}

func jobState(t *testing.T, pool *pgxpool.Pool, jobID int64) rivertype.JobState {
	t.Helper()
	var state string
	if err := pool.QueryRow(t.Context(), "SELECT state FROM river_job WHERE id = $1", jobID).Scan(&state); err != nil {
		t.Fatalf("select job %d state: %v", jobID, err)
	}
	return rivertype.JobState(state)
}

func workWithWorkerClient(t *testing.T, pool *pgxpool.Pool, arguments ledger.RollupRefreshArgs) *rivertype.JobRow {
	t.Helper()
	workers := river.NewWorkers()
	river.AddWorker(workers, ledger.NewRollupRefreshWorker(pool))
	client := jobstest.StartWorkerClient(t, pool, workers, prometheus.NewRegistry())
	finished, cancelSubscription := client.Subscribe(river.EventKindJobCompleted, river.EventKindJobCancelled, river.EventKindJobFailed)
	defer cancelSubscription()

	if _, err := client.Insert(t.Context(), arguments, nil); err != nil {
		t.Fatalf("insert rollup refresh job: %v", err)
	}

	select {
	case event := <-finished:
		return event.Job
	case <-time.After(waitTimeout):
		t.Fatalf("rollup refresh job not finished within %s", waitTimeout)
		return nil
	}
}

func beginTransaction(ctx context.Context, t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	t.Cleanup(func() {
		if err := transaction.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("roll back: %v", err)
		}
	})
	return transaction
}

func receive[Value any](ctx context.Context, t *testing.T, channel <-chan Value) Value {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-ctx.Done():
		t.Fatalf("no result within %s", waitTimeout)
		var zero Value
		return zero
	}
}
