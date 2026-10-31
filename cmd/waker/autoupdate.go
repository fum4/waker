package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/state"
	"github.com/fum4/waker/internal/update"
	"github.com/fum4/waker/internal/version"
)

// execve replaces serve with the binary at its path (tests record it).
var execve = syscall.Exec

// reexec runs exe in place of this process: same pid, args and env, so
// the supervisor sees the same service.
func reexec(exe string) error {
	return execve(exe, append([]string{exe}, os.Args[1:]...), os.Environ())
}

// resumeUpdate runs first in serve, before the config loads (the new
// binary might reject it). The new binary's first start counts itself in
// the marker; a second start means the first never reached serving, so it
// puts the old binary back and returns true: re-exec into it.
func resumeUpdate(exe string, log *slog.Logger) bool {
	m, ok := update.ReadMarker(exe)
	if !ok || m.RolledBack || m.To != runningTag() {
		return false // settleUpdate deals with it once the config is loaded
	}
	if m.Attempts == 0 {
		m.Attempts = 1
		if err := update.WriteMarker(exe, m); err != nil {
			log.Warn("update: can't count this start; no rollback if it fails", "err", err)
		}
		return false
	}
	if err := update.Rollback(exe); err != nil {
		log.Error("update: "+m.To+" didn't come up and the rollback failed; starting it anyway", "err", err)
		_ = update.ClearMarker(exe)
		return false
	}
	m.RolledBack = true
	if err := update.WriteMarker(exe, m); err != nil {
		log.Warn("update: can't record the rollback", "err", err)
	}
	log.Warn("update: "+m.To+" didn't come up; rolled back", "to", m.From)
	return true
}

// settleUpdate records what a marker left behind, once the config (and
// so the state dir) is known: a rollback blocks its tag; a marker naming
// another version than this one is stale (the binary was replaced by
// hand) and is dropped.
func settleUpdate(cfg *config.Config, exe string, log *slog.Logger) {
	m, ok := update.ReadMarker(exe)
	switch {
	case !ok:
		return
	case m.RolledBack:
		recordAttempt(cfg, m, state.UpdateRolledBack, m.To+" didn't reach serving", true)
		log.Warn("update: blocked after a rollback; update.auto won't install it again", "tag", m.To)
	case m.To != runningTag():
		recordAttempt(cfg, m, state.UpdateFailed, "the binary was replaced before "+m.To+" started", false)
	default:
		return // the new binary's own start: finishUpdate clears it
	}
	_ = update.ClearMarker(exe)
}

// finishUpdate runs once serve is serving: the new binary made it.
func finishUpdate(cfg *config.Config, exe string, log *slog.Logger) {
	m, ok := update.ReadMarker(exe)
	if !ok || m.RolledBack || m.To != runningTag() {
		return
	}
	if err := update.ClearMarker(exe); err != nil {
		log.Warn("update: can't clear the marker; the next start rolls back", "err", err)
		return
	}
	recordAttempt(cfg, m, state.UpdateApplied, "", false)
	log.Info("update: applied", "from", m.From, "to", m.To)
}

// autoUpdate is update.auto at serve's start, once a day at most: fetch
// and verify the latest release, install it (keeping .prev), dry-run it
// against the config, leave the marker and return true: re-exec into it.
func autoUpdate(cfg *config.Config, cfgPath, exe string, log *slog.Logger) bool {
	if !cfg.Update.Autos() || !update.Unattended() || !releaseBuild() {
		return false
	}
	rec, err := state.LoadUpdate(cfg.StateDir)
	if err != nil {
		rec = state.Update{}
	}
	t := now()
	if age := t.Sub(rec.AutoAt); !rec.AutoAt.IsZero() && age >= 0 && age < update.CheckEvery {
		return false
	}
	rec.AutoAt = t
	if err := state.SaveUpdate(cfg.StateDir, rec); err != nil {
		log.Warn("update: can't record the attempt, so no automatic update", "err", err)
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	src := releaseSource()
	tag, err := src.Latest(ctx)
	cur := update.ParseVersion(version.String())
	if err != nil || !cur.OlderThan(tag) || slices.Contains(rec.Blocked, tag.String()) {
		return false // silent: no answer, nothing newer, or a tag that failed here before
	}
	m := update.Marker{From: cur.String(), To: tag.String()}
	fail := func(outcome, detail string, block bool) bool {
		recordAttempt(cfg, m, outcome, detail, block)
		log.Warn("update: "+outcome, "to", m.To, "detail", detail)
		return false
	}
	rel, err := src.Fetch(ctx, tag, update.Asset())
	if err != nil {
		return fail(state.UpdateFailed, err.Error(), false)
	}
	switch err := update.Install(ctx, rel, exe); {
	case errors.Is(err, update.ErrCantWrite):
		return fail(state.UpdateSkipped, "run: "+sudoUpdateLine(exe, tag), false)
	case errors.Is(err, update.ErrUnchanged), errors.Is(err, update.ErrNotRecorded):
	case err != nil:
		return fail(state.UpdateFailed, err.Error(), false)
	}
	if out, err := exec.CommandContext(ctx, exe, "check", "--config", cfgPath).CombinedOutput(); err != nil {
		detail := m.To + " rejects the config: " + strings.TrimSpace(string(out))
		if rerr := update.Rollback(exe); rerr != nil {
			detail += "; rollback failed: " + rerr.Error()
		}
		return fail(state.UpdateRolledBack, detail, true)
	}
	if err := update.WriteMarker(exe, m); err != nil {
		// Without a marker a crash couldn't roll back: don't run it yet.
		return fail(state.UpdateFailed, "can't write the marker: "+err.Error()+"; installed, applies at the next restart", false)
	}
	recordAttempt(cfg, m, state.UpdateInstalled, "", false)
	log.Info("update: installed; restarting into it", "from", m.From, "to", m.To)
	return true
}

// recordAttempt writes the attempt to update.json (best effort: the log
// has it too), blocking its tag when asked.
func recordAttempt(cfg *config.Config, m update.Marker, outcome, detail string, block bool) {
	rec, err := state.LoadUpdate(cfg.StateDir)
	if err != nil {
		rec = state.Update{}
	}
	rec.Last = &state.Attempt{At: now(), From: m.From, To: m.To, Outcome: outcome, Detail: detail}
	if block && !slices.Contains(rec.Blocked, m.To) {
		rec.Blocked = append(rec.Blocked, m.To)
	}
	_ = state.SaveUpdate(cfg.StateDir, rec)
}

// runningTag is this binary's version as a tag ("v0.4.0"), or "".
func runningTag() string {
	v := update.ParseVersion(version.String())
	if !v.Known || v.Pre {
		return ""
	}
	return v.Core.String()
}
