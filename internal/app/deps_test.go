package app

import (
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/fum4/waker/internal/clock"
	"github.com/fum4/waker/internal/config"
)

// cluster is several apps sharing one fake clock, one recording gate and
// one notify channel, wired by their depends_on.
type cluster struct {
	clock    *clock.Fake
	gate     *recGate
	apps     map[string]*App
	procs    map[string]*fakeProcess
	notified chan struct{}
}

// newCluster builds one app per name; deps maps a name to its depends_on.
// Every app idles after 10s.
func newCluster(t *testing.T, deps map[string][]string, names ...string) *cluster {
	t.Helper()
	c := &cluster{clock: clock.NewFake(), gate: &recGate{}, apps: map[string]*App{}, procs: map[string]*fakeProcess{}, notified: make(chan struct{}, 100)}
	base := newFixture(t, nil).app.Config()
	for _, n := range names {
		ac := base
		ac.Name, ac.DependsOn, ac.Idle = n, deps[n], config.Duration(10*time.Second)
		c.procs[n] = newFake()
		c.apps[n] = New(ac, Deps{Clock: c.clock, Process: c.procs[n], Gate: c.gate, Notify: c.notify,
			Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, nil)
		t.Cleanup(c.apps[n].Wait)
	}
	for _, n := range names {
		var ds []*App
		for _, d := range deps[n] {
			ds = append(ds, c.apps[d])
		}
		c.apps[n].SetDependencies(ds)
	}
	return c
}

func (c *cluster) notify() {
	select {
	case c.notified <- struct{}{}:
	default:
	}
}

func (c *cluster) drain() {
	for len(c.notified) > 0 {
		<-c.notified
	}
}

type recGate struct {
	mu       sync.Mutex
	admitted []string
}

func (g *recGate) Admit(a *App) (func() bool, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.admitted = append(g.admitted, a.Name())
	return nil, true
}

func (g *recGate) Release(*App) {}

func (g *recGate) order() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.admitted)
}

func TestDependenciesBootFirstInParallel(t *testing.T) {
	c := newCluster(t, map[string][]string{"api": {"db", "cache"}}, "api", "db", "cache")
	for _, n := range []string{"db", "cache"} {
		c.procs[n].gate, c.procs[n].startedCh = make(chan struct{}), make(chan struct{})
	}
	depsUp := false
	c.procs["api"].onStart = func() { depsUp = c.procs["db"].isUp() && c.procs["cache"].isUp() }

	if st := c.apps["api"].Wake(ctx); st != Starting {
		t.Fatalf("Wake = %s", st)
	}
	<-c.procs["db"].startedCh // both are starting at once:
	<-c.procs["cache"].startedCh
	if runs := c.procs["api"].Runs(); len(runs) != 0 || c.apps["api"].State(ctx) != Starting {
		t.Fatalf("api ran %v before its dependencies were up", runs)
	}
	if got := c.gate.order(); !slices.Contains(got, "db") || !slices.Contains(got, "cache") || slices.Contains(got, "api") {
		t.Fatalf("admitted %v: each dependency through its own gate, api not yet", got)
	}
	close(c.procs["db"].gate)
	close(c.procs["cache"].gate)
	c.apps["api"].Wait()
	if !depsUp || c.apps["api"].State(ctx) != Up {
		t.Fatalf("api started with deps up: %v, state %s", depsUp, c.apps["api"].State(ctx))
	}
	if got := c.gate.order(); got[len(got)-1] != "api" {
		t.Fatalf("admitted %v, want api last", got)
	}
}

func TestADependencyThatDoesNotComeUpFailsTheBoot(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(c *cluster)
		want    string
		depRuns []string
	}{
		{"failed", func(c *cluster) { c.procs["db"].exitErr = errExit }, "dependency db: failed", []string{"start"}},
		{"mode off: not woken", func(c *cluster) {
			off := config.ModeOff
			if err := c.apps["db"].SetMode(&off, 0); err != nil {
				panic(err)
			}
			c.apps["db"].Wait()
		}, "dependency db: offline (mode off)", []string{"stop"}}, // the mode-off stop only
		{"the other one failed", func(c *cluster) { c.procs["cache"].exitErr = errExit }, "dependency cache: failed", []string{"start"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCluster(t, map[string][]string{"api": {"db", "cache"}}, "api", "db", "cache")
			tt.setup(c)
			c.apps["api"].Wake(ctx)
			c.apps["api"].Wait()
			s := c.apps["api"].Snapshot(ctx)
			if s.State != Failed || s.Error != tt.want {
				t.Fatalf("state %s, error %q; want failed, %q", s.State, s.Error, tt.want)
			}
			if runs := c.procs["api"].Runs(); len(runs) != 0 {
				t.Fatalf("api ran %v", runs)
			}
			if got := c.procs["db"].Runs(); !slices.Equal(got, tt.depRuns) {
				t.Fatalf("db ran %v, want %v", got, tt.depRuns)
			}
			if got := c.apps["api"].Wake(ctx); got != Failed {
				t.Fatalf("inside the cooldown Wake = %s", got)
			}
		})
	}
}

func TestADependencyAlreadyUpIsNotBooted(t *testing.T) {
	c := newCluster(t, map[string][]string{"api": {"db"}}, "api", "db")
	c.procs["db"].setUp(true)
	c.apps["api"].Wake(ctx)
	c.apps["api"].Wait()
	if runs := c.procs["db"].Runs(); len(runs) != 0 || c.apps["api"].State(ctx) != Up {
		t.Fatalf("db ran %v, api %s", runs, c.apps["api"].State(ctx))
	}
}

func TestADependencyStaysUpWhileItsDependentIs(t *testing.T) {
	c := newCluster(t, map[string][]string{"api": {"db"}}, "api", "db")
	api, db := c.apps["api"], c.apps["db"]
	api.Wake(ctx)
	api.Wait()
	db.Wait()
	api.Activity().Begin() // a long request keeps api up past db's idle
	c.clock.Advance(time.Hour)
	db.Tick()
	if _, ok := db.DeadlineIn(); ok {
		t.Fatal("a kept-up dependency has an idle deadline")
	}
	if s := db.Snapshot(ctx); s.State != Up || !slices.Equal(s.KeptUpBy, []string{"api"}) || s.IdleIn != nil {
		t.Fatalf("db %s, kept up by %v, idle_in %v", s.State, s.KeptUpBy, s.IdleIn)
	}
	if s := api.Snapshot(ctx); !slices.Equal(s.DependsOn, []string{"db"}) {
		t.Fatalf("api depends_on = %v", s.DependsOn)
	}

	api.Activity().End()
	c.clock.Advance(10 * time.Second)
	c.drain()
	api.Tick() // api's idle stop releases db
	assertRunsOf(t, c.procs["api"], "start", "stop")
	select {
	case <-c.notified:
	default:
		t.Fatal("the release did not re-plan the idle loop")
	}
	if left, ok := db.DeadlineIn(); !ok || left != 10*time.Second {
		t.Fatalf("db DeadlineIn = %v, %v; want its own idle from api's stop", left, ok)
	}
	c.clock.Advance(9 * time.Second)
	db.Tick()
	assertRunsOf(t, c.procs["db"], "start")
	c.clock.Advance(time.Second)
	db.Tick()
	assertRunsOf(t, c.procs["db"], "start", "stop")
}

func assertRunsOf(t *testing.T, p *fakeProcess, want ...string) {
	t.Helper()
	if got := p.Runs(); !slices.Equal(got, want) {
		t.Fatalf("runs = %v, want %v", got, want)
	}
}
