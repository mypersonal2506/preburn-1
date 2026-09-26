package ledger

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/ledger/queries"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	uncostedRerateBatchSize        = 1000
	uncostedRerateTimeout          = time.Hour
	correctionIdempotencyKeyPrefix = "correction:"
)

// RerateDependencies are what an UncostedRerateWorker reads and writes.
type RerateDependencies struct {
	// Pool holds the ledger entries, the rollups and the River jobs.
	Pool *pgxpool.Pool
	// Pricing loads the RuleSet and the override digest of an environment.
	Pricing *pricing.Service
	// Cache publishes the customer invalidations.
	Cache *cache.Client
	// BeginCorrection registers a new correction entry as a pending change of
	// the Redis counter of its customer period, as
	// decisions.Counters.BeginChange does, inside the transaction that stores
	// it.
	BeginCorrection func(ctx context.Context, correction Entry) error
	// SettleCorrection finishes that pending change by adding the cost of the
	// correction to the counter without adding to its counts, as
	// decisions.Counters.SettleUnreserved does with a count increment of 0.
	SettleCorrection func(ctx context.Context, correction Entry) error
	// Clock tells the time of each pass.
	Clock clock.Clock
	// Logger writes the events of the job.
	Logger *logging.Logger
}

// UncostedRerateWorker works uncosted_rerate jobs. Create one with
// NewUncostedRerateWorker.
type UncostedRerateWorker struct {
	river.WorkerDefaults[pricing.UncostedRerateArgs]
	pool             *pgxpool.Pool
	queries          *queries.Queries
	pricing          *pricing.Service
	cache            *cache.Client
	beginCorrection  func(ctx context.Context, correction Entry) error
	settleCorrection func(ctx context.Context, correction Entry) error
	clock            clock.Clock
	logger           *logging.Logger
}

type storedCorrection struct {
	entry        Entry
	registration error
}

type rollupAnchor struct {
	customerID  uuid.UUID
	periodStart time.Time
}

var errOverridesChanged = errors.New("overrides changed during the rerate pass")

// NewUncostedRerateWorker returns an UncostedRerateWorker over dependencies.
// It only stores them.
func NewUncostedRerateWorker(dependencies RerateDependencies) *UncostedRerateWorker {
	return &UncostedRerateWorker{
		pool:             dependencies.Pool,
		queries:          queries.New(dependencies.Pool),
		pricing:          dependencies.Pricing,
		cache:            dependencies.Cache,
		beginCorrection:  dependencies.BeginCorrection,
		settleCorrection: dependencies.SettleCorrection,
		clock:            dependencies.Clock,
		logger:           dependencies.Logger,
	}
}

// Timeout gives an uncosted_rerate job one hour. A job that runs out keeps
// the batches it committed, and its retry continues with the entries that
// still have no correction.
func (worker *UncostedRerateWorker) Timeout(*river.Job[pricing.UncostedRerateArgs]) time.Duration {
	return uncostedRerateTimeout
}

// Work re-rates the uncosted ledger entries of the job's environment that
// occurred within pricing.RerateWindow before now and have no correction.
// It reads the RuleSet from Postgres, not from a cache, and prices each entry
// with pricing.RateUncosted: at its occurred_at, then each meter still
// missing at now.
//
// It goes through the entries 1,000 at a time in id order. Each batch
// inserts, in one transaction, a correction entry for every entry that
// becomes costed and refreshes the rollups of each corrected customer period.
// A correction has the entry's customer, customer user, feature, provider,
// model, attributes, usage, decision source, period and occurred_at, no
// decision, correction_of set to the entry and the idempotency key
// correction:<entry UUID>. Before the commit it runs BeginCorrection for each
// correction whose period has not ended, and after the commit
// SettleCorrection for each of them, logging ledger.rerate_settle_deferred
// when either fails. It then publishes the customer invalidation of each
// corrected customer.
//
// A pass ends by marking the job completed. That waits for the override
// changes whose insert of this job River skipped as a duplicate, and when the
// overrides of the environment differ from those the pass read, it rolls the
// completion back and runs another pass. It logs
// ledger.uncosted_rerate_completed with the environment and the number of
// corrections.
func (worker *UncostedRerateWorker) Work(ctx context.Context, job *river.Job[pricing.UncostedRerateArgs]) error {
	environment := job.Args.Environment
	var corrected int
	for {
		digest, err := worker.pricing.OverrideDigest(ctx, environment)
		if err != nil {
			return err
		}
		ruleSet, err := worker.pricing.LoadRuleSet(ctx, environment)
		if err != nil {
			return fmt.Errorf("load %s rule set: %w", environment, err)
		}
		passCorrected, err := worker.rerate(ctx, environment, ruleSet)
		if err != nil {
			return err
		}
		corrected += passCorrected
		completed, err := worker.complete(ctx, job, digest)
		if err != nil {
			return err
		}
		if completed {
			break
		}
	}
	worker.logger.Info(ctx, logging.LedgerUncostedRerateCompleted, slog.String("environment", string(environment)), slog.Int("corrections", corrected))
	return nil
}

func (worker *UncostedRerateWorker) rerate(ctx context.Context, environment httpapi.Environment, ruleSet *pricing.RuleSet) (int, error) {
	now := worker.clock.Now()
	var corrected int
	afterLedgerEntryID := uuid.Nil
	for {
		rows, err := worker.queries.ListUncorrectedUncostedEntries(ctx, queries.ListUncorrectedUncostedEntriesParams{
			Environment:        queries.Environment(environment),
			OccurredSince:      now.Add(-pricing.RerateWindow),
			AfterLedgerEntryID: afterLedgerEntryID,
			BatchSize:          uncostedRerateBatchSize,
		})
		if err != nil {
			return 0, fmt.Errorf("list uncorrected uncosted entries: %w", err)
		}
		corrections, err := correctionsOf(rows, ruleSet, now)
		if err != nil {
			return 0, err
		}
		stored, err := worker.storeCorrections(ctx, environment, corrections, now)
		if err != nil {
			return 0, err
		}
		corrected += len(stored)
		if err := worker.settleAndInvalidate(ctx, environment, stored, now); err != nil {
			return 0, err
		}
		if len(rows) < uncostedRerateBatchSize {
			return corrected, nil
		}
		afterLedgerEntryID = rows[len(rows)-1].LedgerEntry.LedgerEntryID
	}
}

func (worker *UncostedRerateWorker) storeCorrections(ctx context.Context, environment httpapi.Environment, corrections []Entry, now time.Time) ([]storedCorrection, error) {
	if len(corrections) == 0 {
		return nil, nil
	}
	stored := make([]storedCorrection, 0, len(corrections))
	err := database.InTransaction(ctx, worker.pool, func(ctx context.Context, transaction pgx.Tx) error {
		entries := make([]Entry, 0, len(corrections))
		for _, correction := range corrections {
			entry, duplicate, err := InsertEntry(ctx, transaction, correction)
			if err != nil {
				return err
			}
			if !duplicate {
				entries = append(entries, entry)
			}
		}
		if err := refreshCorrectedRollups(ctx, transaction, environment, entries); err != nil {
			return err
		}
		for _, entry := range entries {
			correction := storedCorrection{entry: entry}
			if entry.PeriodEnd.After(now) {
				correction.registration = worker.beginCorrection(ctx, entry)
			}
			stored = append(stored, correction)
		}
		return nil
	})
	return stored, err
}

func (worker *UncostedRerateWorker) settleAndInvalidate(ctx context.Context, environment httpapi.Environment, corrections []storedCorrection, now time.Time) error {
	customerIDs := map[uuid.UUID]struct{}{}
	for _, correction := range corrections {
		customerIDs[correction.entry.CustomerID] = struct{}{}
		if correction.entry.PeriodEnd.After(now) {
			worker.settle(ctx, correction)
		}
	}
	for customerID := range customerIDs {
		err := worker.cache.PublishInvalidation(ctx, cache.Invalidation{
			Kind:        cache.InvalidationKindCustomer,
			Environment: string(environment),
			ID:          identifiers.Encode(identifiers.PrefixCustomer, customerID),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (worker *UncostedRerateWorker) settle(ctx context.Context, correction storedCorrection) {
	err := correction.registration
	if err == nil {
		err = worker.settleCorrection(ctx, correction.entry)
	}
	if err != nil {
		worker.logger.Warn(ctx, logging.LedgerRerateSettleDeferred,
			slog.String("environment", string(correction.entry.Environment)),
			slog.String("ledger_entry_id", identifiers.Encode(identifiers.PrefixLedgerEntry, correction.entry.ID)),
			slog.String("error", err.Error()),
		)
	}
}

func (worker *UncostedRerateWorker) complete(ctx context.Context, job *river.Job[pricing.UncostedRerateArgs], digest string) (bool, error) {
	err := database.InTransaction(ctx, worker.pool, func(ctx context.Context, transaction pgx.Tx) error {
		// Completing the job waits for override changes that River deduplicated onto
		// this running job, so only a digest read after it includes their overrides.
		if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, transaction, job); err != nil {
			return fmt.Errorf("complete uncosted rerate job: %w", err)
		}
		current, err := worker.pricing.OverrideDigest(ctx, job.Args.Environment)
		if err != nil {
			return err
		}
		if current != digest {
			return errOverridesChanged
		}
		return nil
	})
	if errors.Is(err, errOverridesChanged) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func correctionsOf(rows []queries.ListUncorrectedUncostedEntriesRow, ruleSet *pricing.RuleSet, now time.Time) ([]Entry, error) {
	var corrections []Entry
	for _, row := range rows {
		original, err := entryFromRow(row.LedgerEntry)
		if err != nil {
			return nil, err
		}
		rating, err := pricing.RateUncosted(pricing.RatingRequest{
			Provider:   original.Provider,
			Model:      original.Model,
			Attributes: original.Attributes,
			Usage:      original.Usage,
			OccurredAt: original.OccurredAt,
		}, ruleSet, now)
		if err != nil {
			return nil, fmt.Errorf("rate ledger entry %s: %w", original.ID, err)
		}
		if rating.CostStatus == pricing.CostStatusCosted {
			corrections = append(corrections, correctionOf(original, rating, now))
		}
	}
	return corrections, nil
}

func correctionOf(original Entry, rating pricing.RatedRequest, now time.Time) Entry {
	return Entry{
		ID:             identifiers.New(),
		Environment:    original.Environment,
		CustomerID:     original.CustomerID,
		CustomerUserID: original.CustomerUserID,
		IdempotencyKey: correctionIdempotencyKeyPrefix + original.ID.String(),
		Feature:        original.Feature,
		Provider:       original.Provider,
		Model:          original.Model,
		Attributes:     original.Attributes,
		Usage:          original.Usage,
		Rating:         rating,
		DecisionSource: original.DecisionSource,
		PeriodStart:    original.PeriodStart,
		PeriodEnd:      original.PeriodEnd,
		CorrectionOf:   &original.ID,
		OccurredAt:     original.OccurredAt,
		CreatedAt:      now,
	}
}

func refreshCorrectedRollups(ctx context.Context, transaction pgx.Tx, environment httpapi.Environment, corrections []Entry) error {
	anchors := make([]rollupAnchor, 0, len(corrections))
	for _, correction := range corrections {
		anchors = append(anchors, rollupAnchor{customerID: correction.CustomerID, periodStart: correction.PeriodStart})
	}
	slices.SortFunc(anchors, compareRollupAnchors)
	anchors = slices.CompactFunc(anchors, func(left, right rollupAnchor) bool { return compareRollupAnchors(left, right) == 0 })
	for _, anchor := range anchors {
		if err := RefreshRollups(ctx, transaction, environment, anchor.customerID, anchor.periodStart); err != nil {
			return fmt.Errorf("refresh rollups of customer %s: %w", anchor.customerID, err)
		}
	}
	return nil
}

func compareRollupAnchors(left, right rollupAnchor) int {
	return cmp.Or(bytes.Compare(left.customerID[:], right.customerID[:]), left.periodStart.Compare(right.periodStart))
}
