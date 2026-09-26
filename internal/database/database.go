package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/config"
)

// Open returns a pool on configuration.DatabaseURL that holds at most
// configuration.DatabaseMaximumConnections connections. Every connection sets
// the runtime parameters timezone to UTC and application_name to
// applicationName, replacing URL query parameters of the same name in any
// letter case. Open pings
// the database, so an unreachable server or a missing database fails here
// rather than on first use.
func Open(ctx context.Context, configuration config.Config, applicationName string) (*pgxpool.Pool, error) {
	poolConfiguration, err := pgxpool.ParseConfig(configuration.DatabaseURL.String())
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	poolConfiguration.MaxConns = configuration.DatabaseMaximumConnections
	for name := range poolConfiguration.ConnConfig.RuntimeParams {
		if strings.EqualFold(name, "timezone") || strings.EqualFold(name, "application_name") {
			delete(poolConfiguration.ConnConfig.RuntimeParams, name)
		}
	}
	poolConfiguration.ConnConfig.RuntimeParams["timezone"] = "UTC"
	poolConfiguration.ConnConfig.RuntimeParams["application_name"] = applicationName
	pool, err := pgxpool.NewWithConfig(ctx, poolConfiguration)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

// InTransaction runs work in a transaction on a connection from pool. It
// commits when work returns nil. When work returns an error it rolls back and
// returns that error unchanged, so callers can match it with errors.Is and
// errors.As.
func InTransaction(ctx context.Context, pool *pgxpool.Pool, work func(ctx context.Context, transaction pgx.Tx) error) error {
	transaction, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()
	if err := work(ctx, transaction); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
