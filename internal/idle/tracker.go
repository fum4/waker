// Package idle tracks an app's activity and drives the deadline-based idle
// clock that stops on-demand apps.
//
// Activity is measured from the END of a request: Begin raises an in-flight
// counter, End lowers it and restarts the clock, and no deadline exists while
// a request is in flight, so an app is never stopped mid-request.
package idle

import (
	"sync"
	"time"
)

// Now is the time source the tracker needs.
type Now interface{ Now() time.Time }

// Tracker is one app's activity record. It is safe for concurrent use.
type Tracker struct {
	clock  Now
	notify func()

	mu       sync.Mutex
	last     time.Time
	armed    bool
	inflight int
}

// NewTracker starts armed, with the last activity at "now". notify is called
// whenever a deadline may have moved earlier or appeared, so the scheduler
// re-plans; it must not block.
func NewTracker(clock Now, notify func()) *Tracker {
	return &Tracker{clock: clock, notify: notify, last: clock.Now(), armed: true}
}

// Touch records activity now and re-arms a stopped clock.
func (t *Tracker) Touch() {
	t.mu.Lock()
	t.last = t.clock.Now()
	wasArmed := t.armed
	t.armed = true
	t.mu.Unlock()
	if !wasArmed {
		t.notify()
	}
}

// Begin marks a request in flight.
func (t *Tracker) Begin() {
	t.mu.Lock()
	t.inflight++
	t.mu.Unlock()
	t.Touch()
}

// End marks a request finished; the idle clock restarts from now.
func (t *Tracker) End() {
	t.mu.Lock()
	t.inflight--
	t.mu.Unlock()
	t.Touch()
	t.notify()
}

// Inflight returns the number of requests in flight.
func (t *Tracker) Inflight() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.inflight
}

// LastActivity returns when activity was last recorded.
func (t *Tracker) LastActivity() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.last
}

// IdleIn returns how long until idle expires (negative once passed),
// ignoring arming and in-flight requests.
func (t *Tracker) IdleIn(idle time.Duration) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return idle - t.clock.Now().Sub(t.last)
}

// DeadlineIn returns the time left before an idle stop is due, clamped at 0.
// ok is false when no stop is pending: disarmed, or a request is in flight.
func (t *Tracker) DeadlineIn(idle time.Duration) (left time.Duration, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.deadlineLocked(idle)
}

func (t *Tracker) deadlineLocked(idle time.Duration) (time.Duration, bool) {
	if !t.armed || t.inflight > 0 {
		return 0, false
	}
	return max(0, idle-t.clock.Now().Sub(t.last)), true
}

// Expire disarms the clock and reports true when the idle deadline has
// passed. It fires once; the next activity re-arms it.
func (t *Tracker) Expire(idle time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if left, ok := t.deadlineLocked(idle); !ok || left > 0 {
		return false
	}
	t.armed = false
	return true
}

// Disarm stops the clock until the next activity (used on eviction).
func (t *Tracker) Disarm() {
	t.mu.Lock()
	t.armed = false
	t.mu.Unlock()
}
