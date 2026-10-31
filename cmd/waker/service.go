package main

import (
	"fmt"
	"io"
	"os"

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
	dryRun := fs.Bool("dry-run", false, "print the unit and the commands; change nothing")
	force := fs.Bool("force", false, "overwrite a unit that install didn't write")
	sf := addServiceFlags(fs)
	r, err := newServiceRun([]string{"service", "install"}, fs, cfgFlag, sf, args)
	if err != nil {
		return err
	}
	plan, err := service.PlanInstall(r.h, r.sp)
	if err := r.planned(err, stderr); err != nil {
		return err
	}
	if *dryRun {
		r.printDryRun(plan, stdout, stderr)
		return nil
	}
	if err := r.ready(); err != nil {
		return err
	}
	return r.apply(plan, *force, "installed; check it with: waker service status", stdout)
}

func runServiceUninstall(args []string, stdout, stderr io.Writer) error {
	fs, cfgFlag := newFlagSet("service uninstall", stderr)
	system := fs.Bool("system", false, "remove the system service instead of the user one (default: service.system)")
	dryRun := fs.Bool("dry-run", false, "print the commands; change nothing")
	force := fs.Bool("force", false, "remove the unit even if install didn't write it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	// The scope is the file's unless --system says otherwise; a missing or
	// broken config never blocks an uninstall (the flag still works).
	if !flagGiven(fs, "system") {
		if cfg, err := config.Load(config.ResolvePath(*cfgFlag)); err == nil {
			*system = cfg.Service.System
		}
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
