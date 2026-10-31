package main

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/service"
	"github.com/fum4/waker/internal/state"
)

// healGuard is the loop guard: serve rewrites its own unit at most once in
// this long, so a unit that renders differently on every start can't make
// it restart forever.
const healGuard = 10 * time.Minute

// errReexec is serve's exit when it rewrote its unit and must restart to
// run it: non-zero, so restart: on-failure brings it back too (75 is
// EX_TEMPFAIL).
var errReexec = exitCode(75)

// ownCgroup is this process's cgroup (tests fake it): how serve tells it
// is the service, not a run by hand.
var ownCgroup = func() string {
	b, _ := os.ReadFile("/proc/self/cgroup")
	return string(b)
}

// now is the clock the loop guard reads (tests move it).
var now = time.Now

// selfHeal makes the edited service: block apply whatever restarted
// waker: when serve runs as the service waker installed and the unit is
// stale, it rewrites the unit and reloads systemd, then reports whether
// serve must exit so the supervisor restarts it with the new command,
// environment or user. It leaves the unit alone, logging the command that
// applies it, when it can't or shouldn't rewrite it: a unit waker didn't
// write, a system unit while running as a non-root user, an app command
// missing from the new PATH, a heal in the last healGuard.
func selfHeal(cfg *config.Config, cfgPath string, log *slog.Logger) (reexec bool) {
	h, err := currentHost()
	if err != nil || h.OS != "linux" {
		return false // launchd caches the job: the startup drift warning says `waker restart`
	}
	sp, err := serviceSpec(cfg.Service, cfgPath, h)
	if err != nil || !service.InUnit(ownCgroup(), sp.System) {
		return false
	}
	d, unit, err := service.CheckDrift(h, sp)
	if err != nil || d != service.DriftStale {
		return false
	}
	skip := func(why string, args ...any) bool {
		log.Warn("service out of date, not rewritten: "+why,
			append(args, "unit", unit, "apply", restartLine(sp))...)
		return false
	}
	if sp.System && h.EUID != 0 {
		return skip("waker runs as " + h.User + ", and only root writes a system unit")
	}
	if ws := pathWarnings(cfg, sp.PATH, sp.WorkDir); len(ws) > 0 {
		return skip("the new service.path misses app commands", "problems", strings.Join(ws, "; "))
	}
	last, err := state.LoadSelfHeal(cfg.StateDir)
	if err != nil {
		return skip("can't read the loop guard", "err", err)
	}
	if t := now(); t.Sub(last) < healGuard {
		return skip("it was rewritten less than "+healGuard.String()+" ago (loop guard)", "last", last)
	}
	if err := state.SaveSelfHeal(cfg.StateDir, now()); err != nil {
		return skip("can't write the loop guard", "err", err)
	}
	plan, reexec, err := service.PlanHeal(h, sp)
	if err != nil {
		return skip("can't plan it", "err", err)
	}
	var steps bytes.Buffer
	if err := plan.Apply(serviceRunner, &steps, false); err != nil {
		return skip("rewriting it failed", "err", err, "steps", steps.String())
	}
	switch {
	case !reexec:
		log.Info("rewrote the service from waker.yaml; it applies now", "unit", unit)
	case sp.Restart == config.RestartNever:
		log.Warn("rewrote the service from waker.yaml; restart to apply it (restart: never, so waker can't restart itself)",
			"unit", unit, "apply", restartLine(sp))
		return false
	default:
		log.Info("rewrote the service from waker.yaml; exiting so the supervisor restarts waker with it", "unit", unit)
	}
	return reexec
}

// restartLine is the command that applies waker.yaml's service: block now.
func restartLine(sp service.Spec) string {
	argv := []string{"waker", "restart", "--config", sp.Config}
	if sp.System {
		argv = append([]string{"sudo"}, argv...)
	}
	return service.ShellJoin(argv)
}
