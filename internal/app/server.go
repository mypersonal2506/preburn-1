package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/metrics"
	"github.com/preburn/preburn/internal/tracing"
	"github.com/preburn/preburn/internal/version"
	"github.com/preburn/preburn/web"
)

const (
	apiPathPrefix         = "/api/"
	dashboardPath         = "/"
	readHeaderTimeout     = 10 * time.Second
	idleTimeout           = 2 * time.Minute
	shutdownTimeout       = 30 * time.Second
	httpServerLibraryName = "net/http"
)

var errScriptsNotLoaded = errors.New("redis scripts not loaded")

// Serve answers HTTP requests on listener until ctx ends: /healthz and
// /readyz, the API under /api/v1, a not_found problem on every other /api/
// path, and the dashboard on every other path. It first stores a new setup
// link while setup is pending and logs it at warn level as
// installation.setup_link_created with the url attribute. While it serves,
// it makes the counters ready with CounterBootstrap, which rebuilds them
// when the counters_ready marker is missing or waits for the process that
// does, and /readyz fails until they are. It also serves the rebuild
// requests of checks that find the marker missing, one at a time. While it
// runs, the caches of resolved API keys, customers, customer states, active
// policies and pricing rule sets follow the invalidation channel, so a key
// that any process revokes stops working here too. When the subscription
// reconnects each cache is emptied and a counter rebuild is requested,
// because a Redis restart loses both the invalidations and the counters. It
// logs
// server.started with the listener's address. When ctx ends it stops
// accepting connections, ends the open decision streams, waits up to 30
// seconds for open requests, logs server.stopped and returns nil. It returns
// an error when the setup link cannot be stored, the counters cannot be made
// ready, the server fails or open requests outlast the deadline. Serve
// registers the HTTP metrics on Registry, so it runs once per App.
func (application *App) Serve(ctx context.Context, listener net.Listener) error {
	if err := application.prepareSetupLink(ctx); err != nil {
		return err
	}
	stopSubscription := application.subscribeInvalidations(ctx)
	defer stopSubscription()
	server := &http.Server{
		Handler:           application.handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(application.Logger.Library(httpServerLibraryName).Handler(), slog.LevelWarn),
	}
	server.RegisterOnShutdown(application.DecisionStream.Stop)
	group, groupContext := errgroup.WithContext(ctx)
	group.Go(func() error {
		return application.serveUntilDone(groupContext, server, listener)
	})
	group.Go(func() error {
		if err := application.CounterBootstrap.EnsureCountersReady(groupContext); err != nil && groupContext.Err() == nil {
			return fmt.Errorf("ensure counters ready: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		application.CounterBootstrap.ServeRebuildRequests(groupContext)
		return nil
	})
	return group.Wait()
}

func (application *App) serveUntilDone(ctx context.Context, server *http.Server, listener net.Listener) error {
	served := make(chan error, 1)
	go func() {
		served <- server.Serve(listener)
	}()
	application.Logger.Info(ctx, logging.ServerStarted,
		slog.String("address", listener.Addr().String()),
		slog.String("version", version.Version),
	)
	select {
	case err := <-served:
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
	}
	shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down http server: %w", err)
	}
	application.Logger.Info(ctx, logging.ServerStopped)
	return nil
}

func (application *App) prepareSetupLink(ctx context.Context) error {
	link, pending, err := application.Installation.PrepareSetupLink(ctx)
	if err != nil {
		return err
	}
	if pending {
		application.Logger.Warn(ctx, logging.InstallationSetupLinkCreated, slog.String("url", link))
	}
	return nil
}

func (application *App) subscribeInvalidations(ctx context.Context) func() {
	subscriptionContext, cancel := context.WithCancel(ctx)
	invalidate, resubscribed := application.invalidationHandlers()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		application.Cache.SubscribeInvalidations(subscriptionContext, invalidate, resubscribed)
	}()
	return func() {
		cancel()
		<-stopped
	}
}

func (application *App) invalidationHandlers() (invalidate func(cache.Invalidation), resubscribed func()) {
	keyAuthenticator := application.APIKeys.Authenticator()
	customerCache := application.Customers.Cache()
	ruleSets := application.Pricing.RuleSets()
	customerStates := application.CustomerStates
	activePolicies := application.Policies.Cache()
	invalidate = func(invalidation cache.Invalidation) {
		keyAuthenticator.Invalidate(invalidation)
		customerCache.Invalidate(invalidation)
		ruleSets.Invalidate(invalidation)
		customerStates.Invalidate(invalidation)
		activePolicies.Invalidate(invalidation)
	}
	resubscribed = func() {
		keyAuthenticator.ClearCache()
		customerCache.Clear()
		ruleSets.Clear()
		customerStates.Clear()
		activePolicies.Clear()
		application.CounterBootstrap.RequestRebuild()
	}
	return invalidate, resubscribed
}

func (application *App) handler() http.Handler {
	mux := http.NewServeMux()
	api := newAPI(mux, application)
	httpapi.RegisterHealthRoutes(mux, application.Logger, application.readinessChecks())
	mux.Handle(apiPathPrefix, api.NotFoundHandler())
	mux.Handle(dashboardPath, web.Handler())
	httpMetrics := metrics.NewHTTP(application.Registry)
	return httpapi.RequestID(
		httpapi.SecurityHeaders(
			tracing.Middleware(
				httpMetrics.Middleware(
					httpapi.AccessLog(application.Logger,
						httpapi.Recover(application.Logger, mux))))))
}

func (application *App) readinessChecks() []httpapi.ReadinessCheck {
	return []httpapi.ReadinessCheck{
		{Name: "postgres", Check: application.Pool.Ping},
		{Name: "redis", Check: func(ctx context.Context) error {
			return application.Cache.Redis().Ping(ctx).Err()
		}},
		{Name: "redis_scripts", Check: func(context.Context) error {
			if !application.Cache.ScriptsLoaded() {
				return errScriptsNotLoaded
			}
			return nil
		}},
		decisions.CountersReadinessCheck(application.Counters),
	}
}
