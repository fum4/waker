package evict

import (
	"context"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
)

var ctx = context.Background()

func TestEvictsLowestPriorityThenLongestIdleOneAtATimeThenBoots(t *testing.T) {
	w := newWorld(config.Eviction{})
	top := w.add("top", 10, 1000, 1000) // requires 1000 + 512
	p3 := w.add("p3", 3, 500, 500)
	idle := w.add("p1-idle", 1, 600, 600)
	recent := w.add("p1-recent", 1, 600, 600)
	for _, a := range []*app.App{p3, idle, recent} {
		w.running(t, a)
	}
	w.clock.Advance(time.Minute)
	recent.Activity().Touch()
	w.squeeze(400)

	if st := top.Wake(ctx); st != app.Starting {
		t.Fatalf("wake = %s", st)
	}
	top.Wait()
	assertLog(t, w, "p1-idle stop", "p1-recent stop", "top start")
	if top.State(ctx) != app.Up || p3.State(ctx) != app.Up {
		t.Fatal("top not up or p3 evicted needlessly")
	}
	ev := idle.Snapshot(ctx).Evicted
	if ev == nil || ev.By != "top" || ev.Priority != 10 {
		t.Fatalf("evicted record = %+v", ev)
	}
	if _, ok := idle.DeadlineIn(); ok {
		t.Fatal("an evicted app's idle clock must stay disarmed")
	}
}

func TestNeverEvictsOnEqualHigherOrWhatItDidNotStart(t *testing.T) {
	w := newWorld(config.Eviction{})
	booting := w.add("booting", 5, 1000, 1000)
	on := w.add("on", 1, 2000, 2000, func(a *config.App) { a.Mode = config.ModeOn })
	equal := w.add("equal", 5, 2000, 2000)
	higher := w.add("higher", 9, 2000, 2000)
	w.add("foreign", 1, 2000, 2000) // up, but not started by this waker
	for _, a := range []*app.App{on, equal, higher} {
		w.running(t, a)
	}
	w.pool.running["foreign"] = 2000
	w.squeeze(0)
	if st := booting.Wake(ctx); st != app.Busy || booting.State(ctx) != app.Busy {
		t.Fatalf("wake = %s", st)
	}
	assertLog(t, w)
}

func TestAnOverrideToOnProtects(t *testing.T) {
	w := newWorld(config.Eviction{})
	booting := w.add("booting", 9, 1000, 1000)
	victim := w.add("victim", 1, 3000, 3000)
	w.running(t, victim)
	on := config.ModeOn
	if err := victim.SetMode(&on, 0); err != nil {
		t.Fatal(err)
	}
	victim.Wait()
	w.pool.runs = nil
	w.squeeze(0)
	if st := booting.Wake(ctx); st != app.Busy {
		t.Fatalf("wake = %s", st)
	}
	assertLog(t, w)
}

func TestNoPointlessEviction(t *testing.T) {
	w := newWorld(config.Eviction{})
	booting := w.add("booting", 10, 2000, 2000) // requires 2512
	victim := w.add("victim", 1, 500, 500)
	w.running(t, victim)
	w.squeeze(1000) // short 1512; the victim frees only 500
	if st := booting.Wake(ctx); st != app.Busy {
		t.Fatalf("wake = %s", st)
	}
	assertLog(t, w)
	if victim.Snapshot(ctx).Evicted != nil {
		t.Fatal("evicted record set without an eviction")
	}
}

func TestBusyWhenTheEstimateWasWrong(t *testing.T) {
	w := newWorld(config.Eviction{})
	booting := w.add("booting", 10, 1000, 1000) // requires 1512
	victim := w.add("victim", 1, 1100, 100)     // declares 1100, frees 100
	w.running(t, victim)
	w.squeeze(1000)
	if st := booting.Wake(ctx); st != app.Starting {
		t.Fatalf("wake = %s", st)
	}
	booting.Wait()
	assertLog(t, w, "victim stop")
	if booting.State(ctx) != app.Busy {
		t.Fatalf("state = %s", booting.State(ctx))
	}
}

// Item 4: concurrent boots don't both spend the same free memory; the
// reservation ends with the boot.
func TestConcurrentAdmissionsReserveMemory(t *testing.T) {
	w := newWorld(config.Eviction{})
	a := w.add("a", 5, 1000, 0) // requires 1000 + 2844 = 3844
	b := w.add("b", 5, 1000, 0)
	w.squeeze(5000) // room for one boot, not two
	if _, ok := w.gate.Admit(a); !ok {
		t.Fatal("first boot refused")
	}
	if _, ok := w.gate.Admit(b); ok {
		t.Fatal("second boot admitted against memory reserved by the first")
	}
	w.gate.Release(a)
	if _, ok := w.gate.Admit(b); !ok {
		t.Fatal("refused after the first boot ended")
	}
	w.gate.Release(b)
}

func TestABootReleasesItsReservationWhenItEnds(t *testing.T) {
	w := newWorld(config.Eviction{})
	a := w.add("a", 5, 1000, 0)
	b := w.add("b", 5, 1000, 0)
	w.squeeze(5000)
	if st := a.Wake(ctx); st != app.Starting {
		t.Fatalf("wake a = %s", st)
	}
	a.Wait()
	if st := b.Wake(ctx); st != app.Starting {
		t.Fatalf("wake b after a's boot ended = %s", st)
	}
	b.Wait()
}

func TestPolicies(t *testing.T) {
	tests := []struct {
		name      string
		policy    config.Eviction
		victimP   float64
		bootP     float64
		mutate    func(*config.App)
		wantEvict bool
	}{
		{"lower: 7.4 < 7.5 is evicted", config.Eviction{}, 7.4, 7.5, nil, true},
		{"lower: equal 7.5 is not", config.Eviction{}, 7.5, 7.5, nil, false},
		{"lower-or-equal: equal 7.5 is", config.Eviction{Policy: config.EvictLowerOrEqual}, 7.5, 7.5, nil, true},
		{"lower-or-equal: higher is not", config.Eviction{Policy: config.EvictLowerOrEqual}, 7.6, 7.5, nil, false},
		{"never", config.Eviction{Policy: config.EvictNever}, 1, 10, nil, false},
		{"protect list", config.Eviction{Protect: []string{"victim"}}, 1, 10, nil, false},
		{"evictable: false", config.Eviction{}, 1, 10, func(a *config.App) { f := false; a.Evictable = &f }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(tt.policy)
			booting := w.add("booting", tt.bootP, 1000, 1000)
			var mut []func(*config.App)
			if tt.mutate != nil {
				mut = append(mut, tt.mutate)
			}
			victim := w.add("victim", tt.victimP, 5000, 5000, mut...)
			w.running(t, victim)
			w.squeeze(0)
			if got := w.gate.Evictable(victim, booting); got != tt.wantEvict {
				t.Fatalf("Evictable = %v, want %v", got, tt.wantEvict)
			}
		})
	}
}

func TestDisabledEvictionNeverEvicts(t *testing.T) {
	w := newWorld(config.Eviction{})
	w.gate.Policy.Enabled = false
	booting := w.add("booting", 10, 1000, 1000)
	victim := w.add("victim", 1, 5000, 5000)
	w.running(t, victim)
	w.squeeze(0)
	if w.gate.Evictable(victim, booting) {
		t.Fatal("evictable with eviction disabled")
	}
}

func TestOrderIdleThenPriority(t *testing.T) {
	w := newWorld(config.Eviction{Order: config.OrderIdleThenPriority})
	booting := w.add("booting", 10, 1000, 1000)
	low := w.add("low-recent", 1, 3000, 3000)
	mid := w.add("mid-idle", 4, 3000, 3000)
	w.running(t, mid)
	w.clock.Advance(time.Minute)
	w.running(t, low)
	plan := w.gate.Plan(booting, 1)
	if len(plan) != 2 || plan[0] != mid || plan[1] != low {
		t.Fatalf("plan = %v", names(plan))
	}
	w2 := newWorld(config.Eviction{})
	w2.add("booting", 10, 1000, 1000)
	low2 := w2.add("low-recent", 1, 3000, 3000)
	mid2 := w2.add("mid-idle", 4, 3000, 3000)
	w2.running(t, mid2)
	w2.clock.Advance(time.Minute)
	w2.running(t, low2)
	if plan := w2.gate.Plan(w2.apps["booting"], 1); plan[0] != low2 {
		t.Fatalf("priority-then-idle plan = %v", names(plan))
	}
}

func names(apps []*app.App) []string {
	var out []string
	for _, a := range apps {
		out = append(out, a.Name())
	}
	return out
}
