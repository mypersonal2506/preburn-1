package jobs_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime/pprof"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/jobs/jobstest"
	"github.com/preburn/preburn/internal/logging"
)

const (
	completedMetricName  = "preburn_jobs_completed_total"
	waitTimeout          = 10 * time.Second
	pollInterval         = 20 * time.Millisecond
	observerRunFrame     = "internal/jobs.(*observer).run("
	goroutineStacksDebug = 2
)

type exampleArgs struct {
	Customer string `json:"customer"`
}

type exampleWorker struct {
	river.WorkerDefaults[exampleArgs]
}

type failingArgs struct {
	Panic bool `json:"panic"`
}

type failingWorker struct {
	river.WorkerDefaults[failingArgs]
}

type failedJobRecord struct {
	Message string `json:"msg"`
	JobID   int64  `json:"job_id"`
	Kind    string `json:"kind"`
	Attempt int    `json:"attempt"`
	Error   string `json:"error"`
	Stack   string `json:"stack"`
}

type failedJobLog struct {
	Kind     string
	Attempt  int
	Error    string
	HasJobID bool
	HasStack bool
}

type lockedBuffer struct {
	mutex    sync.Mutex
	contents bytes.Buffer
}

func (exampleArgs) Kind() string { return "example" }

func (worker *exampleWorker) Work(context.Context, *river.Job[exampleArgs]) error {
	return nil
}

func (failingArgs) Kind() string { return "failing" }

func (worker *failingWorker) Work(_ context.Context, job *river.Job[failingArgs]) error {
	if job.Args.Panic {
		panic("example panic")
	}
	return errors.New("example failure")
}

func TestInsertClientInsertsJob(t *testing.T) {
	pool := databasetest.NewPool(t)
	client, err := jobs.NewInsertClient(pool, logging.New(t.Output(), slog.LevelDebug))
	if err != nil {
		t.Fatalf("NewInsertClient: %v", err)
	}

	if _, err := client.Insert(t.Context(), exampleArgs{Customer: "cus_1"}, &river.InsertOpts{Queue: jobs.QueueLedger}); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	job := jobstest.RequireInserted(t, pool, exampleArgs{}, &rivertest.RequireInsertedOpts{
		Queue: jobs.QueueLedger,
		State: rivertype.JobStateAvailable,
	})
	if job.Args.Customer != "cus_1" {
		t.Errorf("inserted job customer = %q, want cus_1", job.Args.Customer)
	}
}

func TestWorkerClientRunsInsertedJobToCompletion(t *testing.T) {
	pool := databasetest.NewPool(t)
	registry := prometheus.NewRegistry()
	workers := river.NewWorkers()
	river.AddWorker(workers, &exampleWorker{})
	client := jobstest.StartWorkerClient(t, pool, workers, registry)
	completed, cancelSubscription := client.Subscribe(river.EventKindJobCompleted)
	defer cancelSubscription()

	if _, err := jobstest.NewInsertClient(t, pool).Insert(t.Context(), exampleArgs{Customer: "cus_1"}, nil); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	select {
	case event := <-completed:
		if event.Job.Kind != "example" || event.Job.State != rivertype.JobStateCompleted {
			t.Errorf("completed event job kind %s state %s, want example completed", event.Job.Kind, event.Job.State)
		}
	case <-time.After(waitTimeout):
		t.Fatalf("job not completed within %s", waitTimeout)
	}
	want := map[string]float64{"kind=example state=completed": 1}
	waitFor(t, "completed metric", func() bool {
		return cmp.Equal(want, gatherSeries(t, registry, completedMetricName))
	})
}

func TestWorkerClientLogsAndCountsFailedJobs(t *testing.T) {
	pool := databasetest.NewPool(t)
	registry := prometheus.NewRegistry()
	workers := river.NewWorkers()
	river.AddWorker(workers, &failingWorker{})
	logs := &lockedBuffer{}
	client, err := jobs.NewWorkerClient(pool, workers, nil, logging.New(logs, slog.LevelDebug), registry)
	if err != nil {
		t.Fatalf("NewWorkerClient: %v", err)
	}
	failed, cancelSubscription := client.Subscribe(river.EventKindJobFailed)
	defer cancelSubscription()
	if err := client.Start(t.Context()); err != nil {
		t.Fatalf("start worker client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Stop(context.Background()); err != nil {
			t.Errorf("stop worker client: %v", err)
		}
	})

	for _, arguments := range []failingArgs{{Panic: false}, {Panic: true}} {
		if _, err := client.Insert(t.Context(), arguments, &river.InsertOpts{MaxAttempts: 1}); err != nil {
			t.Fatalf("insert job: %v", err)
		}
	}

	deadline := time.After(waitTimeout)
	for range 2 {
		select {
		case <-failed:
		case <-deadline:
			t.Fatalf("jobs not failed within %s", waitTimeout)
		}
	}
	wantLogs := []failedJobLog{
		{Kind: "failing", Attempt: 1, Error: "example failure", HasJobID: true},
		{Kind: "failing", Attempt: 1, Error: "example panic", HasJobID: true, HasStack: true},
	}
	if diff := cmp.Diff(wantLogs, logs.failedJobs(t)); diff != "" {
		t.Errorf("jobs.failed records mismatch (-want +got):\n%s", diff)
	}
	wantSeries := map[string]float64{"kind=failing state=discarded": 2}
	waitFor(t, "failed metric", func() bool {
		return cmp.Equal(wantSeries, gatherSeries(t, registry, completedMetricName))
	})
}

func TestWorkerClientObserverEndsWhenClientStops(t *testing.T) {
	pool := databasetest.NewPool(t)
	workers := river.NewWorkers()
	river.AddWorker(workers, &exampleWorker{})
	waitFor(t, "observers of earlier tests ended", func() bool { return observerGoroutines(t) == 0 })
	client, err := jobs.NewWorkerClient(pool, workers, nil, logging.New(t.Output(), slog.LevelDebug), prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("NewWorkerClient: %v", err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatalf("start worker client: %v", err)
	}
	if running := observerGoroutines(t); running != 1 {
		t.Fatalf("%d observer goroutines while the client runs, want 1", running)
	}

	if err := client.Stop(t.Context()); err != nil {
		t.Fatalf("stop worker client: %v", err)
	}

	waitFor(t, "observer ended after stop", func() bool { return observerGoroutines(t) == 0 })
}

func (buffer *lockedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.contents.Write(data)
}

func (buffer *lockedBuffer) failedJobs(t *testing.T) []failedJobLog {
	t.Helper()
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	var failedJobs []failedJobLog
	scanner := bufio.NewScanner(bytes.NewReader(buffer.contents.Bytes()))
	for scanner.Scan() {
		var record failedJobRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("parse log line %q: %v", scanner.Text(), err)
		}
		if record.Message != string(logging.JobsFailed) {
			continue
		}
		failedJobs = append(failedJobs, failedJobLog{
			Kind:     record.Kind,
			Attempt:  record.Attempt,
			Error:    record.Error,
			HasJobID: record.JobID > 0,
			HasStack: record.Stack != "",
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan logs: %v", err)
	}
	slices.SortFunc(failedJobs, func(first, second failedJobLog) int {
		return strings.Compare(first.Error, second.Error)
	})
	return failedJobs
}

func waitFor(t *testing.T, description string, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	deadline := time.After(waitTimeout)
	for !condition() {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("%s: not reached within %s", description, waitTimeout)
		}
	}
}

func observerGoroutines(t *testing.T) int {
	t.Helper()
	var stacks strings.Builder
	if err := pprof.Lookup("goroutine").WriteTo(&stacks, goroutineStacksDebug); err != nil {
		t.Fatalf("write goroutine stacks: %v", err)
	}
	return strings.Count(stacks.String(), observerRunFrame)
}

func gatherSeries(t *testing.T, registry prometheus.Gatherer, familyName string) map[string]float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	series := map[string]float64{}
	for _, family := range families {
		if family.GetName() != familyName {
			continue
		}
		for _, metric := range family.GetMetric() {
			var labels []string
			for _, label := range metric.GetLabel() {
				labels = append(labels, label.GetName()+"="+label.GetValue())
			}
			series[strings.Join(labels, " ")] = metric.GetCounter().GetValue()
		}
	}
	return series
}
