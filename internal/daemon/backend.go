package daemon

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/control"
	"github.com/fum4/waker/internal/state"
	"github.com/fum4/waker/internal/version"
)

// Status implements control.Backend.
func (d *Daemon) Status(ctx context.Context) control.Report {
	r := control.Report{Version: version.String(), Policy: d.policy(), Apps: make([]app.Snapshot, 0, len(d.listening))}
	if u, err := state.LoadUpdate(d.cfg.StateDir); err == nil {
		r.Update = &u
	}
	for _, a := range d.listening {
		r.Apps = append(r.Apps, d.withMemory(a, a.Snapshot(ctx)))
	}
	return r
}

// Health implements control.Backend.
func (d *Daemon) Health() control.Health {
	h := control.Health{Listeners: []string{}, Skipped: maps.Clone(d.skipped)}
	for _, a := range d.listening {
		h.Listeners = append(h.Listeners, a.Name())
	}
	slices.Sort(h.Listeners)
	return h
}

// SetMode implements control.Backend.
func (d *Daemon) SetMode(name string, mode *config.Mode, idle time.Duration) (app.Snapshot, error) {
	var target *app.App
	names := make([]string, 0, len(d.listening))
	for _, a := range d.listening {
		names = append(names, a.Name())
		if a.Name() == name {
			target = a
		}
	}
	if target == nil {
		msg := fmt.Sprintf("no listening app '%s'", name)
		if why, ok := d.skipped[name]; ok {
			msg += " (" + why + ")"
		}
		if len(names) == 0 {
			names = []string{"none"}
		}
		return app.Snapshot{}, &control.NotFoundError{Msg: msg + "; apps: " + strings.Join(names, ", ")}
	}
	if err := target.SetMode(mode, idle); err != nil {
		return app.Snapshot{}, err
	}
	d.log.Info("mode set", "app", name, "mode", target.Mode(), "idle", target.Idle(), "override", target.Override() != nil)
	return d.withMemory(target, target.Snapshot(context.Background())), nil
}

// loadOverrides reads modes.json. A corrupt file is logged and ignored, so
// a bad write never becomes a crash loop; overrides for unknown apps or
// with invalid values are dropped (and vanish on the next save).
func (d *Daemon) loadOverrides() map[string]state.Override {
	all, err := state.LoadOverrides(d.cfg.StateDir)
	if err != nil {
		d.log.Error("overrides unreadable, starting with the declared modes", "err", err)
		return map[string]state.Override{}
	}
	known := map[string]bool{}
	for _, a := range d.cfg.Apps {
		known[a.Name] = true
	}
	for name, o := range all {
		if !known[name] {
			d.log.Warn("dropping the override of an app no longer configured", "app", name)
			delete(all, name)
		} else if p := o.Problem(); p != "" {
			d.log.Warn("dropping an invalid override", "app", name, "problem", p)
			delete(all, name)
		}
	}
	return all
}

// persist records one app's override and rewrites modes.json.
func (d *Daemon) persist(name string, o *state.Override) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if o == nil {
		delete(d.overrides, name)
	} else {
		d.overrides[name] = *o
	}
	return state.SaveOverrides(d.cfg.StateDir, d.overrides)
}
