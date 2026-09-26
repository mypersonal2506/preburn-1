package pricing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/logging"
)

// LiteLLMSnapshotURL is the current LiteLLM price file on the main branch of
// its repository, which pricing_litellm_refresh jobs download.
const LiteLLMSnapshotURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

const (
	liteLLMRefreshKind          = "pricing_litellm_refresh"
	liteLLMDownloadTimeout      = 30 * time.Second
	liteLLMSnapshotMaximumBytes = 20 << 20
	liteLLMRefreshHour          = 5
	liteLLMRefreshMinute        = 0
)

// LiteLLMRefreshArgs is the job of kind pricing_litellm_refresh, which
// downloads the current LiteLLM price file and imports it. It has no
// arguments and runs on the maintenance queue.
type LiteLLMRefreshArgs struct{}

// LiteLLMRefreshWorker works pricing_litellm_refresh jobs. Create one with
// NewLiteLLMRefreshWorker.
type LiteLLMRefreshWorker struct {
	river.WorkerDefaults[LiteLLMRefreshArgs]
	pool        *pgxpool.Pool
	cache       *cache.Client
	snapshotURL string
	client      *http.Client
	clock       clock.Clock
	logger      *logging.Logger
}

// NewLiteLLMRefreshWorker returns a LiteLLMRefreshWorker that downloads the
// snapshot from snapshotURL, normally LiteLLMSnapshotURL, imports it into
// pool, publishes pricing invalidations through cacheClient, reads time from
// timeSource and logs to logger.
func NewLiteLLMRefreshWorker(pool *pgxpool.Pool, cacheClient *cache.Client, snapshotURL string, timeSource clock.Clock, logger *logging.Logger) *LiteLLMRefreshWorker {
	return &LiteLLMRefreshWorker{
		pool:        pool,
		cache:       cacheClient,
		snapshotURL: snapshotURL,
		client:      &http.Client{Timeout: liteLLMDownloadTimeout},
		clock:       timeSource,
		logger:      logger,
	}
}

// LiteLLMRefreshPeriodicJob returns the periodic job that inserts a
// pricing_litellm_refresh job every day at 05:00 UTC. The worker process
// registers it only when PREBURN_PRICING_LITELLM_REFRESH is true, so a
// default install makes no outbound calls.
func LiteLLMRefreshPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(jobs.DailyAt(liteLLMRefreshHour, liteLLMRefreshMinute), func() (river.JobArgs, *river.InsertOpts) {
		return LiteLLMRefreshArgs{}, nil
	}, nil)
}

// Kind returns pricing_litellm_refresh.
func (LiteLLMRefreshArgs) Kind() string {
	return liteLLMRefreshKind
}

// InsertOpts places pricing_litellm_refresh jobs on the maintenance queue.
func (LiteLLMRefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance}
}

// Work downloads the snapshot within 30 seconds and imports it with
// ImportLiteLLM as fetched when the download started. When the import wrote
// rules, it publishes the pricing invalidation of each environment, so every
// process drops its cached rule sets. A download above 20 MiB or a snapshot
// ImportLiteLLM rejects cancels the job with an error wrapping
// ErrInvalidLiteLLMSnapshot, since a retry would fetch the same file. Other
// failures, such as a network error or a status other than 200, return an
// error River retries.
func (worker *LiteLLMRefreshWorker) Work(ctx context.Context, _ *river.Job[LiteLLMRefreshArgs]) error {
	fetchedAt := worker.clock.Now()
	content, err := worker.download(ctx)
	if err != nil {
		return cancelInvalidSnapshot(err)
	}
	summary, err := ImportLiteLLM(ctx, worker.pool, LiteLLMSnapshot{Content: content, FetchedAt: fetchedAt}, worker.clock, worker.logger)
	if err != nil {
		return cancelInvalidSnapshot(err)
	}
	if !summary.wroteRules() {
		return nil
	}
	for _, environment := range []httpapi.Environment{httpapi.EnvironmentTest, httpapi.EnvironmentLive} {
		err := worker.cache.PublishInvalidation(ctx, cache.Invalidation{Kind: cache.InvalidationKindPricing, Environment: string(environment)})
		if err != nil {
			return err
		}
	}
	return nil
}

func (worker *LiteLLMRefreshWorker) download(ctx context.Context) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, worker.snapshotURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build litellm snapshot request: %w", err)
	}
	response, err := worker.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download litellm snapshot: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download litellm snapshot: status %d", response.StatusCode)
	}
	snapshot, err := io.ReadAll(io.LimitReader(response.Body, liteLLMSnapshotMaximumBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read litellm snapshot: %w", err)
	}
	if len(snapshot) > liteLLMSnapshotMaximumBytes {
		return nil, fmt.Errorf("%w: larger than %d bytes", ErrInvalidLiteLLMSnapshot, liteLLMSnapshotMaximumBytes)
	}
	return snapshot, nil
}

func cancelInvalidSnapshot(err error) error {
	if errors.Is(err, ErrInvalidLiteLLMSnapshot) {
		return river.JobCancel(err)
	}
	return err
}
