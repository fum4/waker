package app

import (
	"context"
	"fmt"
	"time"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/state"
)

var timeZero time.Time

// Stop records what OnStop needs (the memory peak), then runs the stop.
func (a *App) Stop(why string) {
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

// Evict stops the app to make room for by, recording who did it. The idle
// clock stays disarmed until the app's next activity.
func (a *App) Evict(by *App) {
	a.activity.Disarm()
	a.mu.Lock()
	a.evicted = &Eviction{By: by.Name(), Priority: by.Priority(), At: a.d.Clock.Now().UTC().Truncate(time.Second)}
	a.mu.Unlock()
	a.Stop(fmt.Sprintf("evicted by %s (p%g)", by.Name(), by.Priority()))
}

// DeadlineIn returns the time left until an idle stop; ok is false when
// none is pending (not on-demand, booting, disarmed or a request in flight).
func (a *App) DeadlineIn() (time.Duration, bool) {
	a.mu.Lock()
	if a.modeLocked() != config.ModeOnDemand || a.booting {
		a.mu.Unlock()
		return 0, false
	}
	idle := a.idleLocked()
	a.mu.Unlock()
	return a.activity.DeadlineIn(idle)
}

// Tick stops the app if its idle deadline has passed. Health is not
// consulted: an unhealthy but running app is stopped too.
func (a *App) Tick() {
	a.mu.Lock()
	if a.modeLocked() != config.ModeOnDemand || a.booting {
		a.mu.Unlock()
		return
	}
	idle := a.idleLocked()
	a.mu.Unlock()
	if a.activity.Expire(idle) {
		a.Stop("idle " + idle.String())
	}
}

// SetMode sets a runtime override; mode nil resets to the declared mode.
// An override equal to the declaration is dropped rather than stored.
// Switching to off stops the app in the background; to on, wakes it.
func (a *App) SetMode(mode *config.Mode, idle time.Duration) error {
	var o *state.Override
	if mode != nil && !(*mode == a.cfg.Mode && (idle == 0 || idle == a.cfg.Idle.D())) {
		o = &state.Override{Mode: *mode, Since: a.d.Clock.Now().UTC().Truncate(time.Second), Idle: config.Duration(idle)}
		if p := o.Problem(); p != "" {
			return &InputError{p}
		}
	}
	a.mu.Lock()
	a.override = o
	a.mu.Unlock()
	if a.d.Persist != nil {
		if err := a.d.Persist(a.cfg.Name, o); err != nil {
			a.d.Log.Error("saving the mode override failed", "err", err)
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
	return nil
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
