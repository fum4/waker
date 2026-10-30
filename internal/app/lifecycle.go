package app

import (
	"context"
	"fmt"
	"time"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/state"
)

var timeZero time.Time

// Stop stops the app unconditionally (mode off, shutdown). Requests that
// arrive meanwhile wait for it, then find the app down.
func (a *App) Stop(why string) {
	a.activity.BeginStop()
	a.stopCommitted(why)
}

// stopCommitted runs a stop already committed in the tracker. It records
// what OnStop needs (the memory peak) first, and holds the lifecycle lock so
// a new boot cannot start until the old process is fully gone. Its
// dependencies' idle clocks restart once it is (defers run last-first).
func (a *App) stopCommitted(why string) {
	defer a.releaseDeps()
	defer a.activity.EndStop()
	a.life.Lock()
	defer a.life.Unlock()
	if a.d.OnStop != nil {
		a.d.OnStop(a)
	}
	a.d.Log.Info("stopping", "why", why)
	a.mu.Lock()
	a.started = false
	a.mu.Unlock()
	if err := a.d.Process.Stop(context.Background()); err != nil {
		a.d.Log.Warn("stop command failed", "err", err)
	}
}

// Evict stops the app to make room for by, recording who did it, unless a
// request arrived since it was picked (then it returns false). The idle
// clock stays disarmed until the app's next activity.
func (a *App) Evict(by *App) bool {
	if !a.activity.TryStop() {
		return false
	}
	a.mu.Lock()
	a.evicted = &Eviction{By: by.Name(), Priority: by.Priority(), At: a.d.Clock.Now().UTC().Truncate(time.Second)}
	a.mu.Unlock()
	a.stopCommitted(fmt.Sprintf("evicted by %s (p%g)", by.Name(), by.Priority()))
	return true
}

// DeadlineIn returns the time left until an idle stop; ok is false when
// none is pending (not on-demand, booting, kept up by a dependent, disarmed
// or a request in flight).
func (a *App) DeadlineIn() (time.Duration, bool) {
	a.mu.Lock()
	if a.modeLocked() != config.ModeOnDemand || a.booting {
		a.mu.Unlock()
		return 0, false
	}
	idle := a.idleLocked()
	a.mu.Unlock()
	if a.keptUp() {
		return 0, false
	}
	return a.activity.DeadlineIn(idle)
}

// Tick follows a schedule window that opened or closed, then stops the app
// if its idle deadline has passed. Health is not consulted: an unhealthy
// but running app is stopped too.
func (a *App) Tick() {
	a.followSchedule()
	a.mu.Lock()
	if a.modeLocked() != config.ModeOnDemand || a.booting {
		a.mu.Unlock()
		return
	}
	idle := a.idleLocked()
	a.mu.Unlock()
	// A dependent starting after this check first marks itself in flight on
	// a (wakeDep), so Expire then fails or the dependent waits for the stop.
	if a.keptUp() {
		return
	}
	if a.activity.Expire(idle) {
		a.stopCommitted("idle " + idle.String())
	}
}

// SetMode sets a runtime override; mode nil resets to the declared mode
// (and the schedule). An override equal to the declaration is dropped
// rather than stored, unless the app has a schedule: then it pins the mode
// through windows. Switching to off stops the app in the background; to
// on, wakes it.
func (a *App) SetMode(mode *config.Mode, idle time.Duration) error {
	var o *state.Override
	same := mode != nil && *mode == a.cfg.Mode && (idle == 0 || idle == a.cfg.Idle.D())
	if mode != nil && !(same && a.sched.Empty()) {
		o = &state.Override{Mode: *mode, Since: a.d.Clock.Now().UTC().Truncate(time.Second), Idle: config.Duration(idle)}
		if p := o.Problem(); p != "" {
			return &InputError{p}
		}
	}
	a.mu.Lock()
	a.override = o
	// SetMode acts on this change itself; Tick must not act on it again.
	a.lastMode = a.modeLocked()
	a.mu.Unlock()
	var saveErr error
	if a.d.Persist != nil {
		if err := a.d.Persist(a.cfg.Name, o); err != nil {
			a.d.Log.Error("saving the mode override failed", "err", err)
			saveErr = fmt.Errorf("mode applied but not saved (it will not survive a restart): %w", err)
		}
	}
	a.activity.Touch()
	a.d.Notify()
	switch a.Mode() {
	case config.ModeOff:
		a.goBackground(func() { a.Stop("mode off") })
	case config.ModeOn:
		a.Wake(context.Background())
	}
	return saveErr
}

// Override returns a copy of the active override, or nil.
func (a *App) Override() *state.Override {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.override == nil {
		return nil
	}
	o := *a.override
	return &o
}

// InputError is a rejected request from the CLI (a 400, not a crash).
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }
