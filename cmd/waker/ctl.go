package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/control"
)

// systemSocket is where a system service listens; tests point it at a
// temp file.
var systemSocket = config.SystemControlSocket

// client resolves the control socket from the config and returns it with
// the path it chose.
func client(cfgFlag string) (*control.Client, string, error) {
	cfg, err := config.Load(config.ResolvePath(cfgFlag))
	if err != nil {
		return nil, "", err
	}
	path := controlSocket(cfg)
	return control.NewClient(path), path, nil
}

// controlSocket is the config's socket, unless the file leaves it to the
// default, nothing listens there and a system service's socket exists: a
// `--system` daemon has no XDG_RUNTIME_DIR to share with this shell.
func controlSocket(cfg *config.Config) string {
	if cfg.ControlSocketSet() || isSocket(cfg.ControlSocket) || !isSocket(systemSocket) {
		return cfg.ControlSocket
	}
	return systemSocket
}

func isSocket(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode()&os.ModeSocket != 0
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
	c, socket, err := client(*cfgFlag)
	if err != nil {
		return err
	}
	report, err := c.Status(context.Background())
	if err != nil {
		return err
	}
	snaps := report.Apps
	if len(pos) == 1 {
		snaps = filterApp(snaps, pos[0])
		if len(snaps) == 0 {
			return fmt.Errorf("no listening app %q", pos[0])
		}
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		report.Apps = snaps
		return enc.Encode(report)
	}
	now := time.Now()
	renderPolicy(stdout, report.Policy)
	for _, s := range snaps {
		fmt.Fprintln(stdout)
		renderStatus(stdout, s, now)
	}
	fmt.Fprintf(stdout, "\nvia %s\n", socket)
	cfgPath := config.ResolvePath(*cfgFlag)
	if cfg, err := config.Load(cfgPath); err == nil {
		reportDrift(cfg, cfgPath, stdout)
		if l := knownUpdate(cfg); l != "" {
			fmt.Fprintf(stdout, "\n%s\n", l)
		}
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
	return setMode(*cfgFlag, stdout, stderr, control.ModeRequest{App: pos[0], Mode: config.Mode(pos[1]), Idle: config.Duration(*idle)})
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
	return setMode(*cfgFlag, stdout, stderr, control.ModeRequest{App: pos[0], Reset: true})
}

func setMode(cfgFlag string, stdout, stderr io.Writer, req control.ModeRequest) error {
	c, _, err := client(cfgFlag)
	if err != nil {
		return err
	}
	snap, err := c.SetMode(context.Background(), req)
	if err != nil {
		return err
	}
	renderStatus(stdout, snap, time.Now())
	if w := offWarning(snap); w != "" {
		fmt.Fprintln(stderr, w)
	}
	return nil
}

// offWarning flags switching off an app its up dependents still need. It is
// allowed (the operator asked for it); they just lose what they need.
func offWarning(s app.Snapshot) string {
	if s.Mode != config.ModeOff || len(s.KeptUpBy) == 0 {
		return ""
	}
	return fmt.Sprintf("warning: %s is off while %s (up) still need it", s.Name, strings.Join(s.KeptUpBy, ", "))
}
