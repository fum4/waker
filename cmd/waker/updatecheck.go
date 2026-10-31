package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/prompt"
	"github.com/fum4/waker/internal/update"
	"github.com/fum4/waker/internal/version"
)

const (
	// hintTimeout bounds the CLI's request: a command waits for it, at
	// most once a day.
	hintTimeout = 2 * time.Second
	// serveRecheck is how often serve re-reads the shared record (asking
	// the release home only once it is a day old), so a CLI refresh never
	// delays serve's by a day.
	serveRecheck = time.Hour
)

// stderrTerminal reports whether the hint may go to w (tests fake a TTY).
var stderrTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && prompt.IsTerminal(f)
}

// updateCheck is cfg's background release check; ok is false when the
// config turns it off, WAKER_NO_UPDATE_CHECK is set or this isn't a clean
// release build (a dev, pseudo-version or +dirty build has no release).
func updateCheck(cfg *config.Config) (c update.Check, ok bool) {
	s := version.String()
	v := update.ParseVersion(s)
	if !cfg.Update.Checks() || !update.Unattended() || !v.Known || v.Pre || strings.Contains(s, "+") {
		return update.Check{}, false
	}
	return update.Check{Source: releaseSource(), StateDir: cfg.StateDir}, true
}

// available is the "newer release" line for tag, or "".
func available(tag update.Tag, ok bool) string {
	if !ok {
		return ""
	}
	return update.Available(update.ParseVersion(version.String()), tag)
}

// knownUpdate is the line for the last recorded check, without a request
// (status: it must stay fast and offline).
func knownUpdate(cfg *config.Config) string {
	c, ok := updateCheck(cfg)
	if !ok {
		return ""
	}
	return available(c.Known())
}

// startHint looks for a newer release while a CLI command runs; the
// returned func prints one dim line on stderr if there is one and the
// command succeeded. It does nothing off a terminal (scripts and pipes
// never see it) or as root (sudo must not create or own the state dir).
func startHint(args []string, stderr io.Writer) func(succeeded bool) {
	if h, err := currentHost(); err != nil || h.EUID == 0 || !stderrTerminal(stderr) {
		return func(bool) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	line := make(chan string, 1)
	go func() {
		cfg, err := config.Load(config.ResolvePath(configArg(args)))
		c, ok := update.Check{}, false
		if err == nil {
			c, ok = updateCheck(cfg)
		}
		if !ok {
			line <- ""
			return
		}
		c.Timeout = hintTimeout
		line <- available(c.Latest(ctx))
	}()
	return func(succeeded bool) {
		if !succeeded {
			cancel() // a cancelled check records nothing
		}
		l := <-line // never leave it running: it may still be writing update.json
		cancel()
		if succeeded && l != "" {
			io.WriteString(stderr, prompt.Dim(l)+"\n")
		}
	}
}

// configArg finds --config's value in a command's raw arguments, before
// its own flag set parses them ("" = the default).
func configArg(args []string) string {
	for i, a := range args {
		if a == "--" {
			break
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") || name != "config" {
			continue
		}
		if hasVal {
			return val
		}
		if i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// watchUpdates logs a newer release at serve's start and whenever the
// latest tag changes, until ctx ends. It never installs anything.
func watchUpdates(ctx context.Context, cfg *config.Config, log *slog.Logger) {
	c, ok := updateCheck(cfg)
	if !ok {
		return
	}
	var logged update.Tag
	for {
		if t, ok := c.Latest(ctx); ok && t != logged {
			logged = t
			if l := available(t, true); l != "" {
				log.Info("update available", "detail", l)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(serveRecheck):
		}
	}
}
