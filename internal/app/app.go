// Package app is one fronted app's runtime: its effective mode, the boot
// state machine, the idle clock and stop. It owns no sockets; the proxy and
// the control socket drive it.
package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fum4/waker/internal/clock"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/idle"
	"github.com/fum4/waker/internal/state"
)

// State is what a visitor (and status) sees of an app.
type State string

// States, in the precedence State() checks them.
const (
	Starting State = "starting"
	Up       State = "up"
	Failed   State = "failed"
	Busy     State = "busy"
	Offline  State = "offline"
	Down     State = "down"
)

// BootRetryCooldown is how long a failed boot answers "failed" before the
// next request may try again.
const BootRetryCooldown = 120 * time.Second

// Process runs an app's commands; boot.Process implements it.
type Process interface {
	// Start launches the start command; the channel yields its exit once.
	Start() (<-chan error, error)
	Healthy(ctx context.Context) bool
	Stop(ctx context.Context) error
}

// Gate decides whether memory allows a boot. Admit returns ok=false to
// refuse at once (busy); otherwise makeRoom, when non-nil, runs on the boot
// goroutine before the start and returns false if it could not free enough.
type Gate interface {
	Admit(a *App) (makeRoom func() bool, ok bool)
}

// Deps are an App's collaborators. Gate, Persist and OnStop may be nil.
type Deps struct {
	Clock   clock.Clock
	Process Process
	Gate    Gate
	// Notify wakes the idle loop to re-plan; it must not block.
	Notify func()
	// Persist saves the app's override (nil = removed).
	Persist func(name string, o *state.Override) error
	// OnStop runs before every stop (e.g. to record the memory peak).
	OnStop func(a *App)
	Log    *slog.Logger
}

// App is one app's runtime. It is safe for concurrent use.
type App struct {
	cfg      config.App
	d        Deps
	activity *idle.Tracker
	bg       sync.WaitGroup

	mu           sync.Mutex
	override     *state.Override
	booting      bool
	bootErr      string
	bootFailedAt time.Time
	busy         bool
	started      bool
	evicted      *Eviction
}

// Eviction records the last time another app stopped this one for memory.
type Eviction struct {
	By       string    `json:"by"`
	Priority int       `json:"priority"`
	At       time.Time `json:"at"`
}

// New builds an app from its config and an optional persisted override.
func New(cfg config.App, d Deps, override *state.Override) *App {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Notify == nil {
		d.Notify = func() {}
	}
	a := &App{cfg: cfg, d: d, override: override}
	a.activity = idle.NewTracker(d.Clock, d.Notify)
	a.d.Log = d.Log.With("app", cfg.Name)
	return a
}

// Config returns the app's declared config.
func (a *App) Config() config.App { return a.cfg }

// Name returns the app's name.
func (a *App) Name() string { return a.cfg.Name }

// Priority returns the app's priority (1 lowest … 10 highest).
func (a *App) Priority() int { return a.cfg.Priority }

// Mode returns the effective mode: the override's, else the declared one.
func (a *App) Mode() config.Mode {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.modeLocked()
}

func (a *App) modeLocked() config.Mode {
	if a.override != nil {
		return a.override.Mode
	}
	return a.cfg.Mode
}

// Idle returns the effective idle duration.
func (a *App) Idle() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.idleLocked()
}

func (a *App) idleLocked() time.Duration {
	if a.override != nil && a.override.Idle > 0 {
		return a.override.Idle.D()
	}
	return a.cfg.Idle.D()
}

// Activity exposes the idle tracker: the proxy calls Begin/End around
// forwarded requests and Touch for heartbeats.
func (a *App) Activity() *idle.Tracker { return a.activity }

// Healthy probes the app once.
func (a *App) Healthy(ctx context.Context) bool { return a.d.Process.Healthy(ctx) }

// State probes health and returns what a visitor sees.
func (a *App) State(ctx context.Context) State {
	a.mu.Lock()
	booting := a.booting
	a.mu.Unlock()
	if booting {
		return Starting
	}
	healthy := a.Healthy(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	switch mode := a.modeLocked(); {
	case a.booting:
		return Starting
	case healthy:
		return Up
	case a.bootErr != "":
		return Failed
	case a.busy && mode != config.ModeOff:
		return Busy
	case mode == config.ModeOff:
		return Offline
	default:
		return Down
	}
}

// Started reports whether this waker booted the app and has not stopped it
// since — only such apps may be evicted.
func (a *App) Started() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.started
}

// Booting reports whether a boot is in progress.
func (a *App) Booting() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.booting
}

// Wait blocks until background boots and stops finish (tests, shutdown).
func (a *App) Wait() { a.bg.Wait() }

func (a *App) goBackground(f func()) {
	a.bg.Add(1)
	go func() {
		defer a.bg.Done()
		f()
	}()
}
