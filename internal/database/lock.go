package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AcquireAdvisoryLock blocks until it holds the Postgres session advisory lock
// pg_advisory_lock(hashtext(name)) in the database pool connects to. The lock
// lives on a dedicated connection opened with the settings of pool but outside
// its connection limit, so waiting never takes a connection other work needs.
// The returned release function closes that connection, which ends the
// session and releases the lock. When ctx ends before the lock is granted,
// AcquireAdvisoryLock closes the connection and returns an error.
func AcquireAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, name string) (func(), error) {
	connection, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig)
	if err != nil {
		return nil, fmt.Errorf("connect for advisory lock %s: %w", name, err)
	}
	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", name); err != nil {
		return nil, errors.Join(fmt.Errorf("acquire advisory lock %s: %w", name, err), connection.Close(context.WithoutCancel(ctx)))
	}
	release := func() {
		_ = connection.Close(context.WithoutCancel(ctx))
	}
	return release, nil
}
