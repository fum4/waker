package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/state"
)

// Item 1: a boot requested while the previous process is still stopping
// waits for the stop, so the stop can't hit the new process.
func TestABootWaitsForAStopInProgress(t *testing.T) {
	f := newFixture(t, nil)
	bootUp(t, f)
	f.proc.stopGate = make(chan struct{})
	stopped := make(chan struct{})
	go func() { f.app.Stop("test"); close(stopped) }()
	waitUntil(t, func() bool { f.proc.mu.Lock(); defer f.proc.mu.Unlock(); return f.proc.inStop })

	if st := f.app.Wake(ctx); st != Starting {
		t.Fatalf("wake during a stop = %s, want starting (not the dying app's up)", st)
	}
	time.Sleep(50 * time.Millisecond)
	assertRuns(t, f, "start") // the second start is held back
	close(f.proc.stopGate)
	<-stopped
	f.app.Wait()
	assertRuns(t, f, "start", "stop", "start")
	if f.proc.overlap || f.app.State(ctx) != Up {
		t.Fatalf("overlap %v, state %s", f.proc.overlap, f.app.State(ctx))
	}
}

// Item 2: a request arriving mid-stop waits for it instead of being
// forwarded to the dying app; eviction skips an app that got a request.
func TestARequestDuringAnIdleStopWaitsForIt(t *testing.T) {
	f := newFixture(t, func(a *config.App) { a.Idle = config.Duration(time.Second) })
	bootUp(t, f)
	f.proc.stopGate = make(chan struct{})
	f.clock.Advance(time.Second)
	ticked := make(chan struct{})
	go func() { f.app.Tick(); close(ticked) }()
	waitUntil(t, func() bool { return f.app.Activity().Stopping() })

	begun := make(chan struct{})
	go func() { f.app.Activity().Begin(); close(begun) }()
	select {
	case <-begun:
		t.Fatal("a request began while the app was stopping")
	case <-time.After(50 * time.Millisecond):
	}
	close(f.proc.stopGate)
	<-ticked
	<-begun
	f.app.Activity().End()
}

func TestEvictSkipsAnAppWithARequestInFlight(t *testing.T) {
	f := newFixture(t, nil)
	bootUp(t, f)
	by := newFixture(t, nil).app
	f.app.Activity().Begin()
	if f.app.Evict(by) {
		t.Fatal("evicted mid-request")
	}
	f.app.Activity().End()
	assertRuns(t, f, "start")
	if !f.app.Evict(by) {
		t.Fatal("an idle app was not evicted")
	}
	assertRuns(t, f, "start", "stop")
}

// Item 5: visitor polls share one probe and reuse it for StateCacheTTL.
func TestVisitorStateIsCachedAndCoalesced(t *testing.T) {
	f := newFixture(t, nil)
	f.proc.probeWait = 20 * time.Millisecond
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = f.app.VisitorState(ctx) }()
	}
	wg.Wait()
	if n := probes(f); n != 1 {
		t.Fatalf("20 concurrent polls made %d probes, want 1", n)
	}
	f.clock.Advance(StateCacheTTL - time.Millisecond)
	f.app.VisitorState(ctx)
	if n := probes(f); n != 1 {
		t.Fatalf("a poll inside the TTL probed (%d)", n)
	}
	f.clock.Advance(time.Millisecond)
	f.app.VisitorState(ctx)
	if n := probes(f); n != 2 {
		t.Fatalf("a poll after the TTL did not probe (%d)", n)
	}
}

// Nit: a mode change that can't be saved is applied but reported.
func TestSetModeReportsAFailedSave(t *testing.T) {
	f := newFixture(t, nil)
	f.app.d.Persist = func(string, *state.Override) error { return errors.New("disk full") }
	off := config.ModeOff
	err := f.app.SetMode(&off, 0)
	if err == nil || f.app.Mode() != config.ModeOff {
		t.Fatalf("err %v, mode %s", err, f.app.Mode())
	}
	f.app.Wait()
}

func probes(f *fixture) int {
	f.proc.mu.Lock()
	defer f.proc.mu.Unlock()
	return f.proc.probes
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 5s")
		}
		time.Sleep(time.Millisecond)
	}
}
