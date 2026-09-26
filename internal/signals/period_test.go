package signals_test

import (
	"testing"
	"time"

	"github.com/preburn/preburn/internal/signals"
)

func TestResolvePeriod(t *testing.T) {
	berlin := time.FixedZone("UTC+2", 2*60*60)
	stripePeriod := signals.Period{
		Start: time.Date(2026, time.September, 10, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC),
	}
	revenuePeriod := signals.Period{
		Start: time.Date(2026, time.September, 5, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name                      string
		now                       time.Time
		subscriptionPeriod        *signals.Period
		subscriptionRevenuePeriod *signals.Period
		want                      signals.Period
	}{
		{
			name: "calendar month",
			now:  time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC),
			want: monthPeriod(time.September),
		},
		{
			name: "last instant of a 31 day month",
			now:  time.Date(2026, time.January, 31, 23, 59, 59, 999_999_999, time.UTC),
			want: signals.Period{
				Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
				End:   time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "first instant of a month",
			now:  time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
			want: monthPeriod(time.March),
		},
		{
			name: "february of a leap year",
			now:  time.Date(2028, time.February, 29, 12, 0, 0, 0, time.UTC),
			want: signals.Period{
				Start: time.Date(2028, time.February, 1, 0, 0, 0, 0, time.UTC),
				End:   time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "february of a common year",
			now:  time.Date(2026, time.February, 28, 23, 0, 0, 0, time.UTC),
			want: monthPeriod(time.February),
		},
		{
			name: "december rolls into the next year",
			now:  time.Date(2026, time.December, 15, 0, 0, 0, 0, time.UTC),
			want: signals.Period{
				Start: time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC),
				End:   time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "month is taken in UTC",
			now:  time.Date(2026, time.October, 1, 1, 0, 0, 0, berlin),
			want: monthPeriod(time.September),
		},
		{
			name:                      "subscription revenue period",
			now:                       time.Date(2026, time.September, 16, 0, 0, 0, 0, time.UTC),
			subscriptionRevenuePeriod: &revenuePeriod,
			want:                      revenuePeriod,
		},
		{
			name:                      "subscription period comes first",
			now:                       time.Date(2026, time.September, 16, 0, 0, 0, 0, time.UTC),
			subscriptionPeriod:        &stripePeriod,
			subscriptionRevenuePeriod: &revenuePeriod,
			want:                      stripePeriod,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := signals.ResolvePeriod(test.now, test.subscriptionPeriod, test.subscriptionRevenuePeriod)
			if !got.Start.Equal(test.want.Start) || !got.End.Equal(test.want.End) {
				t.Errorf("ResolvePeriod(%s) = [%s, %s), want [%s, %s)", test.now, got.Start, got.End, test.want.Start, test.want.End)
			}
			if got.Start.Location() != time.UTC || got.End.Location() != time.UTC {
				t.Errorf("ResolvePeriod(%s) locations = %s and %s, want UTC", test.now, got.Start.Location(), got.End.Location())
			}
		})
	}
}

func TestPeriodContains(t *testing.T) {
	period := monthPeriod(time.September)
	tests := []struct {
		moment time.Time
		want   bool
	}{
		{moment: period.Start, want: true},
		{moment: period.End.Add(-time.Nanosecond), want: true},
		{moment: period.End, want: false},
		{moment: period.Start.Add(-time.Nanosecond), want: false},
	}
	for _, test := range tests {
		if got := period.Contains(test.moment); got != test.want {
			t.Errorf("Contains(%s) = %t, want %t", test.moment, got, test.want)
		}
	}
}

func monthPeriod(month time.Month) signals.Period {
	start := time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC)
	return signals.Period{Start: start, End: start.AddDate(0, 1, 0)}
}
