package clock_test

import (
	"sync"
	"testing"
	"time"

	"github.com/preburn/preburn/internal/clock"
)

const concurrentAdvanceCount = 100

func TestSystemReturnsCurrentTimeInUTC(t *testing.T) {
	before := time.Now()
	now := clock.System{}.Now()
	after := time.Now()

	if now.Location() != time.UTC {
		t.Errorf("location = %v, want UTC", now.Location())
	}
	if now.Before(before) || now.After(after) {
		t.Errorf("now = %v, want between %v and %v", now, before, after)
	}
}

func TestManualMovesOnlyWhenTold(t *testing.T) {
	start := time.Date(2026, time.September, 26, 9, 30, 0, 0, time.FixedZone("plus five", 5*60*60))
	manual := clock.NewManual(start)

	assertNow(t, manual, start)
	assertNow(t, manual, start)

	manual.Advance(90 * time.Minute)
	assertNow(t, manual, start.Add(90*time.Minute))

	moment := time.Date(2025, time.January, 2, 3, 4, 5, 6, time.FixedZone("minus eight", -8*60*60))
	manual.Set(moment)
	assertNow(t, manual, moment)
}

func TestManualIsSafeForConcurrentUse(t *testing.T) {
	start := time.Date(2026, time.September, 26, 0, 0, 0, 0, time.UTC)
	manual := clock.NewManual(start)

	var group sync.WaitGroup
	for range concurrentAdvanceCount {
		group.Go(func() {
			manual.Advance(time.Second)
			manual.Now()
		})
	}
	group.Wait()

	assertNow(t, manual, start.Add(concurrentAdvanceCount*time.Second))
}

func assertNow(t *testing.T, source clock.Clock, want time.Time) {
	t.Helper()
	got := source.Now()
	if !got.Equal(want) {
		t.Errorf("now = %v, want %v", got, want)
	}
	if got.Location() != time.UTC {
		t.Errorf("location = %v, want UTC", got.Location())
	}
}
