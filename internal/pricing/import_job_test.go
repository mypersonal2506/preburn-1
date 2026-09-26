package pricing_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/pricing"
)

const liteLLMSnapshotMaximumBytes = 20 << 20

func TestLiteLLMRefreshJobDeclaresKindAndQueue(t *testing.T) {
	t.Parallel()
	arguments := pricing.LiteLLMRefreshArgs{}

	if kind := arguments.Kind(); kind != "pricing_litellm_refresh" {
		t.Errorf("kind = %s, want pricing_litellm_refresh", kind)
	}
	if queue := arguments.InsertOpts().Queue; queue != jobs.QueueMaintenance {
		t.Errorf("queue = %s, want %s", queue, jobs.QueueMaintenance)
	}
}

func TestLiteLLMRefreshWorkerImportsDownloadedSnapshot(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	paddedSnapshot := append([]byte(liteLLMFixture), bytes.Repeat([]byte(" "), liteLLMSnapshotMaximumBytes-len(liteLLMFixture))...)
	worker := pricing.NewLiteLLMRefreshWorker(pool, cachetest.NewClient(t), serveSnapshot(t, http.StatusOK, paddedSnapshot), clock.NewManual(importStart), discardLogger())

	err := worker.Work(t.Context(), refreshJob())

	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if rules := selectStoredRules(t, pool, "litellm"); len(rules) != liteLLMFixtureRuleCount {
		t.Errorf("stored %d rules, want %d", len(rules), liteLLMFixtureRuleCount)
	}
}

func TestLiteLLMRefreshWorkerCancelsInvalidSnapshot(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	tests := []struct {
		name     string
		snapshot []byte
	}{
		{name: "above the size limit", snapshot: bytes.Repeat([]byte(" "), liteLLMSnapshotMaximumBytes+1)},
		{name: "not JSON", snapshot: []byte(`{"gpt-example": `)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			worker := pricing.NewLiteLLMRefreshWorker(pool, cachetest.NewClient(t), serveSnapshot(t, http.StatusOK, test.snapshot), clock.NewManual(importStart), discardLogger())

			err := worker.Work(t.Context(), refreshJob())

			var cancelError *rivertype.JobCancelError
			if !errors.As(err, &cancelError) || !errors.Is(err, pricing.ErrInvalidLiteLLMSnapshot) {
				t.Errorf("Work error = %v, want a job cancel wrapping ErrInvalidLiteLLMSnapshot", err)
			}
		})
	}
	if rules := selectStoredRules(t, pool, "litellm"); len(rules) != 0 {
		t.Errorf("stored %d rules, want 0", len(rules))
	}
}

func TestLiteLLMRefreshWorkerRetriesFailedDownload(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	worker := pricing.NewLiteLLMRefreshWorker(pool, cachetest.NewClient(t), serveSnapshot(t, http.StatusServiceUnavailable, nil), clock.NewManual(importStart), discardLogger())

	err := worker.Work(t.Context(), refreshJob())

	var cancelError *rivertype.JobCancelError
	if err == nil || errors.As(err, &cancelError) {
		t.Errorf("Work error = %v, want an error River retries", err)
	}
}

func TestLiteLLMRefreshWorkerPublishesPricingInvalidationAfterChanges(t *testing.T) {
	t.Parallel()
	pool := databasetest.NewPool(t)
	cacheClient := cachetest.NewClient(t)
	subscription := subscribeInvalidationMessages(t, cacheClient)
	timeSource := clock.NewManual(importStart)
	worker := pricing.NewLiteLLMRefreshWorker(pool, cacheClient, serveSnapshot(t, http.StatusOK, []byte(liteLLMFixture)), timeSource, discardLogger())

	if err := worker.Work(t.Context(), refreshJob()); err != nil {
		t.Fatalf("first Work: %v", err)
	}

	for _, environment := range []httpapi.Environment{httpapi.EnvironmentTest, httpapi.EnvironmentLive} {
		assertInvalidation(t, subscription, cache.Invalidation{Kind: cache.InvalidationKindPricing, Environment: string(environment)})
	}
	if rules := selectStoredRules(t, pool, "litellm"); len(rules) == 0 || !rules[0].ImportedAt.Equal(importStart) {
		t.Errorf("stored rules %v, want rules that record the download time %s", rules, importStart)
	}
	timeSource.Advance(24 * time.Hour)
	if err := worker.Work(t.Context(), refreshJob()); err != nil {
		t.Fatalf("second Work: %v", err)
	}
	sentinel := cache.Invalidation{Kind: cache.InvalidationKindPlan, Environment: string(httpapi.EnvironmentTest)}
	if err := cacheClient.PublishInvalidation(t.Context(), sentinel); err != nil {
		t.Fatalf("publish sentinel invalidation: %v", err)
	}
	assertInvalidation(t, subscription, sentinel)
}

func serveSnapshot(t *testing.T, status int, snapshot []byte) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(status)
		_, _ = writer.Write(snapshot)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func refreshJob() *river.Job[pricing.LiteLLMRefreshArgs] {
	return &river.Job[pricing.LiteLLMRefreshArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: pricing.LiteLLMRefreshArgs{}}
}
