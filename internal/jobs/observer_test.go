package jobs

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/logging"
)

const observerWaitTimeout = 10 * time.Second

type sampledArgs struct{}

type sampleFailedRecord struct {
	Message string `json:"msg"`
	Error   string `json:"error"`
}

func (sampledArgs) Kind() string { return "sampled" }

func TestObserverSamplesQueuesCountsAttemptsAndStopsWhenEventsClose(t *testing.T) {
	pool := databasetest.NewPool(t)
	logger := logging.New(t.Output(), slog.LevelDebug)
	client, err := NewInsertClient(pool, logger)
	if err != nil {
		t.Fatalf("NewInsertClient: %v", err)
	}
	inserts := []river.InsertOpts{
		{Queue: QueueLedger},
		{Queue: QueueLedger},
		{Queue: QueueWebhooks},
		{Queue: QueueStripe, ScheduledAt: time.Now().Add(time.Hour)},
	}
	for _, options := range inserts {
		if _, err := client.Insert(t.Context(), sampledArgs{}, &options); err != nil {
			t.Fatalf("insert job: %v", err)
		}
	}
	registry := prometheus.NewRegistry()
	observer, err := newObserver(pool, logger, registry)
	if err != nil {
		t.Fatalf("newObserver: %v", err)
	}
	events := make(chan *river.Event, 5)
	sampleTicks := make(chan time.Time)
	stopped := make(chan struct{})
	go func() {
		observer.run(events, sampleTicks)
		close(stopped)
	}()

	sampleTicks <- time.Now()
	for _, event := range []*river.Event{
		{Kind: river.EventKindJobCompleted, Job: &rivertype.JobRow{Kind: "sampled", State: rivertype.JobStateCompleted}},
		{Kind: river.EventKindJobFailed, Job: &rivertype.JobRow{Kind: "sampled", State: rivertype.JobStateRetryable}},
		{Kind: river.EventKindJobFailed, Job: &rivertype.JobRow{Kind: "sampled", State: rivertype.JobStateAvailable}},
		{Kind: river.EventKindJobFailed, Job: &rivertype.JobRow{Kind: "sampled", State: rivertype.JobStateDiscarded}},
		{Kind: river.EventKindJobCancelled, Job: &rivertype.JobRow{Kind: "sampled", State: rivertype.JobStateCancelled}},
	} {
		events <- event
	}
	close(events)
	select {
	case <-stopped:
	case <-time.After(observerWaitTimeout):
		t.Fatalf("observer still running %s after its events closed", observerWaitTimeout)
	}

	wantAvailable := map[string]float64{
		"queue=default":     0,
		"queue=imports":     0,
		"queue=ledger":      2,
		"queue=maintenance": 0,
		"queue=simulations": 0,
		"queue=stripe":      0,
		"queue=webhooks":    1,
	}
	if diff := cmp.Diff(wantAvailable, gatherSeries(t, registry, "preburn_river_queue_available")); diff != "" {
		t.Errorf("preburn_river_queue_available mismatch (-want +got):\n%s", diff)
	}
	wantCompleted := map[string]float64{
		"kind=sampled state=completed": 1,
		"kind=sampled state=retryable": 2,
		"kind=sampled state=discarded": 1,
		"kind=sampled state=cancelled": 1,
	}
	if diff := cmp.Diff(wantCompleted, gatherSeries(t, registry, "preburn_jobs_completed_total")); diff != "" {
		t.Errorf("preburn_jobs_completed_total mismatch (-want +got):\n%s", diff)
	}
}

func TestObserverLogsFailedSample(t *testing.T) {
	pool := databasetest.NewPool(t)
	var logs bytes.Buffer
	observer, err := newObserver(pool, logging.New(&logs, slog.LevelDebug), prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("newObserver: %v", err)
	}
	pool.Close()

	observer.sample()

	var record sampleFailedRecord
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatalf("parse log %q: %v", logs.String(), err)
	}
	if record.Message != string(logging.JobsQueueSampleFailed) || record.Error == "" {
		t.Errorf("logged %+v, want %s with an error", record, logging.JobsQueueSampleFailed)
	}
}

func TestNewObserverRejectsDuplicateRegistration(t *testing.T) {
	pool := databasetest.NewPool(t)
	logger := logging.New(t.Output(), slog.LevelDebug)
	registry := prometheus.NewRegistry()
	if _, err := newObserver(pool, logger, registry); err != nil {
		t.Fatalf("first newObserver: %v", err)
	}

	if _, err := newObserver(pool, logger, registry); err == nil {
		t.Error("second newObserver on the same registry succeeded, want an error")
	}
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
			value := metric.GetCounter().GetValue()
			if family.GetType() == dto.MetricType_GAUGE {
				value = metric.GetGauge().GetValue()
			}
			series[strings.Join(labels, " ")] = value
		}
	}
	return series
}
