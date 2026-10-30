package daemon

import (
	"slices"
	"testing"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/clock"
	"github.com/fum4/waker/internal/config"
)

func TestWireDependenciesStopsDependentsFirst(t *testing.T) {
	deps := map[string][]string{"web": {"api"}, "api": {"db", "cache"}, "cache": {"db"}}
	var all []*app.App
	for _, n := range []string{"db", "cache", "web", "api", "solo"} {
		all = append(all, app.New(config.App{Name: n, DependsOn: deps[n]}, app.Deps{Clock: clock.Real{}}, nil))
	}
	order := wireDependencies(all)
	pos := map[string]int{}
	for i, a := range order {
		pos[a.Name()] = i
	}
	if len(order) != len(all) {
		t.Fatalf("order has %d apps, want %d", len(order), len(all))
	}
	for dependent, ds := range deps {
		for _, d := range ds {
			if pos[dependent] > pos[d] {
				t.Errorf("%s stops after its dependency %s", dependent, d)
			}
		}
	}
	if !slices.Contains(names(order), "solo") {
		t.Fatal("an app without dependencies is missing")
	}
}

func names(apps []*app.App) []string {
	var out []string
	for _, a := range apps {
		out = append(out, a.Name())
	}
	return out
}
