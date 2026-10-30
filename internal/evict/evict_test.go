package evict

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/clock"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/memory"
)

// pool is shared memory: every running fake app consumes its size.
type pool struct {
	mu      sync.Mutex
	total   int64
	running map[string]int64
	runs    []string
}

func (p *pool) AvailableMiB() (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	avail := p.total
	for _, s := range p.running {
		avail -= s
	}
	return avail, nil
}

func (p *pool) log() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.runs)
}

type proc struct {
	p    *pool
	name string
	size int64
}

func (f *proc) Start() (<-chan error, error) {
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	f.p.running[f.name] = f.size
	f.p.runs = append(f.p.runs, f.name+" start")
	return make(chan error), nil
}

func (f *proc) Healthy(context.Context) bool {
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	_, ok := f.p.running[f.name]
	return ok
}

func (f *proc) Stop(context.Context) error {
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	delete(f.p.running, f.name)
	f.p.runs = append(f.p.runs, f.name+" stop")
	return nil
}

type world struct {
	pool  *pool
	clock *clock.Fake
	gate  *Gate
	apps  map[string]*app.App
	order []*app.App
}

func newWorld(policy config.Eviction) *world {
	if policy.Policy == "" {
		policy.Policy = config.EvictLower
	}
	if policy.Order == "" {
		policy.Order = config.OrderPriorityThenIdle
	}
	policy.Enabled = true
	w := &world{pool: &pool{total: 1 << 20, running: map[string]int64{}}, clock: clock.NewFake(), apps: map[string]*app.App{}}
	res := config.Resources{Headroom: config.Headroom{Curve: config.CurveLinear, Max: 5120 * config.MiB, Min: 512 * config.MiB}}
	w.gate = &Gate{
		Apps:   func() []*app.App { return w.order },
		Budget: memory.NewBudget(res, nil),
		Avail:  w.pool,
		Policy: policy,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return w
}

// add creates an app declaring memory MiB that really uses size MiB.
func (w *world) add(name string, priority float64, declared, size int64, mutate ...func(*config.App)) *app.App {
	ac := config.App{Name: name, Mode: config.ModeOnDemand, Priority: priority, Memory: config.Size(declared) * config.MiB,
		Idle: config.Duration(time.Hour), BootTimeout: config.Duration(time.Minute), Health: config.Health{Path: "/", Interval: config.Duration(time.Second)}}
	for _, m := range mutate {
		m(&ac)
	}
	a := app.New(ac, app.Deps{Clock: w.clock, Process: &proc{p: w.pool, name: name, size: size}, Gate: w.gate,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, nil)
	w.apps[name] = a
	w.order = append(w.order, a)
	return a
}

// running boots a with plenty of memory, as if it had been up a while.
func (w *world) running(t *testing.T, a *app.App) {
	t.Helper()
	saved := w.pool.total
	w.pool.total = 1 << 30
	if st := a.Wake(context.Background()); st != app.Starting {
		t.Fatalf("%s: wake = %s", a.Name(), st)
	}
	a.Wait()
	w.pool.total = saved
	w.pool.runs = nil
}

// squeeze sets total memory so that exactly avail MiB are free now.
func (w *world) squeeze(avail int64) {
	used := int64(0)
	for _, s := range w.pool.running {
		used += s
	}
	w.pool.total = used + avail
}

func assertLog(t *testing.T, w *world, want ...string) {
	t.Helper()
	if got := w.pool.log(); !slices.Equal(got, want) {
		t.Fatalf("runs = %v, want %v", got, want)
	}
}
