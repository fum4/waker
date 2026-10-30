// Package evict admits boots by memory and, when memory is short, stops
// lower-priority apps to make room — never pointlessly: if evicting every
// candidate still couldn't cover the shortfall, nothing is stopped.
package evict

import (
	"context"
	"log/slog"
	"slices"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/memory"
)

// Gate implements app.Gate over a set of apps.
type Gate struct {
	// Apps returns every app the waker manages.
	Apps   func() []*app.App
	Budget *memory.Budget
	Avail  memory.Available
	Policy config.Eviction
	Log    *slog.Logger
}

// Admit lets a boot start when enough memory is available, or when a plan
// of evictions could free the shortfall; otherwise the app is busy.
func (g *Gate) Admit(a *app.App) (func() bool, bool) {
	bd := g.Budget.Breakdown(a.Config())
	avail, err := g.Avail.AvailableMiB()
	if err != nil {
		g.Log.Warn("available memory unreadable; booting anyway", "app", a.Name(), "err", err)
		return nil, true
	}
	if avail >= bd.RequiredMiB {
		return nil, true
	}
	plan := g.Plan(a, bd.RequiredMiB-avail)
	if len(plan) == 0 {
		g.Log.Warn("boot refused: not enough memory, nothing to evict", "app", a.Name(),
			"available_mib", avail, "required_mib", bd.RequiredMiB, "need_mib", bd.NeedMiB, "headroom_mib", bd.HeadroomMiB)
		return nil, false
	}
	return func() bool { return g.makeRoom(a, plan, bd.RequiredMiB) }, true
}

// Plan returns the apps to stop, in order, so booting can start — or nil
// when even all of them together would not free shortfall MiB.
func (g *Gate) Plan(booting *app.App, shortfall int64) []*app.App {
	var plan []*app.App
	var frees int64
	for _, v := range g.Apps() {
		if g.Evictable(v, booting) {
			plan = append(plan, v)
			frees += g.Budget.Breakdown(v.Config()).NeedMiB
		}
	}
	if frees < shortfall {
		return nil
	}
	slices.SortStableFunc(plan, g.compare)
	return plan
}

// Evictable reports whether v may be stopped for booting: v was started by
// this waker, is on-demand, up, idle (nothing in flight), not booting, not
// protected, and its priority is below (or, by policy, equal to) booting's.
func (g *Gate) Evictable(v, booting *app.App) bool {
	if v == booting || !g.Policy.Enabled || !v.Config().IsEvictable() || slices.Contains(g.Policy.Protect, v.Name()) {
		return false
	}
	switch g.Policy.Policy {
	case config.EvictLower:
		if v.Priority() >= booting.Priority() {
			return false
		}
	case config.EvictLowerOrEqual:
		if v.Priority() > booting.Priority() {
			return false
		}
	default:
		return false
	}
	return v.Started() && v.Mode() == config.ModeOnDemand && !v.Booting() &&
		v.Activity().Inflight() == 0 && v.Healthy(context.Background())
}

func (g *Gate) compare(a, b *app.App) int {
	byPriority := func() int { return cmpFloat(a.Priority(), b.Priority()) }
	byIdle := func() int { return a.Activity().LastActivity().Compare(b.Activity().LastActivity()) }
	first, second := byPriority, byIdle
	if g.Policy.Order == config.OrderIdleThenPriority {
		first, second = byIdle, byPriority
	}
	if c := first(); c != 0 {
		return c
	}
	return second()
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// makeRoom evicts along the plan, one at a time, re-measuring after each,
// and skips a victim that stopped qualifying meanwhile.
func (g *Gate) makeRoom(booting *app.App, plan []*app.App, required int64) bool {
	for _, v := range plan {
		if avail, err := g.Avail.AvailableMiB(); err == nil && avail >= required {
			return true
		}
		if !g.Evictable(v, booting) {
			continue
		}
		g.Log.Warn("evicting", "app", v.Name(), "priority", v.Priority(), "for", booting.Name(), "for_priority", booting.Priority(), "required_mib", required)
		v.Evict(booting)
	}
	avail, err := g.Avail.AvailableMiB()
	if err != nil || avail < required {
		g.Log.Warn("still short after evicting: busy", "app", booting.Name(), "available_mib", avail, "required_mib", required)
		return false
	}
	return true
}
