package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
)

var ctx = context.Background()

func TestAnUpAppIsNotBooted(t *testing.T) {
	f := newFixture(t, nil)
	f.proc.setUp(true)
	if got := f.app.Wake(ctx); got != Up {
		t.Fatalf("Wake = %s", got)
	}
	assertRuns(t, f)
}

func TestBoot(t *testing.T) {
	f := newFixture(t, nil)
	if got := f.app.Wake(ctx); got != Starting {
		t.Fatalf("Wake = %s", got)
	}
	f.app.Wait()
	assertRuns(t, f, "start")
	if got := f.app.State(ctx); got != Up || !f.app.Started() {
		t.Fatalf("State = %s, started %v", got, f.app.Started())
	}
}

func TestConcurrentWakesShareOneBoot(t *testing.T) {
	f := newFixture(t, nil)
	f.proc.gate = make(chan struct{})
	var wg sync.WaitGroup
	states := make(chan State, 20)
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); states <- f.app.Wake(ctx) }()
	}
	wg.Wait()
	close(states)
	for s := range states {
		if s != Starting {
			t.Fatalf("a concurrent Wake = %s", s)
		}
	}
	if f.app.State(ctx) != Starting {
		t.Fatal("not starting while the boot runs")
	}
	close(f.proc.gate)
	f.app.Wait()
	assertRuns(t, f, "start")
}

func TestFailureCooldownThenRetry(t *testing.T) {
	f := newFixture(t, nil)
	f.proc.exitErr = errExit
	f.app.Wake(ctx)
	f.app.Wait()
	if got := f.app.State(ctx); got != Failed {
		t.Fatalf("State = %s", got)
	}
	if !strings.Contains(f.app.Snapshot(ctx).Error, "start command exited: exit status 1") {
		t.Fatalf("error = %q", f.app.Snapshot(ctx).Error)
	}
	f.clock.Advance(BootRetryCooldown - 1)
	if got := f.app.Wake(ctx); got != Failed {
		t.Fatalf("inside the cooldown Wake = %s", got)
	}
	f.clock.Advance(1)
	f.proc.exitErr = nil
	if got := f.app.Wake(ctx); got != Starting {
		t.Fatalf("after the cooldown Wake = %s", got)
	}
	f.app.Wait()
	assertRuns(t, f, "start", "start")
	if f.app.State(ctx) != Up {
		t.Fatal("retry did not come up")
	}
}

func TestHealthTimeoutIsAFailure(t *testing.T) {
	f := newFixture(t, nil)
	f.proc.comesUp = false
	f.app.Wake(ctx)
	f.app.Wait()
	if e := f.app.Snapshot(ctx).Error; !strings.Contains(e, "no answer on / after 10m0s") {
		t.Fatalf("error = %q", e)
	}
}

func TestACrashingBootIsAFailureNotStuckStarting(t *testing.T) {
	f := newFixture(t, nil)
	f.proc.panicked = true
	f.app.mu.Lock()
	f.app.claimBootLocked() // skip Wake's own probe, which would panic first
	f.app.mu.Unlock()
	f.app.goBackground(func() { f.app.boot(nil, f.clock.Now().Add(time.Minute)) })
	f.app.Wait()
	if f.app.Booting() || !strings.Contains(f.app.bootErr, "boot crashed: probe exploded") {
		t.Fatalf("booting %v, err %q", f.app.Booting(), f.app.bootErr)
	}
	if got := f.app.Wake(ctx); got != Failed {
		t.Fatalf("Wake = %s", got)
	}
}

func TestBootEndReplansTheIdleClock(t *testing.T) {
	f := newFixture(t, nil)
	f.drainNotified()
	f.app.Wake(ctx)
	f.app.Wait()
	select {
	case <-f.notified:
	default:
		t.Fatal("boot end did not notify the idle loop")
	}
}

func TestModeOffDuringABootSticks(t *testing.T) {
	f := newFixture(t, nil)
	f.proc.gate = make(chan struct{})
	f.proc.startedCh = make(chan struct{})
	f.app.Wake(ctx)
	<-f.proc.startedCh
	off := config.ModeOff
	if err := f.app.SetMode(&off, 0); err != nil {
		t.Fatal(err)
	}
	close(f.proc.gate)
	f.app.Wait()
	runs := f.proc.Runs()
	if runs[len(runs)-1] != "stop" || f.app.State(ctx) != Offline {
		t.Fatalf("runs %v, state %s", runs, f.app.State(ctx))
	}
}

type gate struct {
	ok       bool
	makeRoom func() bool
}

func (g gate) Admit(*App) (func() bool, bool) { return g.makeRoom, g.ok }

func (gate) Release(*App) {}

func TestGateRefusalIsBusy(t *testing.T) {
	f := newFixture(t, nil)
	f.app.d.Gate = gate{ok: false}
	if got := f.app.Wake(ctx); got != Busy {
		t.Fatalf("Wake = %s", got)
	}
	assertRuns(t, f)
	if got := f.app.State(ctx); got != Busy {
		t.Fatalf("State = %s", got)
	}
}

func TestBusyWhenMakingRoomFails(t *testing.T) {
	f := newFixture(t, nil)
	f.app.d.Gate = gate{ok: true, makeRoom: func() bool { return false }}
	if got := f.app.Wake(ctx); got != Starting {
		t.Fatalf("Wake = %s", got)
	}
	f.app.Wait()
	assertRuns(t, f)
	if got := f.app.State(ctx); got != Busy || f.app.Started() {
		t.Fatalf("State = %s", got)
	}
}
