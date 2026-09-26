package databasetest_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/database/databasetest"
)

const maximumPoolConnections = 4

func TestNewPoolReturnsSeparateMigratedDatabases(t *testing.T) {
	ctx := t.Context()
	first := databasetest.NewPool(t)
	second := databasetest.NewPool(t)

	firstName := currentDatabase(t, first)
	if secondName := currentDatabase(t, second); firstName == secondName {
		t.Fatalf("both pools use database %s", firstName)
	}
	if _, err := first.Exec(ctx, "UPDATE installation SET name = 'Changed' WHERE installation_id = 1"); err != nil {
		t.Fatalf("update installation in the first database: %v", err)
	}
	var secondInstallationName string
	if err := second.QueryRow(ctx, "SELECT name FROM installation WHERE installation_id = 1").Scan(&secondInstallationName); err != nil {
		t.Fatalf("read installation in the second database: %v", err)
	}
	if secondInstallationName != "Preburn" {
		t.Errorf("second database installation name=%q, want Preburn", secondInstallationName)
	}
	for _, pool := range []*pgxpool.Pool{first, second} {
		if maximum := pool.Config().MaxConns; maximum != maximumPoolConnections {
			t.Errorf("MaxConns=%d, want %d", maximum, maximumPoolConnections)
		}
	}
}

func TestNewEmptyPoolReturnsEmptyDatabase(t *testing.T) {
	pool := databasetest.NewEmptyPool(t)

	var tableCount int
	err := pool.QueryRow(t.Context(), "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'").Scan(&tableCount)
	if err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tableCount != 0 {
		t.Errorf("empty database has %d tables in public, want 0", tableCount)
	}
}

func TestDatabasesAreDroppedWhenTheirTestEnds(t *testing.T) {
	var names []string
	t.Run("migrated", func(t *testing.T) {
		names = append(names, currentDatabase(t, databasetest.NewPool(t)))
	})
	t.Run("empty", func(t *testing.T) {
		names = append(names, currentDatabase(t, databasetest.NewEmptyPool(t)))
	})

	observer := databasetest.NewEmptyPool(t)
	for _, name := range names {
		var exists bool
		if err := observer.QueryRow(t.Context(), "SELECT EXISTS (SELECT FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
			t.Fatalf("look up database %s: %v", name, err)
		}
		if exists {
			t.Errorf("database %s still exists after its test ended", name)
		}
	}
}

func currentDatabase(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var name string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("read current database: %v", err)
	}
	return name
}
