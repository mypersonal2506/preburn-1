package decisions_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/jobs/jobstest"
)

const (
	noScriptErrorPrefix  = "NOSCRIPT"
	postgresCounterQuery = `SELECT counter_rows.feature,
		sum(counter_rows.settled_nanos)::bigint,
		sum(counter_rows.reserved_nanos)::bigint,
		sum(counter_rows.request_count)::bigint
	FROM (
		SELECT feature, 0::bigint AS settled_nanos,
			CASE WHEN status = 'reserved' AND expires_at > $3 THEN reserved_nanos ELSE 0 END AS reserved_nanos,
			CASE WHEN reason = 'hard_limit_reached' THEN 0 ELSE 1 END AS request_count
		FROM decisions
		WHERE environment = 'test' AND customer_id = $1 AND period_start = $2
		UNION ALL
		SELECT feature, coalesce(cost_nanos, 0), 0,
			CASE WHEN decision_id IS NULL AND correction_of IS NULL THEN 1 ELSE 0 END
		FROM ledger_entries
		WHERE environment = 'test' AND customer_id = $1 AND period_start = $2
	) AS counter_rows
	GROUP BY counter_rows.feature`
)

type lifecycleHarness struct {
	*reportHarness
	expiry    *decisions.ExpiryWorker
	reconcile *decisions.ReconcileWorker
}

type scriptHook struct {
	firstKey  string
	before    func()
	after     func()
	loseReply bool
	refuse    bool
	running   atomic.Bool
	beforeRan atomic.Bool
	done      atomic.Bool
}

var (
	errReplyLost     = errors.New("connection reset before the reply")
	errScriptRefused = errors.New("connection refused before the script ran")
)

func newLifecycleHarness(t *testing.T) *lifecycleHarness {
	t.Helper()
	report := newReportHarness(t)
	registry := prometheus.NewRegistry()
	expiry, err := decisions.NewExpiryWorker(report.pool, report.counters, jobstest.NewInsertClient(t, report.pool), report.clock, registry)
	if err != nil {
		t.Fatalf("new expiry worker: %v", err)
	}
	reconcile, err := decisions.NewReconcileWorker(report.pool, report.counters, expiry, report.clock, registry)
	if err != nil {
		t.Fatalf("new reconcile worker: %v", err)
	}
	return &lifecycleHarness{reportHarness: report, expiry: expiry, reconcile: reconcile}
}

func (harness *lifecycleHarness) decisionID(t *testing.T, checked decisions.CheckResponse) uuid.UUID {
	t.Helper()
	return decodeIdentifier(t, identifiers.PrefixDecision, checked.DecisionID)
}

func (harness *lifecycleHarness) hook(hook *scriptHook) *scriptHook {
	harness.cache.Redis().AddHook(hook)
	return hook
}

func (harness *lifecycleHarness) postgresCounter(t *testing.T, customerID uuid.UUID, periodStart time.Time) map[string]string {
	t.Helper()
	rows, err := harness.pool.Query(t.Context(), postgresCounterQuery, customerID, periodStart, harness.clock.Now().Truncate(time.Second))
	if err != nil {
		t.Fatalf("sum counter rows: %v", err)
	}
	defer rows.Close()
	fields := map[string]string{}
	var settled, reserved, count int64
	for rows.Next() {
		var feature string
		var featureSettled, featureReserved, featureCount int64
		if err := rows.Scan(&feature, &featureSettled, &featureReserved, &featureCount); err != nil {
			t.Fatalf("scan counter row: %v", err)
		}
		fields["settled:"+feature] = strconv.FormatInt(featureSettled, 10)
		fields["reserved:"+feature] = strconv.FormatInt(featureReserved, 10)
		fields["count:"+feature] = strconv.FormatInt(featureCount, 10)
		settled += featureSettled
		reserved += featureReserved
		count += featureCount
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read counter rows: %v", err)
	}
	fields["settled"] = strconv.FormatInt(settled, 10)
	fields["reserved"] = strconv.FormatInt(reserved, 10)
	fields["count"] = strconv.FormatInt(count, 10)
	return fields
}

func (harness *lifecycleHarness) redisCounter(t *testing.T, customerID uuid.UUID, periodStart time.Time) map[string]string {
	t.Helper()
	fields, err := harness.cache.Redis().HGetAll(t.Context(), harness.counters.CounterKey(httpapi.EnvironmentTest, customerID, periodStart)).Result()
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return fields
}

func (hook *scriptHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return next(ctx, network, address)
	}
}

func (hook *scriptHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, command redis.Cmder) error {
		arguments := command.Args()
		matches := command.Name() == "evalsha" && len(arguments) > 3 && arguments[3] == hook.firstKey
		if !matches || hook.done.Load() || !hook.running.CompareAndSwap(false, true) {
			return next(ctx, command)
		}
		defer hook.running.Store(false)
		if hook.before != nil && hook.beforeRan.CompareAndSwap(false, true) {
			hook.before()
		}
		if hook.refuse {
			hook.done.Store(true)
			command.SetErr(errScriptRefused)
			return errScriptRefused
		}
		err := next(ctx, command)
		if redis.HasErrorPrefix(err, noScriptErrorPrefix) {
			return err
		}
		hook.done.Store(true)
		if hook.after != nil {
			hook.after()
		}
		if hook.loseReply && err == nil {
			command.SetErr(errReplyLost)
			return errReplyLost
		}
		return err
	}
}

func (hook *scriptHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
