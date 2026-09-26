package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/catalog"
	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/installation"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/metrics"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
	"github.com/preburn/preburn/internal/revenue"
)

const migrateApplicationName = "preburn-migrate"

// Role is the process an App runs in. Its value is the application_name of
// the process's database connections.
type Role string

const (
	// RoleAPI is the process that serves the API and the dashboard.
	RoleAPI Role = "preburn-api"
	// RoleWorker is the process that works River jobs.
	RoleWorker Role = "preburn-worker"
	// RoleAdmin is the process of a preburn admin command.
	RoleAdmin Role = "preburn-admin"
)

// App holds the dependencies that the routes, jobs and servers of a process
// share. Create one with New and release it with Close. Registration
// functions read the fields and never replace them.
type App struct {
	// Configuration is the validated environment configuration.
	Configuration config.Config
	// Logger writes the process's events.
	Logger *logging.Logger
	// Pool is the Postgres connection pool.
	Pool *pgxpool.Pool
	// Cache is the Redis client for every command except blocking stream
	// reads.
	Cache *cache.Client
	// Streams is the Redis client for the blocking stream reads of dashboard
	// streams, with a connection pool of its own.
	Streams *cache.Client
	// Jobs is the River client that inserts jobs and works none.
	Jobs *river.Client[pgx.Tx]
	// Registry holds the process's Prometheus metrics.
	Registry *prometheus.Registry
	// Clock tells the current time.
	Clock clock.Clock
	// Members manages members, their sessions and their one-time links.
	Members *members.Service
	// APIKeys manages API keys and holds the Authenticator of their bearer
	// tokens, whose cache the api process keeps subscribed to invalidations.
	APIKeys *apikeys.Service
	// Installation reads and completes the first-run setup.
	Installation *installation.Service
	// Settings reads and changes the installation name and the default plan
	// and Stripe metadata key of each environment.
	Settings *installation.SettingsService
	// Customers manages customers and customer users, and holds the cache
	// of customers by external id, which the api process keeps subscribed to
	// invalidations.
	Customers *customers.Service
	// Plans manages the plans of each environment.
	Plans *plans.Service
	// Pricing prices requests, lists the catalog and manages overrides, and
	// holds the rule set cache, which the api process keeps subscribed to
	// invalidations.
	Pricing *pricing.Service
	// CustomerStates caches the plan, period and net revenue of customers
	// for checks, reports and revenue records. The api process keeps it
	// subscribed to invalidations.
	CustomerStates *customerstate.Cache
	// Policies manages and previews policies, and holds the cache of active
	// policies, which the api process keeps subscribed to invalidations.
	Policies *policies.Service
	// Revenue records and lists revenue entries.
	Revenue *revenue.Service
	// Counters keeps the Redis period counters and reservations.
	Counters *decisions.Counters
	// CounterBootstrap rebuilds the counters from Postgres when the
	// counters_ready marker is missing. The api process serves its rebuild
	// requests.
	CounterBootstrap *decisions.CounterBootstrap
	// Checks decides checks and reserves their cost.
	Checks *decisions.CheckService
	// Reports stores usage reports in the ledger and releases decisions.
	Reports *decisions.ReportService
	// DecisionStream relays the decision stream to the dashboard and checks
	// the member session of each open stream at every heartbeat. The api
	// process stops it when the server shuts down.
	DecisionStream *dashboard.StreamService

	closers []func() error
}

// New loads the embedded catalog files, opens the Postgres pool with role as
// its application name, the Redis client and the stream client, and creates
// the River insert client, the metrics registry, the system clock and the
// domain services, whose metrics it registers. It then loads the counter
// Lua scripts. When a step fails it closes what it opened and returns the
// error.
func New(ctx context.Context, configuration config.Config, role Role, logger *logging.Logger) (*App, error) {
	files, err := catalogfiles.Load(catalog.Files)
	if err != nil {
		return nil, fmt.Errorf("load catalog files: %w", err)
	}
	application := &App{
		Configuration: configuration,
		Logger:        logger,
		Registry:      metrics.NewRegistry(),
		Clock:         clock.System{},
	}
	if err := application.open(ctx, role); err != nil {
		return nil, errors.Join(err, application.Close())
	}
	if err := application.createServices(files); err != nil {
		return nil, errors.Join(err, application.Close())
	}
	if err := application.Cache.LoadScripts(ctx, application.Counters.Scripts()...); err != nil {
		return nil, errors.Join(err, application.Close())
	}
	return application, nil
}

// Migrate applies the pending schema migrations with database.Migrate over a
// pool of its own whose connections are named preburn-migrate, then imports
// the embedded pricing catalog: the curated files with pricing.ImportCurated
// and the vendored LiteLLM snapshot with pricing.ImportLiteLLM. The vendored
// snapshot is skipped when the database already holds it or a newer one,
// such as a download of the pricing_litellm_refresh job. It closes the pool
// and needs no Redis. Running it again imports only what changed.
func Migrate(ctx context.Context, configuration config.Config, logger *logging.Logger) error {
	files, err := catalogfiles.Load(catalog.Files)
	if err != nil {
		return fmt.Errorf("load catalog files: %w", err)
	}
	snapshot, err := pricing.VendoredLiteLLMSnapshot(catalog.Files)
	if err != nil {
		return err
	}
	pool, err := database.Open(ctx, configuration, migrateApplicationName)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool, logger); err != nil {
		return err
	}
	timeSource := clock.System{}
	if _, err := pricing.ImportCurated(ctx, pool, files, timeSource, logger); err != nil {
		return fmt.Errorf("import curated catalog: %w", err)
	}
	if _, err := pricing.ImportLiteLLM(ctx, pool, snapshot, timeSource, logger); err != nil {
		return fmt.Errorf("import litellm snapshot: %w", err)
	}
	return nil
}

// Close closes the stream client, the Redis client and the Postgres pool, in
// the reverse order New opened them, and returns every error.
func (application *App) Close() error {
	var failures []error
	for index := len(application.closers) - 1; index >= 0; index-- {
		failures = append(failures, application.closers[index]())
	}
	return errors.Join(failures...)
}

func (application *App) open(ctx context.Context, role Role) error {
	pool, err := database.Open(ctx, application.Configuration, string(role))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	application.Pool = pool
	application.closers = append(application.closers, func() error {
		pool.Close()
		return nil
	})
	cacheClient, err := cache.Open(ctx, application.Configuration, application.Logger)
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	application.Cache = cacheClient
	application.closers = append(application.closers, cacheClient.Close)
	streamClient, err := cache.OpenStreamClient(ctx, application.Configuration, application.Logger)
	if err != nil {
		return fmt.Errorf("open stream cache: %w", err)
	}
	application.Streams = streamClient
	application.closers = append(application.closers, streamClient.Close)
	insertClient, err := jobs.NewInsertClient(pool, application.Logger)
	if err != nil {
		return err
	}
	application.Jobs = insertClient
	return nil
}

func (application *App) createServices(files catalogfiles.Catalog) error {
	application.Members = members.NewService(application.Pool, application.Cache, application.Clock, application.Configuration)
	application.APIKeys = apikeys.NewService(application.Pool, application.Cache, application.Clock, application.Logger)
	application.Installation = installation.NewService(application.Pool, application.Members, application.Clock, application.Configuration)
	application.Settings = installation.NewSettingsService(application.Pool, application.Cache, application.Clock)
	application.Customers = customers.NewService(application.Pool, application.Cache, application.Clock)
	application.Plans = plans.NewService(application.Pool, application.Cache, application.Clock)
	application.Pricing = pricing.NewService(application.Pool, application.Cache, application.Jobs, files, application.Clock)
	application.CustomerStates = customerstate.NewCache(customerstate.NewLoader(application.Pool), application.Clock)
	application.Policies = policies.NewService(application.Pool, application.Cache, files, application.Clock)
	application.Revenue = revenue.NewService(application.Pool, application.Customers.Cache(), application.CustomerStates, application.Cache, application.Clock)
	application.Counters = decisions.NewCounters(application.Cache)
	application.DecisionStream = dashboard.NewStreamService(application.Streams, members.NewSessionAuthenticator(application.Members.Sessions()), application.Logger, dashboard.StreamHeartbeatInterval)
	counterBootstrap, err := decisions.NewCounterBootstrap(application.Pool, application.Counters, application.Jobs, application.Clock, application.Logger, application.Registry)
	if err != nil {
		return err
	}
	application.CounterBootstrap = counterBootstrap
	checks, err := decisions.NewCheckService(decisions.CheckDependencies{
		Pool:           application.Pool,
		Jobs:           application.Jobs,
		Customers:      application.Customers,
		CustomerStates: application.CustomerStates,
		RuleSets:       application.Pricing.RuleSets(),
		Policies:       application.Policies,
		ModelAliases:   files.Aliases,
		Counters:       application.Counters,
		Bootstrap:      counterBootstrap,
		Stream:         decisions.NewStreamWriter(application.Cache, application.Logger),
		Clock:          application.Clock,
		Logger:         application.Logger,
		Registry:       application.Registry,
	})
	if err != nil {
		return err
	}
	application.Checks = checks
	reports, err := decisions.NewReportService(decisions.ReportDependencies{
		Pool:           application.Pool,
		Jobs:           application.Jobs,
		Customers:      application.Customers,
		CustomerStates: application.CustomerStates,
		RuleSets:       application.Pricing.RuleSets(),
		Policies:       application.Policies,
		ModelAliases:   files.Aliases,
		Counters:       application.Counters,
		Clock:          application.Clock,
		Logger:         application.Logger,
		Registry:       application.Registry,
	})
	if err != nil {
		return err
	}
	application.Reports = reports
	return nil
}
