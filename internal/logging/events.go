package logging

// Event is the name of a logged event and the message of its log line. Every
// event is a constant in this file with a lowercase dotted value, such as
// decisions.settle_deferred, in one const block per emitting package.
type Event string

// Events of package logging.
const (
	// LoggingLibraryMessage is logged for a warning or error that a
	// third-party library such as River logs through Logger.Library. The
	// library attribute names it and message holds its original text.
	LoggingLibraryMessage Event = "logging.library_message"
)

// Events of package cache.
const (
	// CacheInvalidationResubscribed is logged when the invalidation subscriber
	// subscribes again after losing its connection. Invalidations published
	// while it was disconnected are lost.
	CacheInvalidationResubscribed Event = "cache.invalidation_resubscribed"
	// CacheInvalidationMalformed is logged when a message on the invalidate
	// channel is not a valid invalidation. The subscriber skips it.
	CacheInvalidationMalformed Event = "cache.invalidation_malformed"
)

// Events of package tracing.
const (
	// TracingSDKError is logged when the OpenTelemetry SDK reports an error,
	// such as a failed span export. The error attribute holds its text.
	TracingSDKError Event = "tracing.sdk_error"
)

// Events of package database.
const (
	// DatabaseMigrationsApplied is logged when a migration run finishes. The
	// versions attribute lists the goose versions it applied and
	// river_versions the River versions, both empty when the schema was
	// current.
	DatabaseMigrationsApplied Event = "database.migrations_applied"
)

// Events of package httpapi.
const (
	// HTTPRequestCompleted is logged when the server finishes a request, with
	// the method, the matched route_pattern, the status and duration_seconds.
	HTTPRequestCompleted Event = "http.request_completed"
	// HTTPInternalError is logged when a handler or authenticator returns an
	// error that is not a CodedError. The client gets an internal_error
	// problem and the error attribute holds the error text.
	HTTPInternalError Event = "http.internal_error"
	// HTTPRequestCanceled is logged at info level instead of
	// HTTPInternalError when a handler or authenticator returns any error
	// after the client closed the request, such as an SDK check past its
	// timeout or a page reload, with the error. The answer is 499
	// client_closed_request, which no client receives.
	HTTPRequestCanceled Event = "http.request_canceled"
	// HTTPPanicRecovered is logged when a handler panics. The client gets an
	// internal_error problem, the panic and stack attributes hold the panic
	// value and the goroutine stack.
	HTTPPanicRecovered Event = "http.panic_recovered"
	// HTTPReadinessCheckFailed is logged when a readiness check fails, with
	// the check name and the error. /readyz answers 503.
	HTTPReadinessCheckFailed Event = "http.readiness_check_failed"
	// HTTPResponseWriteFailed is logged when writing a JSON response body
	// fails, usually because the client closed the connection.
	HTTPResponseWriteFailed Event = "http.response_write_failed"
)

// Events of package jobs.
const (
	// JobsFailed is logged when a job attempt returns an error or panics,
	// with the job_id, kind, attempt and error. A panic adds its stack. River
	// retries the job until its attempts run out. A worker that returns
	// river.JobCancel ends its job without this event.
	JobsFailed Event = "jobs.failed"
	// JobsQueueSampleFailed is logged when counting the available jobs per
	// queue for preburn_river_queue_available fails, with the error. The
	// gauge keeps its previous values until the next sample.
	JobsQueueSampleFailed Event = "jobs.queue_sample_failed"
)

// Events of package app.
const (
	// ServerStarted is logged when the api process opens its listener, with
	// the listen address and the version.
	ServerStarted Event = "server.started"
	// ServerStopped is logged when the api process has finished its open
	// requests after the shutdown signal.
	ServerStopped Event = "server.stopped"
	// WorkerStarted is logged when the worker process starts working jobs.
	WorkerStarted Event = "worker.started"
	// WorkerStopped is logged when the worker process has stopped working
	// jobs after the shutdown signal.
	WorkerStopped Event = "worker.stopped"
	// InstallationSetupLinkCreated is logged at warn level when the api
	// process starts while setup is pending, with the url attribute holding
	// the new setup link. Only the latest link completes setup.
	InstallationSetupLinkCreated Event = "installation.setup_link_created"
)

// Events of package apikeys.
const (
	// APIKeysLastUsedUpdateFailed is logged at warn level when writing the
	// last use of an API key fails, with the key_id and the error. The request
	// goes on, and the next write is due a minute later.
	APIKeysLastUsedUpdateFailed Event = "apikeys.last_used_update_failed"
)

// Events of package members.
const (
	// MembersCleanupCompleted is logged when a member_cleanup job finishes,
	// with deleted_sessions counting the expired sessions it deleted and
	// deleted_links the member links that expired or were consumed more than
	// 7 days ago.
	MembersCleanupCompleted Event = "members.cleanup_completed"
)

// Events of package pricing.
const (
	// PricingCatalogImported is logged when a catalog import commits, with the
	// source it imported (curated or litellm) and the counts created, changed,
	// updated, deprecated, unchanged and aliases.
	PricingCatalogImported Event = "pricing.catalog_imported"
	// PricingCatalogImportSkipped is logged when a LiteLLM import writes
	// nothing, because the database holds rules from a snapshot fetched at
	// or after its own, with the source it skipped.
	PricingCatalogImportSkipped Event = "pricing.catalog_import_skipped"
)

// Events of package ledger.
const (
	// LedgerUncostedRerateCompleted is logged when an uncosted_rerate job
	// finishes, with the environment and the number of corrections it
	// inserted.
	LedgerUncostedRerateCompleted Event = "ledger.uncosted_rerate_completed"
	// LedgerRerateSettleDeferred is logged at warn level when the cost of a
	// committed correction entry cannot be added to its Redis counter, with
	// the environment, the ledger_entry_id of the correction and the error.
	// counters_reconcile repairs the counter.
	LedgerRerateSettleDeferred Event = "ledger.rerate_settle_deferred"
)

// Events of package decisions.
const (
	// DecisionsCountersUnavailable is logged when a check cannot read or
	// reserve its counter in Redis within the check deadline, or finds the
	// counters_ready marker missing, with the error. The client gets 503
	// counters_unavailable.
	DecisionsCountersUnavailable Event = "decisions.counters_unavailable"
	// DecisionsCountersRebuildFailed is logged when a counter rebuild that a
	// check or a reconnected invalidation subscription requested fails, with
	// the error. /readyz keeps failing until the next request or
	// counters_reconcile run makes the counters ready.
	DecisionsCountersRebuildFailed Event = "decisions.counters_rebuild_failed"
	// DecisionsCounterChangeEndFailed is logged at warn level when a check
	// cannot end the pending change its reservation registered, with the
	// environment, the decision_id and the error. The check still answers,
	// and counters_reconcile skips the counter until the change is a minute
	// old.
	DecisionsCounterChangeEndFailed Event = "decisions.counter_change_end_failed"
	// DecisionsDatabaseUnavailable is logged when a check fails on a Postgres
	// read or write, with the error. The client gets 503 database_unavailable.
	DecisionsDatabaseUnavailable Event = "decisions.database_unavailable"
	// DecisionsStreamAppendFailed is logged at warn level when appending a
	// decision to the decision stream fails, with the environment, the
	// decision_id and the error. The check still answers.
	DecisionsStreamAppendFailed Event = "decisions.stream_append_failed"
	// DecisionsSettleDeferred is logged at warn level when a committed report
	// cannot move its usage into the Redis counter, because Redis failed, the
	// counters_ready marker was missing or its pending change was dropped,
	// with the environment, the ledger_entry_id and the error. The report
	// still answers 202 and counters_reconcile or the counter rebuild repairs
	// the counter.
	DecisionsSettleDeferred Event = "decisions.settle_deferred"
	// DecisionsReleaseDeferred is logged at warn level when a committed
	// release cannot free its reservation in Redis, with the environment, the
	// decision_id and the error. The release still answers 200 and
	// counters_reconcile or the expiry job frees the reservation.
	DecisionsReleaseDeferred Event = "decisions.release_deferred"
	// DecisionsDroppedReportsRecordFailed is logged at warn level when adding
	// a Preburn-Dropped-Reports count to the day's dropped report counter in
	// Redis fails, with the environment, the count and the error. The request
	// still answers.
	DecisionsDroppedReportsRecordFailed Event = "decisions.dropped_reports_record_failed"
	// DecisionsReportFailed is logged when one report of a batch fails with an
	// error that is not a CodedError while the request is live, with its index
	// and the error. Its result is a 500 internal_error. Once the client
	// closed the request, the batch stops and HTTPRequestCanceled is logged
	// instead.
	DecisionsReportFailed Event = "decisions.report_failed"
	// DecisionsRetentionCompleted is logged when a decision_retention job
	// finishes, with the number of decisions it deleted in deleted_decisions.
	DecisionsRetentionCompleted Event = "decisions.retention_completed"
)

// Events of package dashboard.
const (
	// DashboardStreamFailed is logged at warn level when a decision stream
	// ends because reading the Redis stream or an entry of it failed, with the
	// environment and the error. The browser reconnects with the id of the
	// last event it received.
	DashboardStreamFailed Event = "dashboard.stream_failed"
)
