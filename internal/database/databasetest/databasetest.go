package databasetest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/preburn/preburn/db"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/logging"
)

const (
	serverURLVariable      = "PREBURN_TEST_DATABASE_URL"
	applicationName        = "preburn-test"
	maximumPoolConnections = 4
	serverPoolConnections  = 1
	testDatabasePrefix     = "test_"
	templateDatabasePrefix = "preburn_template_"
	templateHashLength     = 16
	templateLockName       = "databasetest_template"
	emptyTemplate          = "template0"
	migrationFilePattern   = "migrations/*.sql"
)

var migratedTemplate = sync.OnceValues(prepareTemplate)

// NewPool creates a database named test_<random> as a copy of the migrated
// template and returns a pool of at most 4 connections on it. The first call
// in a test binary creates the template when no earlier run did. The template
// is named after a hash of the goose and River migrations and is built under
// an advisory lock, so parallel test binaries build it once. A cleanup
// registered on t closes the pool and drops the database.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	template, err := migratedTemplate()
	if err != nil {
		t.Fatalf("prepare template database: %v", err)
	}
	return newDatabase(t, template)
}

// NewEmptyPool creates an empty database named test_<random>, with no
// migrations applied, and returns a pool of at most 4 connections on it. A
// cleanup registered on t closes the pool and drops the database.
func NewEmptyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newDatabase(t, emptyTemplate)
}

func newDatabase(t *testing.T, template string) *pgxpool.Pool {
	t.Helper()
	serverURL, err := testServerURL()
	if err != nil {
		t.Fatal(err)
	}
	name := testDatabasePrefix + strings.ToLower(rand.Text())
	if err := executeOnServer(t.Context(), serverURL, createDatabaseStatement(name, template)); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		if err := executeOnServer(context.Background(), serverURL, dropDatabaseStatement(name)); err != nil {
			t.Errorf("drop test database %s: %v", name, err)
		}
	})
	pool, err := openPool(t.Context(), databaseURL(serverURL, name), maximumPoolConnections)
	if err != nil {
		t.Fatalf("open test database %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func prepareTemplate() (string, error) {
	ctx := context.Background()
	serverURL, err := testServerURL()
	if err != nil {
		return "", err
	}
	name, err := templateName()
	if err != nil {
		return "", err
	}
	serverPool, err := openPool(ctx, serverURL, serverPoolConnections)
	if err != nil {
		return "", fmt.Errorf("open test server: %w", err)
	}
	defer serverPool.Close()
	release, err := database.AcquireAdvisoryLock(ctx, serverPool, templateLockName)
	if err != nil {
		return "", err
	}
	defer release()

	var complete bool
	switch err := serverPool.QueryRow(ctx, "SELECT datistemplate FROM pg_database WHERE datname = $1", name).Scan(&complete); {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return "", fmt.Errorf("look up template database %s: %w", name, err)
	case complete:
		return name, nil
	default:
		if _, err := serverPool.Exec(ctx, dropDatabaseStatement(name)); err != nil {
			return "", fmt.Errorf("drop incomplete template database %s: %w", name, err)
		}
	}
	if _, err := serverPool.Exec(ctx, createDatabaseStatement(name, emptyTemplate)); err != nil {
		return "", fmt.Errorf("create template database %s: %w", name, err)
	}
	if err := migrateTemplate(ctx, databaseURL(serverURL, name)); err != nil {
		return "", err
	}
	if _, err := serverPool.Exec(ctx, "ALTER DATABASE "+quotedIdentifier(name)+" WITH IS_TEMPLATE true ALLOW_CONNECTIONS false"); err != nil {
		return "", fmt.Errorf("mark template database %s: %w", name, err)
	}
	return name, nil
}

func migrateTemplate(ctx context.Context, templateURL *url.URL) error {
	pool, err := openPool(ctx, templateURL, maximumPoolConnections)
	if err != nil {
		return fmt.Errorf("open template database: %w", err)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool, logging.New(io.Discard, slog.LevelInfo)); err != nil {
		return fmt.Errorf("migrate template database: %w", err)
	}
	return nil
}

func templateName() (string, error) {
	hash := sha256.New()
	files, err := fs.Glob(db.Migrations, migrationFilePattern)
	if err != nil {
		return "", fmt.Errorf("list embedded migrations: %w", err)
	}
	for _, file := range files {
		contents, err := db.Migrations.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read embedded migration %s: %w", file, err)
		}
		hash.Write(fmt.Appendf(nil, "%s %d\n", file, len(contents)))
		hash.Write(contents)
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(nil), nil)
	if err != nil {
		return "", fmt.Errorf("create river migrator: %w", err)
	}
	for _, migration := range migrator.AllVersions() {
		hash.Write(fmt.Appendf(nil, "river %d %d\n", migration.Version, len(migration.SQLUp)))
		hash.Write([]byte(migration.SQLUp))
	}
	return templateDatabasePrefix + hex.EncodeToString(hash.Sum(nil))[:templateHashLength], nil
}

func executeOnServer(ctx context.Context, serverURL *url.URL, statement string) error {
	serverPool, err := openPool(ctx, serverURL, serverPoolConnections)
	if err != nil {
		return fmt.Errorf("open test server: %w", err)
	}
	defer serverPool.Close()
	if _, err := serverPool.Exec(ctx, statement); err != nil {
		return fmt.Errorf("execute %s: %w", statement, err)
	}
	return nil
}

func openPool(ctx context.Context, databaseURL *url.URL, connections int32) (*pgxpool.Pool, error) {
	return database.Open(ctx, config.Config{DatabaseURL: databaseURL, DatabaseMaximumConnections: connections}, applicationName)
}

func testServerURL() (*url.URL, error) {
	value := os.Getenv(serverURLVariable)
	if value == "" {
		return nil, fmt.Errorf("%s is not set, run the tests with make test-go", serverURLVariable)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be a Postgres URL", serverURLVariable)
	}
	return parsed, nil
}

func databaseURL(serverURL *url.URL, name string) *url.URL {
	withDatabase := *serverURL
	withDatabase.Path = "/" + name
	withDatabase.RawPath = ""
	return &withDatabase
}

func createDatabaseStatement(name, template string) string {
	return "CREATE DATABASE " + quotedIdentifier(name) + " TEMPLATE " + quotedIdentifier(template)
}

func dropDatabaseStatement(name string) string {
	return "DROP DATABASE " + quotedIdentifier(name) + " WITH (FORCE)"
}

func quotedIdentifier(name string) string {
	return pgx.Identifier{name}.Sanitize()
}
