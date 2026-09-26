package decisions

import (
	"context"
	_ "embed"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/decisions/queries"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/money"
)

const (
	reconcileKind              = "counters_reconcile"
	reconcileScriptName        = "reconcile"
	reconcileInterval          = time.Minute
	reconcileFullSweepInterval = time.Hour
	reconcileActivityOverlap   = time.Minute
	abandonedChangeAge         = time.Minute
	counterBatchSize           = 500
	missingFieldValue          = ""
	replyReconciled            = "reconciled"
	replySkipped               = "skipped"
	exclusiveScoreBound        = "("
)

// ReconcileArgs is the job of kind counters_reconcile, which repairs Redis
// counters that drifted from Postgres. It has no arguments and runs on the
// default queue.
type ReconcileArgs struct{}

// ReconcileWorker works counters_reconcile jobs. It keeps the time of its
// last run in memory, so each process starts with a full sweep. Create one
// with NewReconcileWorker.
type ReconcileWorker struct {
	river.WorkerDefaults[ReconcileArgs]
	queries         *queries.Queries
	counters        *Counters
	expiry          *ExpiryWorker
	script          *cache.Script
	clock           clock.Clock
	repairs         prometheus.Counter
	running         sync.Mutex
	lastRunAt       time.Time
	lastFullSweepAt time.Time
}

type liveCounter struct {
	key         string
	customerID  uuid.UUID
	periodStart time.Time
	periodEnd   time.Time
}

type counterRead struct {
	counter          liveCounter
	fields           *redis.MapStringStringCmd
	pendingChanges   *redis.IntCmd
	openReservations *redis.StringSliceCmd
}

//go:embed scripts/reconcile.lua
var reconcileSource string

// NewReconcileWorker returns a ReconcileWorker that compares the counters in
// counters with the decisions and ledger entries in pool, expires due
// reservations through expiry before each comparison, reads time from
// timeSource and registers preburn_counter_repairs_total on registry. It
// returns an error when that metric is already registered there.
func NewReconcileWorker(pool *pgxpool.Pool, counters *Counters, expiry *ExpiryWorker, timeSource clock.Clock, registry prometheus.Registerer) (*ReconcileWorker, error) {
	repairs := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "preburn_counter_repairs_total",
		Help: "Counters whose Redis fields or reservations reconcile changed because they differed from Postgres.",
	})
	if err := registry.Register(repairs); err != nil {
		return nil, fmt.Errorf("register reconcile metrics: %w", err)
	}
	return &ReconcileWorker{
		queries:  queries.New(pool),
		counters: counters,
		expiry:   expiry,
		script:   cache.NewScript(reconcileScriptName, reconcileSource),
		clock:    timeSource,
		repairs:  repairs,
	}, nil
}

// ReconcilePeriodicJob returns the periodic job that inserts a
// counters_reconcile job every 60 seconds.
func ReconcilePeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(jobs.Every(reconcileInterval), func() (river.JobArgs, *river.InsertOpts) {
		return ReconcileArgs{}, nil
	}, nil)
}

// Kind returns counters_reconcile.
func (ReconcileArgs) Kind() string {
	return reconcileKind
}

// InsertOpts places counters_reconcile jobs on the default queue.
func (ReconcileArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueDefault}
}

// Work repairs the live counters that drifted from Postgres. A live counter
// belongs to a customer period that contains the current time or holds a
// reservation that has not expired. The first run in a process and the first
// run an hour or more after the last full sweep cover every live counter.
// Other runs cover the live counters of customers with a decision or ledger
// entry created since one minute before the previous run, or with a released
// or expired decision whose hold time ended after that moment. Work first
// expires the due reservations.
//
// A counter with a pending change, one a check, report or correction
// registered less than a minute ago, is left alone, because Postgres and the
// counter disagree until that change lands in both. A change pending for a
// minute or more is dropped as abandoned, and a settlement that arrives
// after that applies nothing. Work reads a counter's fields and pending
// changes before Postgres and checks the pending changes again when it
// writes, so a change that starts or lands between the reads is never
// counted twice.
//
// Otherwise it writes, in one step per counter:
//   - reserved and reserved:{feature} as the sum of the counter's
//     reservations that are still reserved in Redis, which is what the
//     settle and release scripts will subtract. A reservation whose decision
//     Postgres already marks settled, released or expired is first given that
//     status, since its settle or release never reached Redis. A reservation
//     without a decision row, left by a check whose reply was lost, stays
//     reserved until the expiry job releases it.
//   - settled and settled:{feature} from ledger entry costs, and count and
//     count:{feature} from decisions the reserve script counted (every reason
//     except hard_limit_reached) plus ledger entries without a decision that
//     are not corrections. Each of these fields is written only while it
//     still holds the value Work read, so an increment that lands between the
//     read and the write is kept.
//
// Each counter that changed adds one to preburn_counter_repairs_total. A run
// that starts while another run of this worker is in progress returns nil at
// once.
func (worker *ReconcileWorker) Work(ctx context.Context, _ *river.Job[ReconcileArgs]) error {
	if !worker.running.TryLock() {
		return nil
	}
	defer worker.running.Unlock()
	now := worker.clock.Now()
	fullSweep := worker.lastFullSweepAt.IsZero() || !now.Before(worker.lastFullSweepAt.Add(reconcileFullSweepInterval))
	if err := worker.expiry.expireDue(ctx, now); err != nil {
		return err
	}
	for _, environment := range []httpapi.Environment{httpapi.EnvironmentTest, httpapi.EnvironmentLive} {
		liveCounters, err := worker.liveCounters(ctx, environment, now, fullSweep)
		if err != nil {
			return err
		}
		for batch := range slices.Chunk(liveCounters, counterBatchSize) {
			if err := worker.reconcileBatch(ctx, environment, batch, now); err != nil {
				return err
			}
		}
	}
	worker.lastRunAt = now
	if fullSweep {
		worker.lastFullSweepAt = now
	}
	return nil
}

func (worker *ReconcileWorker) liveCounters(ctx context.Context, environment httpapi.Environment, now time.Time, fullSweep bool) ([]liveCounter, error) {
	if fullSweep {
		return listLiveCounters(ctx, worker.queries, worker.counters, environment, now)
	}
	rows, err := worker.queries.ListActiveCustomerLiveCounters(ctx, queries.ListActiveCustomerLiveCountersParams{
		Environment:   queries.Environment(environment),
		Now:           now,
		ReservedAfter: reservedAfter(now),
		// created_at is the service time before the inserting transaction, so a
		// row that committed after the previous run read the tables can be
		// older than that run.
		ActiveSince: worker.lastRunAt.Add(-reconcileActivityOverlap),
	})
	if err != nil {
		return nil, fmt.Errorf("list live counters of active customers: %w", err)
	}
	live := make([]liveCounter, 0, len(rows))
	for _, row := range rows {
		live = append(live, newLiveCounter(worker.counters, environment, row.CustomerID, row.PeriodStart, row.PeriodEnd))
	}
	return live, nil
}

func (worker *ReconcileWorker) reconcileBatch(ctx context.Context, environment httpapi.Environment, batch []liveCounter, now time.Time) error {
	abandonedUntil := now.Add(-abandonedChangeAge)
	// Redis is read before Postgres and a counter with a pending change is
	// left out. Otherwise a change caught between its Postgres and Redis steps
	// reads as drift, and the write counts it twice or drops it.
	reads, err := worker.readQuietCounters(ctx, batch, abandonedUntil)
	if err != nil {
		return err
	}
	counters := make([]liveCounter, 0, len(reads))
	var openReservations []uuid.UUID
	for _, read := range reads {
		counters = append(counters, read.counter)
		for _, member := range read.openReservations.Val() {
			decisionID, err := uuid.Parse(member)
			if err != nil {
				return fmt.Errorf("parse open reservation %q of %s: %w", member, read.counter.key, err)
			}
			openReservations = append(openReservations, decisionID)
		}
	}
	expected, err := expectedCounterFields(ctx, worker.queries, worker.counters, environment, counters, reservedAfter(now))
	if err != nil {
		return err
	}
	finished, err := worker.queries.ListFinishedDecisions(ctx, queries.ListFinishedDecisionsParams{
		Environment: queries.Environment(environment),
		DecisionIds: openReservations,
	})
	if err != nil {
		return fmt.Errorf("list finished decisions: %w", err)
	}
	finishedStatuses := make(map[string]string, len(finished))
	for _, decision := range finished {
		finishedStatuses[decision.DecisionID.String()] = decision.Status
	}
	for _, read := range reads {
		arguments := worker.repairArguments(read, expected[read.counter.key], finishedStatuses, abandonedUntil)
		if err := worker.repair(ctx, read.counter, arguments); err != nil {
			return err
		}
	}
	return nil
}

func (worker *ReconcileWorker) readQuietCounters(ctx context.Context, batch []liveCounter, abandonedUntil time.Time) ([]counterRead, error) {
	pipeline := worker.counters.cache.Redis().TxPipeline()
	reads := make([]counterRead, len(batch))
	for index, counter := range batch {
		reads[index] = counterRead{
			counter:          counter,
			fields:           pipeline.HGetAll(ctx, counter.key),
			pendingChanges:   pipeline.ZCount(ctx, counter.key+pendingChangesKeySuffix, exclusiveScoreBound+formatInteger(abandonedUntil.UnixMilli()), "+inf"),
			openReservations: pipeline.SMembers(ctx, counter.key+openReservationsKeySuffix),
		}
	}
	if _, err := pipeline.Exec(ctx); err != nil {
		return nil, fmt.Errorf("read counters: %w", err)
	}
	return slices.DeleteFunc(reads, func(read counterRead) bool {
		return read.pendingChanges.Val() > 0
	}), nil
}

func (worker *ReconcileWorker) repairArguments(read counterRead, expected map[string]int64, finishedStatuses map[string]string, abandonedUntil time.Time) []any {
	var finished []any
	for _, member := range read.openReservations.Val() {
		if status, isFinished := finishedStatuses[member]; isFinished {
			finished = append(finished, member, status)
		}
	}
	arguments := []any{
		formatInteger(abandonedUntil.UnixMilli()),
		formatUnix(counterExpiry(read.counter.periodEnd)),
		worker.counters.cache.Key(reservationKeyName, ""),
		strconv.Itoa(len(finished) / 2),
	}
	arguments = append(arguments, finished...)
	current := read.fields.Val()
	for field, value := range expected {
		if !isReservedField(field) {
			arguments = appendDifference(arguments, field, current[field], value)
		}
	}
	for field, value := range current {
		if _, isExpected := expected[field]; !isExpected && !isReservedField(field) {
			arguments = appendDifference(arguments, field, value, 0)
		}
	}
	return arguments
}

func (worker *ReconcileWorker) repair(ctx context.Context, counter liveCounter, arguments []any) error {
	keys := []string{
		counter.key,
		counter.key + pendingChangesKeySuffix,
		counter.key + openReservationsKeySuffix,
		worker.counters.cache.Key(reservationsExpiringKeyName),
		worker.counters.cache.Key(countersReadyKeyName),
	}
	reply, err := worker.counters.cache.RunScript(ctx, worker.script, keys, arguments...)
	if err != nil {
		return err
	}
	parts, err := replyParts(reconcileScriptName, reply)
	if err != nil {
		return err
	}
	if len(parts) == 2 && parts[0] == replySkipped {
		return nil
	}
	if len(parts) != 2 || parts[0] != replyReconciled {
		return fmt.Errorf("unexpected %s script reply %q", reconcileScriptName, parts)
	}
	written, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("unexpected %s script reply %q: %w", reconcileScriptName, parts, err)
	}
	if written > 0 {
		worker.repairs.Inc()
	}
	return nil
}

func appendDifference(arguments []any, field string, current string, expected int64) []any {
	expectedValue := formatInteger(expected)
	if current == expectedValue || (current == missingFieldValue && expected == 0) {
		return arguments
	}
	return append(arguments, field, current, expectedValue)
}

func isReservedField(field string) bool {
	return field == fieldReserved || strings.HasPrefix(field, fieldReserved+featureFieldSeparator)
}

func listLiveCounters(ctx context.Context, store *queries.Queries, counters *Counters, environment httpapi.Environment, now time.Time) ([]liveCounter, error) {
	rows, err := store.ListLiveCounters(ctx, queries.ListLiveCountersParams{
		Environment:   queries.Environment(environment),
		Now:           now,
		ReservedAfter: reservedAfter(now),
	})
	if err != nil {
		return nil, fmt.Errorf("list live counters: %w", err)
	}
	live := make([]liveCounter, 0, len(rows))
	for _, row := range rows {
		live = append(live, newLiveCounter(counters, environment, row.CustomerID, row.PeriodStart, row.PeriodEnd))
	}
	return live, nil
}

func newLiveCounter(counters *Counters, environment httpapi.Environment, customerID uuid.UUID, periodStart, periodEnd time.Time) liveCounter {
	return liveCounter{
		key:         counters.CounterKey(environment, customerID, periodStart),
		customerID:  customerID,
		periodStart: periodStart,
		periodEnd:   periodEnd,
	}
}

func expectedCounterFields(ctx context.Context, store *queries.Queries, counters *Counters, environment httpapi.Environment, batch []liveCounter, reservedAfter time.Time) (map[string]map[string]int64, error) {
	customerIDs := make([]uuid.UUID, len(batch))
	periodStarts := make([]time.Time, len(batch))
	expected := make(map[string]map[string]int64, len(batch))
	for index, counter := range batch {
		customerIDs[index] = counter.customerID
		periodStarts[index] = counter.periodStart
		expected[counter.key] = map[string]int64{fieldSettled: 0, fieldReserved: 0, fieldCount: 0}
	}
	rows, err := store.ListCounterFeatureTotals(ctx, queries.ListCounterFeatureTotalsParams{
		Environment:   queries.Environment(environment),
		ReservedAfter: reservedAfter,
		CustomerIds:   customerIDs,
		PeriodStarts:  periodStarts,
	})
	if err != nil {
		return nil, fmt.Errorf("sum counter fields: %w", err)
	}
	for _, row := range rows {
		fields := expected[counters.CounterKey(environment, row.CustomerID, row.PeriodStart)]
		fields[featureField(fieldSettled, row.Feature)] = row.SettledNanos
		fields[featureField(fieldReserved, row.Feature)] = row.ReservedNanos
		fields[featureField(fieldCount, row.Feature)] = row.RequestCount
		settled, err := money.Amount(fields[fieldSettled]).Add(money.Amount(row.SettledNanos))
		if err != nil {
			return nil, fmt.Errorf("settled total of customer %s: %w", row.CustomerID, err)
		}
		reserved, err := money.Amount(fields[fieldReserved]).Add(money.Amount(row.ReservedNanos))
		if err != nil {
			return nil, fmt.Errorf("reserved total of customer %s: %w", row.CustomerID, err)
		}
		fields[fieldSettled] = int64(settled)
		fields[fieldReserved] = int64(reserved)
		fields[fieldCount] += row.RequestCount
	}
	return expected, nil
}

// reservedAfter returns the expiry a reserved decision must be later than to
// still hold its reservation at now. ExpiredReservations releases a
// reservation once now reaches its expiry rounded up to a whole second, so a
// reservation that expired within the current second is still held.
func reservedAfter(now time.Time) time.Time {
	return now.Truncate(time.Second)
}
