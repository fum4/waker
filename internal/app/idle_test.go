package app

import (
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
)

func bootUp(t *testing.T, f *fixture) {
	t.Helper()
	f.app.Wake(ctx)
	f.app.Wait()
	if f.app.State(ctx) != Up {
		t.Fatal("did not come up")
	}
}

func TestIdleStopAfterIdle(t *testing.T) {
	f := newFixture(t, nil)
	bootUp(t, f)
	f.clock.Advance(29 * time.Minute)
	f.app.Tick()
	assertRuns(t, f, "start")
	if s := f.app.Snapshot(ctx); s.IdleIn == nil || s.IdleIn.D() != time.Minute {
		t.Fatalf("idle_in = %v", s.IdleIn)
	}
	f.clock.Advance(time.Minute)
	f.app.Tick()
	assertRuns(t, f, "start", "stop")
}

func TestStopsOnceUntilTheNextActivity(t *testing.T) {
	f := newFixture(t, func(a *config.App) { a.Idle = config.Duration(10 * time.Second) })
	bootUp(t, f)
	f.clock.Advance(10 * time.Second)
	f.app.Tick()
	f.proc.setUp(true)
	f.clock.Advance(10 * time.Second)
	f.app.Tick()
	assertRuns(t, f, "start", "stop")
	if _, ok := f.app.DeadlineIn(); ok {
		t.Fatal("deadline while disarmed")
	}
	f.app.Activity().Touch()
	if left, ok := f.app.DeadlineIn(); !ok || left != 10*time.Second {
		t.Fatalf("DeadlineIn = %v, %v", left, ok)
	}
}

func TestAnUnhealthyAppIsStoppedToo(t *testing.T) {
	f := newFixture(t, func(a *config.App) { a.Idle = config.Duration(10 * time.Second) })
	bootUp(t, f)
	f.proc.setUp(false)
	f.clock.Advance(10 * time.Second)
	f.app.Tick()
	assertRuns(t, f, "start", "stop")
}

func TestNoStopWhileARequestIsInFlight(t *testing.T) {
	f := newFixture(t, func(a *config.App) { a.Idle = config.Duration(10 * time.Second) })
	f.app.Activity().Begin()
	f.clock.Advance(time.Hour)
	f.app.Tick()
	if _, ok := f.app.DeadlineIn(); ok {
		t.Fatal("deadline mid-request")
	}
	f.app.Activity().End()
	if left, ok := f.app.DeadlineIn(); !ok || left != 10*time.Second {
		t.Fatalf("DeadlineIn = %v, %v", left, ok)
	}
	assertRuns(t, f)
}

func TestNoDeadlineWhileBooting(t *testing.T) {
	f := newFixture(t, nil)
	f.proc.gate = make(chan struct{})
	f.app.Wake(ctx)
	if _, ok := f.app.DeadlineIn(); ok {
		t.Fatal("deadline while booting")
	}
	close(f.proc.gate)
}

func TestOnAndOffNeverIdle(t *testing.T) {
	for _, m := range []config.Mode{config.ModeOn, config.ModeOff} {
		f := newFixture(t, func(a *config.App) { a.Mode = m })
		f.proc.setUp(true)
		f.clock.Advance(10 * time.Hour)
		f.app.Tick()
		assertRuns(t, f)
		if _, ok := f.app.DeadlineIn(); ok {
			t.Fatalf("%s: has a deadline", m)
		}
		if s := f.app.Snapshot(ctx); s.IdleIn != nil {
			t.Fatalf("%s: idle_in = %v", m, s.IdleIn)
		}
	}
}
