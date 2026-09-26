package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/preburn/preburn/db"
	"github.com/preburn/preburn/internal/logging"
)

const (
	migrationsDirectory = "migrations"
	migrationsLockName  = "migrations"
)

// Migrate applies every pending goose migration embedded in db.Migrations,
// then every pending River migration. It holds the advisory lock migrations
// for the whole run, so concurrent runs against one database serialize and
// the later ones apply nothing. It needs one connection from pool at a time.
// When it finishes it logs database.migrations_applied with the goose and
// River versions it applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool, logger *logging.Logger) error {
	release, err := AcquireAdvisoryLock(ctx, pool, migrationsLockName)
	if err != nil {
		return err
	}
	defer release()
	versions, err := applyGooseMigrations(ctx, pool)
	if err != nil {
		return err
	}
	riverVersions, err := applyRiverMigrations(ctx, pool, logger)
	if err != nil {
		return err
	}
	logger.Info(ctx, logging.DatabaseMigrationsApplied, slog.Any("versions", versions), slog.Any("river_versions", riverVersions))
	return nil
}

func applyGooseMigrations(ctx context.Context, pool *pgxpool.Pool) (versions []int64, err error) {
	files, err := fs.Sub(db.Migrations, migrationsDirectory)
	if err != nil {
		return nil, fmt.Errorf("open embedded migrations: %w", err)
	}
	migrationsDatabase := stdlib.OpenDBFromPool(pool)
	defer func() {
		err = errors.Join(err, migrationsDatabase.Close())
	}()
	provider, err := goose.NewProvider(goose.DialectPostgres, migrationsDatabase, files)
	if err != nil {
		return nil, fmt.Errorf("create goose provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("apply goose migrations: %w", err)
	}
	versions = make([]int64, 0, len(results))
	for _, result := range results {
		versions = append(versions, result.Source.Version)
	}
	return versions, nil
}

func applyRiverMigrations(ctx context.Context, pool *pgxpool.Pool, logger *logging.Logger) ([]int, error) {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Logger: logger.Library("river")})
	if err != nil {
		return nil, fmt.Errorf("create river migrator: %w", err)
	}
	result, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return nil, fmt.Errorf("apply river migrations: %w", err)
	}
	versions := make([]int, 0, len(result.Versions))
	for _, applied := range result.Versions {
		versions = append(versions, applied.Version)
	}
	return versions, nil
}
