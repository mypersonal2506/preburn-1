package database_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/database/databasetest"
)

const openTestApplicationName = "preburn-open-test"

func TestOpenSetsSessionParameters(t *testing.T) {
	ctx := t.Context()
	databaseURL := poolURL(t, databasetest.NewPool(t))
	query := databaseURL.Query()
	query.Set("TimeZone", "Asia/Kolkata")
	query.Set("Application_Name", "preburn-from-url")
	databaseURL.RawQuery = query.Encode()

	pool, err := database.Open(ctx, config.Config{DatabaseURL: databaseURL, DatabaseMaximumConnections: 3}, openTestApplicationName)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	var timeZone, applicationName string
	err = pool.QueryRow(ctx, "SELECT current_setting('TimeZone'), current_setting('application_name')").Scan(&timeZone, &applicationName)
	if err != nil {
		t.Fatalf("read session settings: %v", err)
	}
	if timeZone != "UTC" {
		t.Errorf("TimeZone=%q, want UTC", timeZone)
	}
	if applicationName != openTestApplicationName {
		t.Errorf("application_name=%q, want %q", applicationName, openTestApplicationName)
	}
	wantParameters := map[string]string{"timezone": "UTC", "application_name": openTestApplicationName}
	if diff := cmp.Diff(wantParameters, pool.Config().ConnConfig.RuntimeParams); diff != "" {
		t.Errorf("runtime parameters mismatch (-want +got):\n%s", diff)
	}
	if maximum := pool.Config().MaxConns; maximum != 3 {
		t.Errorf("MaxConns=%d, want 3", maximum)
	}
}

func TestOpenFailsForMissingDatabase(t *testing.T) {
	databaseURL := poolURL(t, databasetest.NewPool(t))
	databaseURL.Path = "/preburn_missing_database"

	pool, err := database.Open(t.Context(), config.Config{DatabaseURL: databaseURL, DatabaseMaximumConnections: 1}, openTestApplicationName)
	if err == nil {
		pool.Close()
		t.Fatal("Open returned no error for a missing database")
	}
}

func TestInTransaction(t *testing.T) {
	workFailed := errors.New("work failed")
	tests := []struct {
		name      string
		workError error
		wantName  string
	}{
		{name: "commits when work returns nil", workError: nil, wantName: "Renamed"},
		{name: "rolls back when work returns an error", workError: workFailed, wantName: "Preburn"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			pool := databasetest.NewPool(t)

			err := database.InTransaction(ctx, pool, func(ctx context.Context, transaction pgx.Tx) error {
				if _, err := transaction.Exec(ctx, "UPDATE installation SET name = 'Renamed' WHERE installation_id = 1"); err != nil {
					return err
				}
				return test.workError
			})

			if !errors.Is(err, test.workError) {
				t.Errorf("InTransaction error=%v, want %v", err, test.workError)
			}
			var name string
			if err := pool.QueryRow(ctx, "SELECT name FROM installation WHERE installation_id = 1").Scan(&name); err != nil {
				t.Fatalf("read installation name: %v", err)
			}
			if name != test.wantName {
				t.Errorf("installation name=%q, want %q", name, test.wantName)
			}
		})
	}
}

func poolURL(t *testing.T, pool *pgxpool.Pool) *url.URL {
	t.Helper()
	parsed, err := url.Parse(pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse pool connection string: %v", err)
	}
	return parsed
}
