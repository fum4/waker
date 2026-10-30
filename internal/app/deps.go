package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fum4/waker/internal/config"
)

// SetDependencies wires the apps a depends on (its depends_on, resolved)
// and registers a as their dependent. Call it for every app before serving:
// the links never change after, so they are read without a lock.
func (a *App) SetDependencies(deps []*App) {
	a.deps = deps
	for _, d := range deps {
		d.dependents = append(d.dependents, a)
	}
}

// KeptUpBy names the dependents holding the app up: started, booting or
// still stopping. While any does, it is neither idle-stopped nor evicted.
func (a *App) KeptUpBy() []string {
	var by []string
	for _, d := range a.dependents {
		if d.Started() || d.Booting() || d.activity.Stopping() {
			by = append(by, d.Name())
		}
	}
	return by
}

func (a *App) keptUp() bool { return len(a.KeptUpBy()) > 0 }

// bootAfterDeps is the boot of an app with dependencies: wake them all,
// then admit and start it, the whole boot within boot_timeout.
func (a *App) bootAfterDeps() {
	deadline := a.d.Clock.Now().Add(a.cfg.BootTimeout.D())
	if problem := a.awaitDeps(deadline); problem != "" {
		a.settle(false, problem)
		return
	}
	var makeRoom func() bool
	if a.d.Gate != nil {
		var ok bool
		if makeRoom, ok = a.d.Gate.Admit(a); !ok {
			a.settle(true, "")
			return
		}
	}
	a.boot(makeRoom, deadline)
}

// awaitDeps wakes every dependency in parallel, each through its own Wake
// (and memory gate), and waits until all are up. It returns "" or what went
// wrong, naming each dependency that did not come up.
func (a *App) awaitDeps(deadline time.Time) string {
	ctx, cancel := context.WithTimeout(context.Background(), deadline.Sub(a.d.Clock.Now()))
	defer cancel()
	problems := make([]string, len(a.deps))
	var wg sync.WaitGroup
	for i, dep := range a.deps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p := a.wakeDep(ctx, dep); p != "" {
				problems[i] = fmt.Sprintf("dependency %s: %s", dep.Name(), p)
				cancel() // the boot fails anyway: stop waiting for the others
			}
		}()
	}
	wg.Wait()
	var out []string
	for _, p := range problems {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "; ")
}

// wakeDep wakes dep and waits for its boot. It returns "" when dep is up or
// a sibling's failure cancelled the wait, else dep's state.
func (a *App) wakeDep(ctx context.Context, dep *App) (problem string) {
	defer func() {
		if r := recover(); r != nil {
			problem = fmt.Sprintf("crashed: %v", r)
		}
	}()
	if dep.Mode() == config.ModeOff {
		return string(Offline) + " (mode off)"
	}
	// In flight while a waits on it, so dep is neither idle-stopped nor
	// evicted before a's boot reaches its own start; then a's booting (and
	// started) state keeps it up.
	dep.activity.Begin()
	defer dep.activity.End()
	st := dep.Wake(ctx)
	if st == Starting {
		st = dep.AwaitBoot(ctx)
	}
	switch {
	case st == Up:
		return ""
	case st != Starting:
		return string(st)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Sprintf("still starting after %s (boot_timeout of %s)", a.cfg.BootTimeout, a.cfg.Name)
	}
	return ""
}

// releaseDeps restarts each dependency's idle clock once a has stopped.
// Touch notifies only a disarmed clock; the deadline a hid is new either way.
func (a *App) releaseDeps() {
	for _, d := range a.deps {
		d.activity.Touch()
		d.d.Notify()
	}
}
