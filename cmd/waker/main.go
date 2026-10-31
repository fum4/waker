// Command waker is a scale-to-zero front for self-hosted apps: the daemon
// (waker serve) and the CLI that controls it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// command is one waker subcommand.
type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) error
}

// exitCode is an error that only sets the exit code; the command has
// already printed what it means.
type exitCode int

func (c exitCode) Error() string { return fmt.Sprintf("exit %d", int(c)) }

// errUsage marks an error whose message is a usage hint; exit code 2.
var errUsage = errors.New("usage")

func commands() []command {
	return []command{
		{"serve", "run the waker daemon", runServe},
		{"status", "show every app (or one): mode, state, activity, priority", runStatus},
		{"mode", "set an app's runtime mode: mode <app> on|off|on-demand [--idle 10s]", runMode},
		{"reset", "drop an app's runtime mode override", runReset},
		{"check", "validate the config file and print a summary", runCheck},
		{"init", "write a waker.yaml for one app (prompts on a terminal; --yes takes flags)", func(a []string, o, e io.Writer) error { return aborted(runInit(a, o, e)) }},
		{"setup", "init (if there is no config yet), then install the service", func(a []string, o, e io.Writer) error { return aborted(runSetup(a, o, e)) }},
		{"service", "install|uninstall|status: run waker as a systemd or launchd service", runService},
		{"restart", "restart the service so it runs what waker.yaml says now (rewrites a stale unit first)", runRestart},
		{"update", "install the latest signed release (or --version) over this binary; --check only looks", runUpdate},
		{"version", "print the waker version", runVersion},
	}
}

// hinted are the commands that end with a "newer release" line on a
// terminal: not serve (it logs it), status (it shows it), update (it is
// the check), nor the first-run init and setup.
var hinted = map[string]bool{"check": true, "mode": true, "reset": true, "restart": true, "service": true, "version": true}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage(stderr)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	for _, c := range commands() {
		if c.name != args[0] {
			continue
		}
		hint := func(bool) {}
		if hinted[c.name] {
			hint = startHint(args[1:], stderr)
		}
		err := c.run(args[1:], stdout, stderr)
		hint(err == nil)
		var code exitCode
		switch {
		case errors.As(err, &code):
			return int(code)
		case err == nil:
			return 0
		case errors.Is(err, flag.ErrHelp):
			return 0
		case errors.Is(err, errUsage):
			fmt.Fprintf(stderr, "waker %s: %v\n", c.name, err)
			return 2
		default:
			fmt.Fprintf(stderr, "waker %s: %v\n", c.name, err)
			return 1
		}
	}
	fmt.Fprintf(stderr, "waker: unknown command %q\n\n", args[0])
	usage(stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: waker <command> [flags]")
	fmt.Fprintln(w)
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-8s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Config: --config <path>, else $WAKER_CONFIG, else ./waker.yaml.")
}

// newFlagSet builds a subcommand's flag set with the shared --config flag.
func newFlagSet(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := newBareFlagSet(name, stderr)
	cfg := fs.String("config", "", "config file (default $WAKER_CONFIG, else ./waker.yaml)")
	return fs, cfg
}

// newBareFlagSet builds a subcommand flag set without --config.
func newBareFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("waker "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}
