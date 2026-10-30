package app

import (
	"testing"

	"github.com/fum4/waker/internal/config"
)

// A stopped app's state says why it isn't running: asleep (on-demand, the
// next request wakes it), offline (mode off) or down (it should be up).
func TestStateOfAStoppedApp(t *testing.T) {
	tests := []struct {
		mode config.Mode
		want State
	}{
		{config.ModeOnDemand, Asleep},
		{config.ModeOff, Offline},
		{config.ModeOn, Down},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			f := newFixture(t, func(a *config.App) { a.Mode = tt.mode })
			if got := f.app.State(ctx); got != tt.want {
				t.Fatalf("State = %s, want %s", got, tt.want)
			}
		})
	}
}

// After an idle stop an on-demand app is asleep, not down.
func TestAsleepAfterAnIdleStop(t *testing.T) {
	f := newFixture(t, nil)
	f.app.Wake(ctx)
	f.app.Wait()
	if got := f.app.State(ctx); got != Up {
		t.Fatalf("after boot = %s", got)
	}
	f.clock.Advance(f.app.Idle() + 1)
	f.app.Tick()
	if got := f.app.State(ctx); got != Asleep {
		t.Fatalf("after the idle stop = %s, want asleep", got)
	}
}
