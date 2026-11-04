package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
)

func workerFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixture(t, func(ac *config.App) {
		ac.Listen, ac.Upstream = "", ""
		ac.Mode = config.ModeOn
		ac.Health.Path = ""
		ac.Health.Command = []string{"healthy"}
	})
}

func TestWorkerTickStartsRestartsAndStops(t *testing.T) {
	f := workerFixture(t)
	f.app.Tick()
	f.app.Wait()
	if !f.proc.isUp() || !f.app.Started() {
		t.Fatal("worker did not start without a request")
	}
	f.clock.Advance(f.app.Config().Idle.D())
	f.app.Tick()
	f.app.Wait()
	if !slices.Equal(f.proc.Runs(), []string{"start"}) {
		t.Fatalf("healthy worker restarted or idle-stopped: %v", f.proc.Runs())
	}
	f.proc.setUp(false)
	f.clock.Advance(f.app.Config().Health.Interval.D())
	f.app.Tick()
	f.app.Wait()
	if !slices.Equal(f.proc.Runs(), []string{"start", "stop", "start"}) || !f.proc.isUp() {
		t.Fatalf("unhealthy worker recovery = %v", f.proc.Runs())
	}
	if err := f.app.SetMode(modePtr(config.ModeOff), 0); err != nil {
		t.Fatal(err)
	}
	f.app.Wait()
	f.clock.Advance(24 * time.Hour)
	f.app.Tick()
	f.app.Wait()
	if f.proc.isUp() || f.app.State(context.Background()) != Offline {
		t.Fatal("operator off did not keep worker stopped")
	}
	if err := f.app.SetMode(nil, 0); err != nil {
		t.Fatal(err)
	}
	f.app.Wait()
	if !f.proc.isUp() || f.app.Override() != nil {
		t.Fatal("reset did not restore declared on")
	}
}

func TestWorkerFailedBootBackoffAndCleanup(t *testing.T) {
	f := workerFixture(t)
	f.proc.exitErr = errors.New("cannot start")
	f.app.Tick()
	f.app.Wait()
	if f.app.State(context.Background()) != Failed || !slices.Equal(f.proc.Runs(), []string{"start", "stop"}) {
		t.Fatalf("failed worker was not cleaned up: %v", f.proc.Runs())
	}
	f.clock.Advance(BootRetryCooldown - time.Nanosecond)
	f.app.Tick()
	f.app.Wait()
	if !slices.Equal(f.proc.Runs(), []string{"start", "stop"}) {
		t.Fatal("retried before cooldown")
	}
	if left, ok := f.app.NextEventIn(); !ok || left != time.Nanosecond {
		t.Fatalf("next retry = %s, %t", left, ok)
	}
	f.proc.exitErr = nil
	f.clock.Advance(time.Nanosecond)
	f.app.Tick()
	f.app.Wait()
	if !f.proc.isUp() || !slices.Equal(f.proc.Runs(), []string{"start", "stop", "start"}) {
		t.Fatalf("worker did not recover at cooldown: %v", f.proc.Runs())
	}
}

func TestWorkerRejectsOnDemandWithoutMutation(t *testing.T) {
	f := workerFixture(t)
	if err := f.app.SetMode(modePtr(config.ModeOff), 0); err != nil {
		t.Fatal(err)
	}
	f.app.Wait()
	before := *f.app.Override()
	var input *InputError
	if err := f.app.SetMode(modePtr(config.ModeOnDemand), time.Second); !errors.As(err, &input) {
		t.Fatalf("on-demand = %v", err)
	}
	if after := f.app.Override(); *after != before || *f.saved[f.app.Name()] != before {
		t.Fatal("invalid mode mutated or persisted override")
	}
	if err := f.app.SetMode(modePtr(config.Mode("invalid")), 0); !errors.As(err, &input) {
		t.Fatalf("invalid mode = %v", err)
	}
	if f.app.Override().Mode != config.ModeOff || f.saved[f.app.Name()].Mode != config.ModeOff {
		t.Fatal("invalid input replaced off override")
	}
}

func TestWorkerRetriesMemoryRefusal(t *testing.T) {
	f := workerFixture(t)
	f.app.d.Gate = gate{ok: false}
	f.app.Tick()
	f.app.Wait()
	if f.app.State(context.Background()) != Busy || len(f.proc.Runs()) != 0 {
		t.Fatal("memory refusal started the worker")
	}
	if left, ok := f.app.NextEventIn(); !ok || left != f.app.Config().Health.Interval.D() {
		t.Fatalf("memory retry = %s, %t", left, ok)
	}
	f.app.d.Gate = gate{ok: true}
	f.clock.Advance(f.app.Config().Health.Interval.D())
	f.app.Tick()
	f.app.Wait()
	if !f.proc.isUp() {
		t.Fatal("worker did not retry when memory became available")
	}
}

func TestWorkerDoesNotRestartAnOngoingBoot(t *testing.T) {
	f := workerFixture(t)
	f.proc.gate, f.proc.startedCh = make(chan struct{}), make(chan struct{})
	f.app.Tick()
	<-f.proc.startedCh
	f.clock.Advance(f.app.Config().Health.Interval.D())
	f.app.Tick()
	close(f.proc.gate)
	f.app.Wait()
	if !f.proc.isUp() || !slices.Equal(f.proc.Runs(), []string{"start"}) {
		t.Fatalf("overlapping worker boots: %v", f.proc.Runs())
	}
}

func TestWorkerBootTimeoutStopsTheProcess(t *testing.T) {
	f := workerFixture(t)
	f.proc.comesUp = false
	f.app.cfg.BootTimeout = config.Duration(2 * time.Second)
	f.app.Tick()
	f.app.Wait()
	if f.app.State(context.Background()) != Failed || f.app.Started() || !slices.Equal(f.proc.Runs(), []string{"start", "stop"}) {
		t.Fatalf("timed-out worker was not cleaned up: %v", f.proc.Runs())
	}
	f.app.Tick()
	f.app.Wait()
	if left, ok := f.app.NextEventIn(); !ok || left != BootRetryCooldown {
		t.Fatalf("timeout cooldown = %s, %t", left, ok)
	}
}

func TestWorkerDependencyFailureCleansUpOnlyOwnedProcesses(t *testing.T) {
	for _, restarting := range []bool{false, true} {
		f := workerFixture(t)
		var want []string
		if restarting {
			f.app.Tick()
			f.app.Wait()
			f.proc.setUp(false)
			want = []string{"start", "stop"}
		}
		dependency := newFixture(t, func(ac *config.App) { ac.Mode = config.ModeOff })
		f.app.SetDependencies([]*App{dependency.app})
		f.clock.Advance(f.app.Config().Health.Interval.D())
		f.app.Tick()
		f.app.Wait()
		if f.app.State(context.Background()) != Failed || !slices.Equal(f.proc.Runs(), want) {
			t.Fatalf("worker commands with restarting=%t: %v, want %v", restarting, f.proc.Runs(), want)
		}
	}
}

func TestWorkerModeOnCleansUpBeforeRestart(t *testing.T) {
	f := workerFixture(t)
	f.app.Tick()
	f.app.Wait()
	f.proc.setUp(false)
	if err := f.app.SetMode(modePtr(config.ModeOn), 0); err != nil {
		t.Fatal(err)
	}
	f.app.Wait()
	if !f.proc.isUp() || !slices.Equal(f.proc.Runs(), []string{"start", "stop", "start"}) {
		t.Fatalf("mode on left the old worker running: %v", f.proc.Runs())
	}
}
