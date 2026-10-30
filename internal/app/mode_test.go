package app

import (
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/state"
)

func modePtr(m config.Mode) *config.Mode { return &m }

func TestOverrideSetsPersistsAndResets(t *testing.T) {
	f := newFixture(t, func(a *config.App) { a.Mode = config.ModeOn })
	f.proc.setUp(true)
	if err := f.app.SetMode(modePtr(config.ModeOff), 0); err != nil {
		t.Fatal(err)
	}
	f.app.Wait()
	assertRuns(t, f, "stop")
	s := f.app.Snapshot(ctx)
	if s.Mode != config.ModeOff || s.DeclaredMode != config.ModeOn || s.OverrideSince == nil || s.State != Offline {
		t.Fatalf("snapshot = %+v", s)
	}
	if o := f.saved["shop"]; o == nil || o.Mode != config.ModeOff {
		t.Fatalf("persisted = %+v", o)
	}
	if err := f.app.SetMode(nil, 0); err != nil {
		t.Fatal(err)
	}
	f.app.Wait()
	if f.app.Mode() != config.ModeOn || f.saved["shop"] != nil {
		t.Fatalf("reset: mode %s, saved %+v", f.app.Mode(), f.saved["shop"])
	}
}

func TestDeclaredModeDropsTheOverride(t *testing.T) {
	f := newFixture(t, func(a *config.App) { a.Mode = config.ModeOff })
	if err := f.app.SetMode(modePtr(config.ModeOnDemand), 0); err != nil {
		t.Fatal(err)
	}
	if f.app.Override() == nil {
		t.Fatal("no override")
	}
	if err := f.app.SetMode(modePtr(config.ModeOff), 0); err != nil {
		t.Fatal(err)
	}
	f.app.Wait()
	if f.app.Override() != nil {
		t.Fatal("override equal to the declaration was kept")
	}
}

func TestSwitchToOnBoots(t *testing.T) {
	f := newFixture(t, func(a *config.App) { a.Mode = config.ModeOff })
	if err := f.app.SetMode(modePtr(config.ModeOn), 0); err != nil {
		t.Fatal(err)
	}
	f.app.Wait()
	assertRuns(t, f, "start")
}

func TestIdleOverride(t *testing.T) {
	f := newFixture(t, func(a *config.App) { a.Mode = config.ModeOff })
	if err := f.app.SetMode(modePtr(config.ModeOnDemand), 10*time.Second); err != nil {
		t.Fatal(err)
	}
	s := f.app.Snapshot(ctx)
	if s.Idle.D() != 10*time.Second || s.DeclaredIdle.D() != 30*time.Minute {
		t.Fatalf("idle %s, declared %s", s.Idle, s.DeclaredIdle)
	}
	if o := f.saved["shop"]; o == nil || o.Idle.D() != 10*time.Second {
		t.Fatalf("persisted = %+v", o)
	}
	_ = f.app.SetMode(nil, 0)
	if f.app.Idle() != 30*time.Minute {
		t.Fatalf("reset idle = %s", f.app.Idle())
	}
}

func TestBadOverridesAreInputErrors(t *testing.T) {
	f := newFixture(t, nil)
	tests := []struct {
		mode config.Mode
		idle time.Duration
		want string
	}{
		{"sometimes", 0, "mode must be one of"},
		{config.ModeOn, time.Second, "idle applies to on-demand only"},
	}
	for _, tt := range tests {
		err := f.app.SetMode(modePtr(tt.mode), tt.idle)
		if _, ok := err.(*InputError); !ok || !strings.HasPrefix(err.Error(), tt.want) {
			t.Fatalf("SetMode(%s, %s) = %v", tt.mode, tt.idle, err)
		}
	}
	if f.app.Override() != nil {
		t.Fatal("a rejected override was applied")
	}
}

func TestPersistedOverrideApplies(t *testing.T) {
	f := newFixture(t, nil)
	a := New(f.app.Config(), f.app.d, &state.Override{Mode: config.ModeOff})
	if a.Mode() != config.ModeOff {
		t.Fatal("persisted override ignored")
	}
}
