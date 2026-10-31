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

// stderrTerminal reports whether the hint may go to w (tests fake a TTY).
var stderrTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && prompt.IsTerminal(f)
}

// updateCheck is cfg's background release check; ok is false when the
// config turns it off, WAKER_NO_UPDATE_CHECK is set or this is a dev
// build (no release is newer or older than it).
func updateCheck(cfg *config.Config) (c update.Check, ok bool) {
	if !cfg.Update.Checks() || !update.Unattended() || !update.ParseVersion(version.String()).Known {
		return update.Check{}, false
	}
	return update.Check{Source: releaseSource(), StateDir: cfg.StateDir}, true
}

// availableLine is the "newer release" line for cfg, or "".
func availableLine(ctx context.Context, cfg *config.Config) string {
	c, ok := updateCheck(cfg)
	if !ok {
		return ""
	}
	tag, ok := c.Latest(ctx)
	if !ok {
		return ""
	}
	return update.Available(update.ParseVersion(version.String()), tag)
}

// startHint looks for a newer release while a CLI command runs; the
// returned func prints one dim line on stderr if there is one and the
// command succeeded. Off the terminal it does nothing: scripts and pipes
// never see it.
func startHint(args []string, stderr io.Writer) func(succeeded bool) {
	if !stderrTerminal(stderr) {
		return func(bool) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	line := make(chan string, 1)
	go func() {
		cfg, err := config.Load(config.ResolvePath(configArg(args)))
		if err != nil {
			line <- ""
			return
		}
		line <- availableLine(ctx, cfg)
	}()
	return func(succeeded bool) {
		defer cancel()
		if !succeeded {
			return
		}
		if l := <-line; l != "" {
			io.WriteString(stderr, dim(l)+"\n")
		}
	}
}

// dim greys s out on a terminal, unless NO_COLOR is set.
func dim(s string) string {
	if os.Getenv("NO_COLOR") != "" {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
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

// watchUpdates logs a newer release at serve's start, then once a day,
// until ctx ends. It never installs anything.
func watchUpdates(ctx context.Context, cfg *config.Config, log *slog.Logger) {
	if _, ok := updateCheck(cfg); !ok {
		return
	}
	for {
		if l := availableLine(ctx, cfg); l != "" {
			log.Info("update available", "detail", l)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(update.CheckEvery):
		}
	}
}
