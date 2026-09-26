package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/database/databasetest"
)

const (
	lockTestDeadline     = 30 * time.Second
	lockTestPollInterval = 10 * time.Millisecond
	lockTestName         = "pricing_import"
)

type lockAcquisition struct {
	release func()
	err     error
}

func TestAcquireAdvisoryLockWaitsForRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), lockTestDeadline)
	defer cancel()
	pool := databasetest.NewPool(t)

	releaseFirst, err := database.AcquireAdvisoryLock(ctx, pool, lockTestName)
	if err != nil {
		t.Fatalf("first AcquireAdvisoryLock: %v", err)
	}
	second := make(chan lockAcquisition, 1)
	go func() {
		release, err := database.AcquireAdvisoryLock(ctx, pool, lockTestName)
		second <- lockAcquisition{release: release, err: err}
	}()

	waitForBlockedAdvisoryLock(ctx, t, pool)
	select {
	case <-second:
		t.Fatal("second acquisition returned while the first held the lock")
	default:
	}
	releaseOther, err := database.AcquireAdvisoryLock(ctx, pool, "counters_rebuild")
	if err != nil {
		t.Fatalf("AcquireAdvisoryLock with another name: %v", err)
	}
	releaseOther()

	releaseFirst()
	select {
	case acquisition := <-second:
		if acquisition.err != nil {
			t.Fatalf("second AcquireAdvisoryLock: %v", acquisition.err)
		}
		acquisition.release()
	case <-ctx.Done():
		t.Fatal("second acquisition did not return after the first released the lock")
	}
}

func waitForBlockedAdvisoryLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ticker := time.NewTicker(lockTestPollInterval)
	defer ticker.Stop()
	for {
		var blocked bool
		err := pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT FROM pg_locks
			WHERE locktype = 'advisory'
				AND NOT granted
				AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
		)`).Scan(&blocked)
		if err != nil {
			t.Fatalf("read pg_locks: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("second acquisition never waited on the advisory lock")
		}
	}
}
