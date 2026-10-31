package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/daemon"
	"github.com/fum4/waker/internal/version"
)

func runServe(args []string, _, stderr io.Writer) error {
	fs, cfgFlag := newFlagSet("serve", stderr)
	jsonLogs := fs.Bool("log-json", false, "log JSON lines instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	var h slog.Handler = slog.NewTextHandler(stderr, nil)
	if *jsonLogs {
		h = slog.NewJSONHandler(stderr, nil)
	}
	log := slog.New(h)
	// An update in flight is resumed or rolled back before anything else:
	// the new binary may be what can't load the config.
	exe, exeErr := binaryPath()
	if exeErr == nil && resumeUpdate(exe, log) {
		return reexec(exe)
	}
	cfgPath := config.ResolvePath(*cfgFlag)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	log.Info("waker starting", "version", version.String(), "apps", len(cfg.Apps))
	if exeErr == nil {
		settleUpdate(cfg, exe, log)
		if autoUpdate(cfg, cfgPath, exe, log) {
			return reexec(exe)
		}
	}
	// Before binding: a re-exec leaves nothing half-started.
	switch selfHeal(cfg, cfgPath, log) {
	case healReexec:
		return errReexec
	case healNone:
		if msg := driftMessage(cfg, cfgPath); msg != "" {
			log.Warn("service out of date", "detail", msg)
		}
	}
	d, err := daemon.New(cfg, log)
	if err != nil {
		return err
	}
	if exeErr == nil {
		d.OnReady(func() { finishUpdate(cfg, exe, log) })
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go watchUpdates(ctx, cfg, log)
	return d.Run(ctx)
}
