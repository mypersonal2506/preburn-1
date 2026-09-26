package jobs_test

import (
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/jobs"
)

func TestDailyAtNext(t *testing.T) {
	fourHoursBehind := time.FixedZone("UTC-4", -4*60*60)
	tests := []struct {
		name     string
		schedule river.PeriodicSchedule
		current  time.Time
		want     time.Time
	}{
		{
			name:     "before the time of day",
			schedule: jobs.DailyAt(3, 0),
			current:  time.Date(2026, 9, 26, 2, 59, 59, 999_999_999, time.UTC),
			want:     time.Date(2026, 9, 26, 3, 0, 0, 0, time.UTC),
		},
		{
			name:     "at the time of day",
			schedule: jobs.DailyAt(3, 0),
			current:  time.Date(2026, 9, 26, 3, 0, 0, 0, time.UTC),
			want:     time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC),
		},
		{
			name:     "after the time of day",
			schedule: jobs.DailyAt(3, 0),
			current:  time.Date(2026, 9, 26, 3, 0, 0, 1, time.UTC),
			want:     time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC),
		},
		{
			name:     "across a month end",
			schedule: jobs.DailyAt(3, 0),
			current:  time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC),
			want:     time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC),
		},
		{
			name:     "across a year end",
			schedule: jobs.DailyAt(3, 0),
			current:  time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC),
			want:     time.Date(2027, 1, 1, 3, 0, 0, 0, time.UTC),
		},
		{
			name:     "across the end of February",
			schedule: jobs.DailyAt(23, 30),
			current:  time.Date(2027, 2, 28, 23, 45, 0, 0, time.UTC),
			want:     time.Date(2027, 3, 1, 23, 30, 0, 0, time.UTC),
		},
		{
			name:     "current time in another zone",
			schedule: jobs.DailyAt(3, 0),
			current:  time.Date(2026, 9, 25, 23, 30, 0, 0, fourHoursBehind),
			want:     time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.schedule.Next(test.current)
			if !got.Equal(test.want) || got.Location() != time.UTC {
				t.Errorf("Next(%s) = %s, want %s", test.current, got, test.want)
			}
		})
	}
}

func TestEveryNext(t *testing.T) {
	current := time.Date(2026, 9, 30, 23, 59, 58, 0, time.UTC)

	got := jobs.Every(5 * time.Second).Next(current)

	if want := time.Date(2026, 10, 1, 0, 0, 3, 0, time.UTC); !got.Equal(want) {
		t.Errorf("Next(%s) = %s, want %s", current, got, want)
	}
}

func TestSchedulesPanicOnInvalidArguments(t *testing.T) {
	tests := []struct {
		name     string
		schedule func() river.PeriodicSchedule
	}{
		{name: "interval below one second", schedule: func() river.PeriodicSchedule { return jobs.Every(999 * time.Millisecond) }},
		{name: "negative interval", schedule: func() river.PeriodicSchedule { return jobs.Every(-time.Minute) }},
		{name: "hour 24", schedule: func() river.PeriodicSchedule { return jobs.DailyAt(24, 0) }},
		{name: "negative hour", schedule: func() river.PeriodicSchedule { return jobs.DailyAt(-1, 0) }},
		{name: "minute 60", schedule: func() river.PeriodicSchedule { return jobs.DailyAt(3, 60) }},
		{name: "negative minute", schedule: func() river.PeriodicSchedule { return jobs.DailyAt(3, -1) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !panics(test.schedule) {
				t.Error("schedule was created, want a panic")
			}
		})
	}
}

func panics(create func() river.PeriodicSchedule) (panicked bool) {
	defer func() {
		panicked = recover() != nil
	}()
	create()
	return false
}
