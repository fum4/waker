package app

import (
	"context"
	"time"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/memory"
)

// Snapshot is an app's full status for the control socket. Visitors never
// see it; they get only State.
type Snapshot struct {
	Name          string           `json:"name"`
	Listen        string           `json:"listen"`
	Upstream      string           `json:"upstream"`
	Mode          config.Mode      `json:"mode"`
	DeclaredMode  config.Mode      `json:"declared_mode"`
	Idle          config.Duration  `json:"idle"`
	DeclaredIdle  config.Duration  `json:"declared_idle"`
	OverrideSince *time.Time       `json:"override_since"`
	State         State            `json:"state"`
	Error         string           `json:"error,omitempty"`
	Priority      float64          `json:"priority"`
	Heartbeat     bool             `json:"heartbeat"`
	LastActivity  time.Time        `json:"last_activity"`
	Inflight      int              `json:"inflight"`
	IdleIn        *config.Duration `json:"idle_in,omitempty"`
	Evicted       *Eviction        `json:"evicted"`
	DependsOn     []string         `json:"depends_on,omitempty"`
	// KeptUpBy names the dependents keeping this app up (no idle stop, no
	// eviction).
	KeptUpBy []string `json:"kept_up_by,omitempty"`
	// Memory is filled in by the daemon, which owns the budget.
	Memory *memory.Breakdown `json:"memory,omitempty"`
}

// Snapshot probes health and returns the app's status. IdleIn is set only
// for an on-demand app that is up and not kept up by a dependent.
func (a *App) Snapshot(ctx context.Context) Snapshot {
	st := a.State(ctx)
	keptUpBy := a.KeptUpBy()
	a.mu.Lock()
	s := Snapshot{
		Name:         a.cfg.Name,
		Listen:       a.cfg.Listen,
		Upstream:     a.cfg.Upstream,
		Mode:         a.modeLocked(),
		DeclaredMode: a.cfg.Mode,
		Idle:         config.Duration(a.idleLocked()),
		DeclaredIdle: a.cfg.Idle,
		State:        st,
		Error:        a.bootErr,
		Priority:     a.cfg.Priority,
		Heartbeat:    a.cfg.HeartbeatEnabled(),
		Evicted:      a.evicted,
		DependsOn:    a.cfg.DependsOn,
		KeptUpBy:     keptUpBy,
	}
	if a.override != nil {
		since := a.override.Since
		s.OverrideSince = &since
	}
	a.mu.Unlock()
	s.LastActivity = a.activity.LastActivity()
	s.Inflight = a.activity.Inflight()
	if s.Mode == config.ModeOnDemand && st == Up && len(keptUpBy) == 0 {
		left := config.Duration(max(0, a.activity.IdleIn(s.Idle.D())))
		s.IdleIn = &left
	}
	return s
}
