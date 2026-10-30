package app

import (
	"context"
	"fmt"

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
	a.booting = true
	a.mu.Unlock()

	var makeRoom func() bool
	if a.d.Gate != nil {
		var ok bool
		if makeRoom, ok = a.d.Gate.Admit(a); !ok {
			a.mu.Lock()
			a.booting, a.busy = false, true
			a.mu.Unlock()
			return Busy
		}
	}
	a.mu.Lock()
	a.bootErr, a.bootFailedAt, a.busy = "", timeZero, false
	a.mu.Unlock()
	a.goBackground(func() { a.boot(makeRoom) })
	return Starting
}

// boot runs on its own goroutine: make room, start, wait for health, then
// settle the state. A mode switched to off meanwhile wins with a final stop.
func (a *App) boot(makeRoom func() bool) {
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
		errMsg = a.start()
	}()
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
// command fails, or boot_timeout passes. It returns an error message or "".
func (a *App) start() string {
	a.life.Lock()
	exit, err := a.d.Process.Start()
	a.life.Unlock()
	if err != nil {
		return err.Error()
	}
	ctx := context.Background()
	deadline := a.d.Clock.Now().Add(a.cfg.BootTimeout.D())
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
