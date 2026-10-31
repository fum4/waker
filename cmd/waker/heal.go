package main

import (
	"bytes"
	"log/slog"
	"os"
	"slices"
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

// now is the clock the loop guard reads (tests move it).
var now = time.Now

// healResult is what serve's self-heal did.
type healResult int

const (
	// healNone: nothing to heal here (no stale unit of ours, or not the
	// service's own process); serve warns about drift as usual.
	healNone healResult = iota
	// healLogged: it healed in place, or logged why not with the command
	// that applies it; nothing more to warn about.
	healLogged
	// healReexec: it rewrote the unit, and serve must exit so the
	// supervisor restarts it with the new command, environment or user.
	healReexec
)

// selfHeal makes the edited service: block apply whatever restarted
// waker: when serve is the main process of the unit waker installed and
// that unit is stale, it rewrites the unit and reloads systemd, and says
// when serve must exit to run it. It leaves the unit alone, logging the
// command that applies it, when it can't do that safely: a system unit
// while running as a non-root user, an app command the new PATH loses, a
// heal in the last healGuard. A run by hand, a unit waker didn't write and
// launchd are never touched.
func selfHeal(cfg *config.Config, cfgPath string, log *slog.Logger) healResult {
	h, err := currentHost()
	if err != nil || h.OS != "linux" {
		return healNone // launchd caches the job: the drift warning says `waker restart`
	}
	sp, err := serviceSpec(cfg.Service, cfgPath, h)
	if err != nil {
		return healNone
	}
	d, unit, err := service.CheckDrift(h, sp)
	if err != nil || d != service.DriftStale || !service.IsMainProcess(h, sp.System, serviceRunner, os.Getpid()) {
		return healNone
	}
	skip := func(why string, args ...any) healResult {
		log.Warn("service out of date, not rewritten: "+why,
			append(args, "unit", unit, "apply", restartLine(sp))...)
		return healLogged
	}
	if sp.System && h.EUID != 0 {
		return skip("waker runs as " + h.User + ", and only root writes a system unit")
	}
	if lost := lostCommands(cfg, sp); len(lost) > 0 {
		return skip("the new service.path loses app commands", "problems", strings.Join(lost, "; "))
	}
	last, err := state.LoadSelfHeal(cfg.StateDir)
	if err != nil {
		return skip("can't read the loop guard", "err", err)
	}
	// A last heal in the future (the clock stepped back) doesn't guard.
	if since := now().Sub(last); since >= 0 && since < healGuard {
		return skip("it was rewritten less than "+healGuard.String()+" ago (loop guard)", "last", last)
	}
	if err := state.SaveSelfHeal(cfg.StateDir, now()); err != nil {
		return skip("can't write the loop guard", "err", err)
	}
	heal, err := service.PlanHeal(h, sp)
	if err != nil {
		return skip("can't plan it", "err", err)
	}
	var steps bytes.Buffer
	if err := heal.Plan.Apply(serviceRunner, &steps, false); err != nil {
		restore(heal, log)
		return skip("rewriting it failed; the old unit is back", "err", err, "steps", steps.String())
	}
	switch {
	case !heal.Reexec:
		log.Info("rewrote the service from waker.yaml; it applies now", "unit", unit)
	case sp.Restart == config.RestartNever:
		// The file stays rewritten, so the next start (a reboot) runs it.
		log.Warn("rewrote the service from waker.yaml; restart to apply it (restart: never, so waker can't restart itself)",
			"unit", unit, "apply", restartLine(sp))
	default:
		log.Info("rewrote the service from waker.yaml; exiting so the supervisor restarts waker with it", "unit", unit)
		return healReexec
	}
	return healLogged
}

// lostCommands is the app commands the new service PATH misses that the
// running process's PATH (the old unit's) finds: a command already
// missing never blocks an unrelated edit.
func lostCommands(cfg *config.Config, sp service.Spec) []string {
	before := pathWarnings(cfg, os.Getenv("PATH"), sp.WorkDir)
	var lost []string
	for _, w := range pathWarnings(cfg, sp.PATH, sp.WorkDir) {
		if !slices.Contains(before, w) {
			lost = append(lost, w)
		}
	}
	return lost
}

// restore puts the old unit back after a failed heal and reloads systemd
// (best effort: the heal's own error is the one reported).
func restore(heal service.Heal, log *slog.Logger) {
	if err := os.WriteFile(heal.Plan.Unit, []byte(heal.Old), 0o644); err != nil {
		log.Error("can't put the old unit back", "unit", heal.Plan.Unit, "err", err)
		return
	}
	if len(heal.Plan.Post) > 0 {
		_, _ = serviceRunner.Run(heal.Plan.Post[0].Argv...) // daemon-reload
	}
}

// restartLine is the command that applies waker.yaml's service: block now.
func restartLine(sp service.Spec) string {
	argv := []string{"waker", "restart", "--config", sp.Config}
	if sp.System {
		argv = append([]string{"sudo"}, argv...)
	}
	return service.ShellJoin(argv)
}
