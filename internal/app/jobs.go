package app

import (
	"context"
	"strconv"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	jobTracerName      = "github.com/preburn/preburn/internal/app"
	jobMessagingSystem = "river"
)

type tracedWorker[Args river.JobArgs] struct {
	river.Worker[Args]
}

type reconcileWorker struct {
	*decisions.ReconcileWorker
	bootstrap *decisions.CounterBootstrap
}

var jobSpan = river.WorkerMiddlewareFunc(func(ctx context.Context, job *rivertype.JobRow, doInner func(ctx context.Context) error) error {
	ctx, span := otel.Tracer(jobTracerName).Start(ctx, job.Kind,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystemKey.String(jobMessagingSystem),
			semconv.MessagingDestinationName(job.Queue),
			semconv.MessagingMessageID(strconv.FormatInt(job.ID, 10)),
		),
	)
	defer span.End()
	err := doInner(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
})

// RegisterJobs returns the River workers of every domain's job kinds, with the
// dependencies each domain reads from application, and the periodic jobs the
// elected leader runs: member_cleanup daily, reservations_expire every 5
// seconds, counters_reconcile every 60 seconds, usage_estimates_refresh
// hourly and decision_retention daily at 03:00 UTC. rollup_refresh and
// uncosted_rerate run when reports and override changes insert them.
// pricing_litellm_refresh and its daily schedule are registered only when
// PREBURN_PRICING_LITELLM_REFRESH is true, so a default install makes no
// outbound calls. Every job runs in a span named after its kind, and each
// counters_reconcile run first makes the counters ready with
// CounterBootstrap. RegisterJobs registers the expiry and reconcile metrics
// on Registry, so it runs once per App.
func RegisterJobs(application *App) (*river.Workers, []*river.PeriodicJob, error) {
	expiry, err := decisions.NewExpiryWorker(application.Pool, application.Counters, application.Jobs, application.Clock, application.Registry)
	if err != nil {
		return nil, nil, err
	}
	reconcile, err := decisions.NewReconcileWorker(application.Pool, application.Counters, expiry, application.Clock, application.Registry)
	if err != nil {
		return nil, nil, err
	}
	workers := river.NewWorkers()
	addWorker(workers, members.NewCleanupWorker(application.Pool, application.Clock, application.Logger))
	addWorker(workers, expiry)
	addWorker(workers, reconcileWorker{ReconcileWorker: reconcile, bootstrap: application.CounterBootstrap})
	addWorker(workers, decisions.NewRetentionWorker(application.Pool, application.Configuration.DecisionRetentionDays, application.Clock, application.Logger))
	addWorker(workers, ledger.NewRollupRefreshWorker(application.Pool))
	addWorker(workers, ledger.NewUsageEstimatesRefreshWorker(application.Pool, application.Clock))
	addWorker(workers, ledger.NewUncostedRerateWorker(ledger.RerateDependencies{
		Pool:             application.Pool,
		Pricing:          application.Pricing,
		Cache:            application.Cache,
		BeginCorrection:  application.beginCorrection,
		SettleCorrection: application.settleCorrection,
		Clock:            application.Clock,
		Logger:           application.Logger,
	}))
	periodicJobs := []*river.PeriodicJob{
		members.CleanupPeriodicJob(),
		decisions.ExpiryPeriodicJob(),
		decisions.ReconcilePeriodicJob(),
		ledger.UsageEstimatesRefreshPeriodicJob(),
		decisions.RetentionPeriodicJob(),
	}
	if application.Configuration.PricingLiteLLMRefresh {
		addWorker(workers, pricing.NewLiteLLMRefreshWorker(application.Pool, application.Cache, pricing.LiteLLMSnapshotURL, application.Clock, application.Logger))
		periodicJobs = append(periodicJobs, pricing.LiteLLMRefreshPeriodicJob())
	}
	return workers, periodicJobs, nil
}

// Middleware returns the middleware of the worker's job, with the job span
// outermost.
func (worker tracedWorker[Args]) Middleware(job *rivertype.JobRow) []rivertype.WorkerMiddleware {
	return append([]rivertype.WorkerMiddleware{jobSpan}, worker.Worker.Middleware(job)...)
}

// Work makes the counters ready with the bootstrap, so a counters_ready
// marker lost with a Redis restart is rebuilt within one reconcile interval,
// and then reconciles the counters.
func (worker reconcileWorker) Work(ctx context.Context, job *river.Job[decisions.ReconcileArgs]) error {
	if err := worker.bootstrap.EnsureCountersReady(ctx); err != nil {
		return err
	}
	return worker.ReconcileWorker.Work(ctx, job)
}

func addWorker[Args river.JobArgs](workers *river.Workers, worker river.Worker[Args]) {
	river.AddWorker[Args](workers, tracedWorker[Args]{Worker: worker})
}

func (application *App) beginCorrection(ctx context.Context, correction ledger.Entry) error {
	return application.Counters.BeginChange(ctx, correctionSettlement(correction), correction.CreatedAt)
}

func (application *App) settleCorrection(ctx context.Context, correction ledger.Entry) error {
	return application.Counters.SettleUnreserved(ctx, correctionSettlement(correction))
}

func correctionSettlement(correction ledger.Entry) decisions.Settlement {
	return decisions.Settlement{
		Environment: correction.Environment,
		CustomerID:  correction.CustomerID,
		PeriodStart: correction.PeriodStart,
		PeriodEnd:   correction.PeriodEnd,
		ChangeID:    correction.ID,
		Feature:     correction.Feature,
		Amount:      *correction.Rating.Cost,
	}
}
