package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/service"
)

// serviceRun is what `service install` and `restart` share: the config,
// the service it describes with the flags applied, and the flags to save
// once the command has succeeded.
type serviceRun struct {
	verb    []string // the subcommand, for the sudo line
	args    []string // as given, for the sudo line
	cfg     *config.Config
	cfgPath string
	h       service.Host
	sp      service.Spec
	save    *pendingSave
	saveErr error // why the flags can't be saved (a managed file)
	noSave  bool
}

// newServiceRun parses fs (the service flags already on it), loads the
// config and resolves the service.
func newServiceRun(verb []string, fs *flag.FlagSet, cfgFlag *string, sf *serviceFlags, args []string) (*serviceRun, error) {
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	r := &serviceRun{verb: verb, args: args, noSave: *sf.noSave}
	var err error
	if r.cfgPath, err = filepath.Abs(config.ResolvePath(*cfgFlag)); err != nil {
		return nil, err
	}
	if r.cfg, err = config.Load(r.cfgPath); err != nil {
		return nil, err
	}
	settings, err := sf.apply(r.cfg.Service)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUsage, err)
	}
	if r.h, err = currentHost(); err != nil {
		return nil, err
	}
	if r.sp, err = serviceSpec(settings, r.cfgPath, r.h); err != nil {
		return nil, err
	}
	if !r.noSave {
		r.save, r.saveErr = prepareSave(r.cfgPath, sf.changedKeys(r.cfg.Service, settings))
	}
	return r, nil
}

// planned reports a plan error, printing the manual steps where no
// service manager is supported, then warns about start commands missing
// from the service's PATH.
func (r *serviceRun) planned(err error, stderr io.Writer) error {
	if errors.Is(err, service.ErrUnsupported) {
		fmt.Fprintln(stderr, service.ManualSteps(r.sp))
	}
	if err != nil {
		return err
	}
	for _, w := range pathWarnings(r.cfg, r.sp.PATH, r.sp.WorkDir) {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	return nil
}

// printDryRun prints the plan and what the real run would save (or why
// it refuses).
func (r *serviceRun) printDryRun(plan service.Plan, stdout, stderr io.Writer) {
	plan.Print(stdout)
	printNotes(stdout, plan.Notes)
	if r.saveErr != nil {
		fmt.Fprintf(stderr, "warning: the real run refuses: %v\n", r.saveErr)
	} else if r.save != nil {
		fmt.Fprintf(stdout, "note: would save %s to %s\n", r.save.describe(), r.save.path)
	}
}

// ready refuses what must not run: flags a managed file can't take, a
// system service without root (with the sudo line), a user service as
// root.
func (r *serviceRun) ready() error {
	if r.saveErr != nil {
		return r.saveErr
	}
	if r.sp.System && r.h.EUID != 0 {
		// The same flags (the file holds the rest); the last --config wins,
		// made absolute for root's working directory. --run-as pins you,
		// whom sudo would otherwise hide.
		argv := append(append([]string{"sudo", r.sp.Binary}, r.verb...), r.args...)
		argv = append(argv, "--run-as", r.sp.RunAs, "--config", r.cfgPath)
		return fmt.Errorf("a system service needs root; run:\n  %s", service.ShellJoin(argv))
	}
	if !r.sp.System && r.h.EUID == 0 && r.h.OS == "linux" {
		return fmt.Errorf("%w: you are root; a user service would run in root's session: use --system", errUsage)
	}
	return nil
}

// apply runs the plan, saves the flags and prints the plan's notes and
// done (the last line). A save that fails after the plan applied is
// returned last, so the output still says what happened.
func (r *serviceRun) apply(plan service.Plan, force bool, done string, stdout io.Writer) error {
	if err := plan.Apply(serviceRunner, stdout, force); err != nil {
		return err
	}
	saveErr := r.save.write(stdout)
	printNotes(stdout, plan.Notes)
	fmt.Fprintln(stdout, done)
	return saveErr
}
