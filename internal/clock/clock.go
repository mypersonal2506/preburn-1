package clock

import (
	"sync"
	"time"
)

// Clock tells services the current time.
type Clock interface {
	// Now returns the current time in UTC.
	Now() time.Time
}

// System is the Clock that reads the operating system time.
type System struct{}

// Manual is a Clock that stays at one moment until Set or Advance moves it.
// Tests, the simulator and the demo generator use it to control time. Create
// one with NewManual. It is safe for concurrent use.
type Manual struct {
	mutex sync.Mutex
	now   time.Time
}

// Now returns the operating system time in UTC.
func (System) Now() time.Time {
	return time.Now().UTC()
}

// NewManual returns a Manual clock that reads start until Set or Advance
// moves it.
func NewManual(start time.Time) *Manual {
	return &Manual{now: start.UTC()}
}

// Now returns the clock's current moment in UTC.
func (manual *Manual) Now() time.Time {
	manual.mutex.Lock()
	defer manual.mutex.Unlock()
	return manual.now
}

// Set moves the clock to moment, earlier or later than its current moment.
func (manual *Manual) Set(moment time.Time) {
	manual.mutex.Lock()
	defer manual.mutex.Unlock()
	manual.now = moment.UTC()
}

// Advance moves the clock forward by duration, or back for a negative
// duration.
func (manual *Manual) Advance(duration time.Duration) {
	manual.mutex.Lock()
	defer manual.mutex.Unlock()
	manual.now = manual.now.Add(duration)
}
