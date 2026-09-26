package jobs

import (
	"fmt"
	"time"

	"github.com/riverqueue/river"
)

const (
	minimumScheduleInterval = time.Second
	hoursPerDay             = 24
	minutesPerHour          = 60
)

type intervalSchedule struct {
	interval time.Duration
}

type dailySchedule struct {
	hour   int
	minute int
}

// Every returns a schedule that runs a periodic job each interval, counted
// from the previous run. It panics when interval is below one second, the
// shortest interval River's periodic job enqueuer supports.
func Every(interval time.Duration) river.PeriodicSchedule {
	if interval < minimumScheduleInterval {
		panic(fmt.Sprintf("schedule interval %s is below the minimum %s", interval, minimumScheduleInterval))
	}
	return intervalSchedule{interval: interval}
}

// DailyAt returns a schedule that runs a periodic job once a day at
// hour:minute UTC. It panics when hour is outside 0 to 23 or minute is outside
// 0 to 59.
func DailyAt(hour, minute int) river.PeriodicSchedule {
	if hour < 0 || hour >= hoursPerDay || minute < 0 || minute >= minutesPerHour {
		panic(fmt.Sprintf("daily schedule time hour=%d minute=%d is not a time of day", hour, minute))
	}
	return dailySchedule{hour: hour, minute: minute}
}

// Next returns current plus the interval.
func (schedule intervalSchedule) Next(current time.Time) time.Time {
	return current.Add(schedule.interval)
}

// Next returns the first hour:minute UTC strictly after current. River passes
// the previous run time, so returning current itself would run the job again
// at once.
func (schedule dailySchedule) Next(current time.Time) time.Time {
	current = current.UTC()
	next := time.Date(current.Year(), current.Month(), current.Day(), schedule.hour, schedule.minute, 0, 0, time.UTC)
	if !next.After(current) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
