package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/control"
)

// client resolves the control socket from the config.
func client(cfgFlag string) (*control.Client, error) {
	cfg, err := config.Load(config.ResolvePath(cfgFlag))
	if err != nil {
		return nil, err
	}
	return control.NewClient(cfg.ControlSocket), nil
}

// parseInterspersed lets flags follow positional arguments
// (`waker mode shop on-demand --idle 10s`).
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func runStatus(args []string, stdout, stderr io.Writer) error {
	fs, cfgFlag := newFlagSet("status", stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fmt.Errorf("%w: status [<app>] [--json]", errUsage)
	}
	c, err := client(*cfgFlag)
	if err != nil {
		return err
	}
	snaps, err := c.Status(context.Background())
	if err != nil {
		return err
	}
	if len(pos) == 1 {
		snaps = filterApp(snaps, pos[0])
		if len(snaps) == 0 {
			return fmt.Errorf("no listening app %q", pos[0])
		}
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(snaps)
	}
	now := time.Now()
	for i, s := range snaps {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		renderStatus(stdout, s, now)
	}
	return nil
}

func filterApp(snaps []app.Snapshot, name string) []app.Snapshot {
	for _, s := range snaps {
		if s.Name == name {
			return []app.Snapshot{s}
		}
	}
	return nil
}

func runMode(args []string, stdout, stderr io.Writer) error {
	fs, cfgFlag := newFlagSet("mode", stderr)
	idle := fs.Duration("idle", 0, "idle override for on-demand, e.g. 10s, 5m")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fmt.Errorf("%w: mode <app> on|off|on-demand [--idle 10s]", errUsage)
	}
	return setMode(*cfgFlag, stdout, control.ModeRequest{App: pos[0], Mode: config.Mode(pos[1]), Idle: config.Duration(*idle)})
}

func runReset(args []string, stdout, stderr io.Writer) error {
	fs, cfgFlag := newFlagSet("reset", stderr)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("%w: reset <app>", errUsage)
	}
	return setMode(*cfgFlag, stdout, control.ModeRequest{App: pos[0], Reset: true})
}

func setMode(cfgFlag string, stdout io.Writer, req control.ModeRequest) error {
	c, err := client(cfgFlag)
	if err != nil {
		return err
	}
	snap, err := c.SetMode(context.Background(), req)
	if err != nil {
		return err
	}
	renderStatus(stdout, snap, time.Now())
	return nil
}
