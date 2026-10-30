package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/service"
)

// errNotRunning makes `waker service status` exit 3 (the LSB "not running"
// code systemctl uses) when no service is running.
var errNotRunning = exitCode(3)

// serviceRunner runs systemctl and launchctl; TestMain swaps in one that
// refuses, so no test can touch the machine's real services.
var serviceRunner service.Runner = service.ExecRunner{}

func runService(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: service install|uninstall|status [flags]", errUsage)
	}
	switch args[0] {
	case "install":
		return runServiceInstall(args[1:], stdout, stderr)
	case "uninstall":
		return runServiceUninstall(args[1:], stdout, stderr)
	case "status":
		return runServiceStatus(args[1:], stdout, stderr)
	}
	return fmt.Errorf("%w: unknown service command %q (want install, uninstall or status)", errUsage, args[0])
}

func runServiceInstall(args []string, stdout, stderr io.Writer) error {
	fs, cfgFlag := newFlagSet("service install", stderr)
	system := fs.Bool("system", false, "install a system service (root) instead of a user one")
	dryRun := fs.Bool("dry-run", false, "print the unit and the commands; change nothing")
	runAs := fs.String("run-as", "", "--system: the account waker runs as (default $SUDO_USER, else you)")
	path := fs.String("path", os.Getenv("PATH"), "the service's PATH (default: this shell's)")
	force := fs.Bool("force", false, "overwrite a unit that install didn't write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	cfgPath, err := filepath.Abs(config.ResolvePath(*cfgFlag))
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find the waker binary: %w", err)
	}
	h, err := currentHost()
	if err != nil {
		return err
	}
	sp := service.Spec{Binary: bin, Config: cfgPath, WorkDir: filepath.Dir(cfgPath), PATH: *path, System: *system, RunAs: *runAs}
	if sp.System && sp.RunAs == "" {
		sp.RunAs = os.Getenv("SUDO_USER")
		if sp.RunAs == "" {
			sp.RunAs = h.User
		}
	}
	plan, err := service.PlanInstall(h, sp)
	if errors.Is(err, service.ErrUnsupported) {
		fmt.Fprintln(stderr, service.ManualSteps(sp))
	}
	if err != nil {
		return err
	}
	for _, w := range pathWarnings(cfg, sp.PATH, sp.WorkDir) {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if *dryRun {
		plan.Print(stdout)
		printNotes(stdout, plan.Notes)
		return nil
	}
	if sp.System && h.EUID != 0 {
		argv := []string{"sudo", bin, "service", "install", "--system", "--config", cfgPath, "--run-as", sp.RunAs, "--path", sp.PATH}
		return fmt.Errorf("a system service needs root; run:\n  %s", service.ShellJoin(argv))
	}
	if !sp.System && h.EUID == 0 && h.OS == "linux" {
		return fmt.Errorf("%w: you are root; a user service would run in root's session: use --system", errUsage)
	}
	if err := plan.Apply(serviceRunner, stdout, *force); err != nil {
		return err
	}
	printNotes(stdout, plan.Notes)
	fmt.Fprintln(stdout, "installed; check it with: waker service status")
	return nil
}

func runServiceUninstall(args []string, stdout, stderr io.Writer) error {
	fs := newBareFlagSet("service uninstall", stderr)
	system := fs.Bool("system", false, "remove the system service instead of the user one")
	dryRun := fs.Bool("dry-run", false, "print the commands; change nothing")
	force := fs.Bool("force", false, "remove the unit even if install didn't write it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	h, err := currentHost()
	if err != nil {
		return err
	}
	plan, err := service.PlanUninstall(h, *system)
	if err != nil {
		return err
	}
	if *dryRun {
		plan.Print(stdout)
		return nil
	}
	if *system && h.EUID != 0 {
		bin, _ := os.Executable()
		return fmt.Errorf("a system service needs root; run:\n  %s", service.ShellJoin([]string{"sudo", bin, "service", "uninstall", "--system"}))
	}
	if err := plan.Apply(serviceRunner, stdout, *force); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "uninstalled")
	return nil
}

func runServiceStatus(args []string, stdout, stderr io.Writer) error {
	fs := newBareFlagSet("service status", stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	h, err := currentHost()
	if err != nil {
		return err
	}
	sts, err := service.Statuses(h, serviceRunner)
	if err != nil {
		return err
	}
	running := false
	for _, st := range sts {
		st.Print(stdout)
		running = running || st.Running
	}
	if !running {
		return errNotRunning
	}
	return nil
}

func printNotes(w io.Writer, notes []string) {
	for _, n := range notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
}
