package evict

import (
	"testing"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
)

func TestADependencyWithAnUpDependentIsNotEvictable(t *testing.T) {
	w := newWorld(config.Eviction{})
	top := w.add("top", 10, 1000, 1000)
	db := w.add("db", 1, 2000, 2000)
	api := w.add("api", 1, 500, 500)
	api.SetDependencies([]*app.App{db})
	w.running(t, api) // boots db first
	db.Wait()
	if !api.Started() || !db.Started() {
		t.Fatal("api and db not both up")
	}
	if w.gate.Evictable(db, top) {
		t.Fatal("db evictable while api is up")
	}
	w.squeeze(0)
	if plan := w.gate.Plan(top, 2000); len(plan) != 0 {
		t.Fatalf("plan = %v: api alone cannot free 2000 MiB, db is kept up", names(plan))
	}
	api.Stop("test")
	if !w.gate.Evictable(db, top) {
		t.Fatal("db not evictable once api stopped")
	}
}
