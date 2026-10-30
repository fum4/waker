// Package idle tracks an app's activity and drives the deadline-based idle
// clock that stops on-demand apps.
//
// Activity is measured from the END of a request: Begin raises an in-flight
// counter, End lowers it and restarts the clock in the same critical
// section, and no deadline exists while a request is in flight, so an app is
// never stopped mid-request. A stop is committed under the same lock
// (Expire, TryStop, BeginStop); until EndStop, Begin waits, so no request is
// forwarded to an app that is going down.
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
	stopped  *sync.Cond // signalled when a stop ends
	last     time.Time
	armed    bool
	inflight int
	stops    int // stops in progress; overlapping ones (mode off + idle) nest
}

// NewTracker starts armed, with the last activity at "now". notify is called
// whenever a deadline may have moved earlier or appeared, so the scheduler
// re-plans; it must not block.
func NewTracker(clock Now, notify func()) *Tracker {
	t := &Tracker{clock: clock, notify: notify, last: clock.Now(), armed: true}
	t.stopped = sync.NewCond(&t.mu)
	return t
}

// Touch records activity now and re-arms a stopped clock.
func (t *Tracker) Touch() {
	t.mu.Lock()
	wasArmed := t.touchLocked()
	t.mu.Unlock()
	if !wasArmed {
		t.notify()
	}
}

func (t *Tracker) touchLocked() (wasArmed bool) {
	t.last = t.clock.Now()
	wasArmed, t.armed = t.armed, true
	return wasArmed
}

// Begin marks a request in flight. While the app is stopping it waits for
// the stop to finish, so the request then finds the app down and wakes it.
func (t *Tracker) Begin() {
	t.mu.Lock()
	for t.stops > 0 {
		t.stopped.Wait()
	}
	t.inflight++
	wasArmed := t.touchLocked()
	t.mu.Unlock()
	if !wasArmed {
		t.notify()
	}
}

// End marks a request finished; the idle clock restarts from now, in the
// same critical section, so no tick can see the count drop with a stale
// last activity.
func (t *Tracker) End() {
	t.mu.Lock()
	t.inflight--
	t.touchLocked()
	t.mu.Unlock()
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
	if !t.armed || t.inflight > 0 || t.stops > 0 {
		return 0, false
	}
	return max(0, idle-t.clock.Now().Sub(t.last)), true
}

// Expire commits an idle stop when the deadline has passed: it disarms the
// clock and marks the app stopping. It fires once; the caller must call
// EndStop when the stop is done.
func (t *Tracker) Expire(idle time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if left, ok := t.deadlineLocked(idle); !ok || left > 0 {
		return false
	}
	t.armed = false
	t.stops++
	return true
}

// TryStop commits a stop only if nothing is in flight (eviction).
func (t *Tracker) TryStop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inflight > 0 || t.stops > 0 {
		return false
	}
	t.armed = false
	t.stops++
	return true
}

// BeginStop marks an unconditional stop (mode off, shutdown).
func (t *Tracker) BeginStop() {
	t.mu.Lock()
	t.stops++
	t.mu.Unlock()
}

// EndStop ends a committed stop and releases waiting requests.
func (t *Tracker) EndStop() {
	t.mu.Lock()
	if t.stops--; t.stops == 0 {
		t.stopped.Broadcast()
	}
	t.mu.Unlock()
}

// Stopping reports whether a stop is in progress.
func (t *Tracker) Stopping() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stops > 0
}

// Disarm stops the clock until the next activity.
func (t *Tracker) Disarm() {
	t.mu.Lock()
	t.armed = false
	t.mu.Unlock()
}
