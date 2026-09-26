package cache_test

import (
	"testing"
	"time"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
)

const localTimeToLive = time.Minute

func TestLocalCacheExpiresEntriesAfterTimeToLive(t *testing.T) {
	manual := clock.NewManual(time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC))
	local := cache.NewLocalCache[string, int](manual, localTimeToLive, 10)

	local.Set("plan", 1)
	manual.Advance(localTimeToLive - time.Nanosecond)
	assertCached(t, local, "plan", 1)

	manual.Advance(time.Nanosecond)
	assertNotCached(t, local, "plan")
}

func TestLocalCacheSetRestartsTimeToLive(t *testing.T) {
	manual := clock.NewManual(time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC))
	local := cache.NewLocalCache[string, int](manual, localTimeToLive, 10)

	local.Set("plan", 1)
	manual.Advance(localTimeToLive / 2)
	local.Set("plan", 2)
	manual.Advance(localTimeToLive - time.Nanosecond)
	assertCached(t, local, "plan", 2)

	manual.Advance(time.Nanosecond)
	assertNotCached(t, local, "plan")
}

func TestLocalCacheEvictsLeastRecentlyUsedAtMaximumSize(t *testing.T) {
	manual := clock.NewManual(time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC))
	local := cache.NewLocalCache[string, int](manual, localTimeToLive, 3)

	local.Set("first", 1)
	local.Set("second", 2)
	local.Set("third", 3)
	assertCached(t, local, "first", 1)
	local.Set("fourth", 4)

	assertNotCached(t, local, "second")
	assertCached(t, local, "first", 1)
	assertCached(t, local, "third", 3)
	assertCached(t, local, "fourth", 4)

	local.Set("third", 30)
	local.Set("fifth", 5)

	assertNotCached(t, local, "first")
	assertCached(t, local, "third", 30)
	assertCached(t, local, "fourth", 4)
	assertCached(t, local, "fifth", 5)
}

func TestLocalCacheDeleteAndClear(t *testing.T) {
	manual := clock.NewManual(time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC))
	local := cache.NewLocalCache[string, int](manual, localTimeToLive, 10)

	local.Set("plan", 1)
	local.Set("pricing", 2)
	local.Set("settings", 3)
	local.Delete("plan")
	local.Delete("missing")

	assertNotCached(t, local, "plan")
	assertCached(t, local, "pricing", 2)

	local.Clear()

	assertNotCached(t, local, "pricing")
	assertNotCached(t, local, "settings")

	local.Set("plan", 4)
	assertCached(t, local, "plan", 4)
}

func TestNewLocalCacheRejectsInvalidLimits(t *testing.T) {
	tests := []struct {
		name           string
		timeToLive     time.Duration
		maximumEntries int
	}{
		{name: "zero time to live", timeToLive: 0, maximumEntries: 10},
		{name: "negative time to live", timeToLive: -time.Second, maximumEntries: 10},
		{name: "zero maximum entries", timeToLive: time.Minute, maximumEntries: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("NewLocalCache(%s, %d) did not panic", test.timeToLive, test.maximumEntries)
				}
			}()
			cache.NewLocalCache[string, int](clock.System{}, test.timeToLive, test.maximumEntries)
		})
	}
}

func assertCached(t *testing.T, local *cache.LocalCache[string, int], key string, want int) {
	t.Helper()
	got, found := local.Get(key)
	if !found {
		t.Fatalf("Get(%q) found nothing, want %d", key, want)
	}
	if got != want {
		t.Errorf("Get(%q) = %d, want %d", key, got, want)
	}
}

func assertNotCached(t *testing.T, local *cache.LocalCache[string, int], key string) {
	t.Helper()
	if got, found := local.Get(key); found {
		t.Errorf("Get(%q) = %d, want nothing", key, got)
	}
}
