package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/preburn/preburn/internal/app"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/metrics"
	"github.com/preburn/preburn/internal/tracing"
	"github.com/preburn/preburn/internal/version"
)

const (
	serviceName            = "preburn"
	redisLibraryName       = "go-redis"
	tracingShutdownTimeout = 10 * time.Second
)

type redisLogger struct {
	library *slog.Logger
}

func runProcess(command *cobra.Command, role app.Role, run func(ctx context.Context, application *app.App) error) (err error) {
	ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	configuration, err := loadConfiguration()
	if err != nil {
		return err
	}
	logger := logging.New(command.OutOrStdout(), configuration.LogLevel)
	redis.SetLogger(redisLogger{library: logger.Library(redisLibraryName)})
	shutdownTracing, err := tracing.Setup(ctx, logger, serviceName, version.Version)
	if errors.Is(err, tracing.ErrUnsupportedProtocol) {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if err != nil {
		return fmt.Errorf("set up tracing: %w", err)
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracingShutdownTimeout)
		defer cancel()
		err = errors.Join(err, shutdownTracing(shutdownContext))
	}()
	application, err := app.New(ctx, configuration, role, logger)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, application.Close())
	}()
	group, groupContext := errgroup.WithContext(ctx)
	group.Go(func() error {
		return metrics.Serve(groupContext, configuration.MetricsAddress, application.Registry)
	})
	group.Go(func() error {
		return run(groupContext, application)
	})
	return group.Wait()
}

func loadConfiguration() (config.Config, error) {
	configuration, err := config.Load(os.LookupEnv)
	if err != nil {
		return config.Config{}, fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	return configuration, nil
}

func (logger redisLogger) Printf(ctx context.Context, format string, arguments ...any) {
	logger.library.WarnContext(ctx, fmt.Sprintf(format, arguments...))
}
