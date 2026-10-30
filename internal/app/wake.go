package app

import (
	"context"
	"fmt"
	"time"

	"github.com/fum4/waker/internal/config"
)

// Wake makes sure the app is up or booting and returns its state: Up,
// Starting, Busy or Failed. Concurrent callers share a single boot.
func (a *App) Wake(ctx context.Context) State {
	a.mu.Lock()
	switch {
	case a.booting:
		a.mu.Unlock()
		return Starting
	case !a.bootFailedAt.IsZero() && a.d.Clock.Now().Sub(a.bootFailedAt) < BootRetryCooldown:
		a.mu.Unlock()
		return Failed
	}
	a.mu.Unlock()

	// A stopping app may still answer health, but it is going down: boot
	// anew (the boot waits for the stop) rather than report it up.
	if !a.activity.Stopping() && a.Healthy(ctx) {
		return Up
	}

	a.mu.Lock()
	if a.booting {
		a.mu.Unlock()
		return Starting
	}
	// Claim the boot before asking the gate, which may read other apps:
	// holding our lock across it could deadlock two apps waking each other.
	a.claimBootLocked()
	if len(a.deps) > 0 {
		// Admitted only once its dependencies are up: a reservation taken now
		// would count against their own boots.
		a.bootErr, a.bootFailedAt, a.busy = "", timeZero, false
		a.mu.Unlock()
		a.goBackground(a.bootAfterDeps)
		return Starting
	}
	a.mu.Unlock()

	var makeRoom func() bool
	if a.d.Gate != nil {
		var ok bool
		if makeRoom, ok = a.d.Gate.Admit(a); !ok {
			a.mu.Lock()
			a.booting, a.busy = false, true
			close(a.bootDone)
			a.mu.Unlock()
			return Busy
		}
	}
	a.mu.Lock()
	a.bootErr, a.bootFailedAt, a.busy = "", timeZero, false
	a.mu.Unlock()
	deadline := a.d.Clock.Now().Add(a.cfg.BootTimeout.D())
	a.goBackground(func() { a.boot(makeRoom, deadline) })
	return Starting
}

// claimBootLocked marks a boot running; whoever ends it closes bootDone.
func (a *App) claimBootLocked() {
	a.booting = true
	a.bootDone = make(chan struct{})
}

// AwaitBoot blocks until the running boot (if any) ends, then returns the
// app's state. If ctx ends first it returns Starting.
func (a *App) AwaitBoot(ctx context.Context) State {
	a.mu.Lock()
	booting, done := a.booting, a.bootDone
	a.mu.Unlock()
	if booting {
		select {
		case <-done:
		case <-ctx.Done():
			return Starting
		}
	}
	// The probe has its own timeout; a deadline about to pass must not fail it.
	return a.State(context.WithoutCancel(ctx))
}

// boot runs on its own goroutine: make room, start, wait for health until
// deadline, then settle the state.
func (a *App) boot(makeRoom func() bool, deadline time.Time) {
	if a.d.Gate != nil {
		defer a.d.Gate.Release(a)
	}
	busy, errMsg := false, ""
	func() {
		defer func() {
			if r := recover(); r != nil {
				errMsg = fmt.Sprintf("boot crashed: %v", r)
			}
		}()
		if makeRoom != nil && !makeRoom() {
			busy = true
			return
		}
		errMsg = a.start(deadline)
	}()
	a.settle(busy, errMsg)
}

// settle ends the claimed boot: it records the outcome and closes bootDone.
// A mode switched to off meanwhile wins with a final stop.
func (a *App) settle(busy bool, errMsg string) {
	modeOff := a.Mode() == config.ModeOff
	if modeOff {
		errMsg = "" // the mode-off stop killed the start; that is not a failure
	}

	a.mu.Lock()
	a.booting, a.busy = false, busy
	a.started = !busy && errMsg == ""
	a.bootErr = errMsg
	if errMsg != "" {
		a.bootFailedAt = a.d.Clock.Now()
	}
	close(a.bootDone)
	a.mu.Unlock()
	a.activity.Touch()
	a.d.Notify()

	switch {
	case errMsg != "":
		a.d.Log.Error("boot failed", "err", errMsg)
	case busy:
		a.d.Log.Warn("boot refused: not enough memory after evicting")
	default:
		a.d.Log.Info("up")
	}
	if modeOff {
		a.Stop("mode off")
	}
}

// start launches the app and polls health until it answers, the start
// command fails, or deadline (boot_timeout from the boot's start) passes. It
// returns an error message or "".
func (a *App) start(deadline time.Time) string {
	a.life.Lock()
	exit, err := a.d.Process.Start()
	a.life.Unlock()
	if err != nil {
		return err.Error()
	}
	ctx := context.Background()
	for {
		if a.Healthy(ctx) {
			return ""
		}
		select {
		case err, ok := <-exit:
			if ok && err != nil {
				return fmt.Sprintf("start command exited: %v", err)
			}
			exit = nil // exited 0: handed off to a service manager; keep polling
		default:
		}
		if !a.d.Clock.Now().Before(deadline) {
			return fmt.Sprintf("no answer on %s after %s", a.healthTarget(), a.cfg.BootTimeout)
		}
		if err := a.d.Clock.Sleep(ctx, a.cfg.Health.Interval.D()); err != nil {
			return err.Error()
		}
	}
}

func (a *App) healthTarget() string {
	if a.cfg.Health.Path != "" {
		return a.cfg.Health.Path
	}
	return a.cfg.Health.Command[0]
}
