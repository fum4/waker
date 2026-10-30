package daemon

import (
	"slices"

	"github.com/fum4/waker/internal/app"
)

// wireDependencies resolves every app's depends_on over all apps, listening
// or not (validation guarantees known names and no cycles), and returns the
// apps dependents-first: the order shutdown stops them in.
func wireDependencies(all []*app.App) []*app.App {
	byName := map[string]*app.App{}
	for _, a := range all {
		byName[a.Name()] = a
	}
	depsOf := func(a *app.App) []*app.App {
		var deps []*app.App
		for _, n := range a.Config().DependsOn {
			if d, ok := byName[n]; ok {
				deps = append(deps, d)
			}
		}
		return deps
	}
	for _, a := range all {
		a.SetDependencies(depsOf(a))
	}
	// Depth-first post-order puts every dependency before its dependents.
	var order []*app.App
	seen := map[*app.App]bool{}
	var visit func(*app.App)
	visit = func(a *app.App) {
		if seen[a] {
			return
		}
		seen[a] = true
		for _, d := range depsOf(a) {
			visit(d)
		}
		order = append(order, a)
	}
	for _, a := range all {
		visit(a)
	}
	slices.Reverse(order)
	return order
}

// ticked returns the apps the idle loop manages: every listening app, plus
// a dependency whose listener didn't bind but that a dependent's boot started.
func (d *Daemon) ticked() []*app.App {
	apps := slices.Clone(d.listening)
	for _, a := range d.all {
		if !slices.Contains(d.listening, a) && a.Started() {
			apps = append(apps, a)
		}
	}
	return apps
}
