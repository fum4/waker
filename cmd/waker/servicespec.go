package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/service"
)

// serviceFlags override the config's service block for one run; a flag
// that isn't given leaves the file's value (or its default) alone.
type serviceFlags struct {
	fs      *flag.FlagSet
	system  *bool
	noStart *bool
	path    *string
	runAs   *string
	restart *string
	noSave  *bool
}

func addServiceFlags(fs *flag.FlagSet) *serviceFlags {
	return &serviceFlags{
		fs:      fs,
		system:  fs.Bool("system", false, "install a system service (root) instead of a user one (service.system)"),
		noStart: fs.Bool("no-start", false, "install and enable it, but don't start or restart it (service.start: false)"),
		path:    fs.String("path", "", "the service's PATH (service.path)"),
		runAs:   fs.String("run-as", "", "--system: the account waker runs as (service.user; default $SUDO_USER, else you)"),
		restart: fs.String("restart", "", "always, on-failure or never (service.restart)"),
		noSave:  fs.Bool("no-save", false, "use these flags for this run only; don't save them to waker.yaml"),
	}
}

// serviceKeyFlag is one service flag: the service: key it sets, how, the
// value it saves, and the key's value in a resolved block (to tell a flag
// that changes nothing).
type serviceKeyFlag struct {
	flag, key string
	set       func(s *config.Service, f *serviceFlags)
	saved     func(f *serviceFlags) any
	resolved  func(s config.Service) any
}

// serviceKeyFlags is the one table of service flags: apply, the saved
// keys and setup's forwarding all read it.
var serviceKeyFlags = []serviceKeyFlag{
	{"system", "system",
		func(s *config.Service, f *serviceFlags) { s.System = *f.system },
		func(f *serviceFlags) any { return *f.system },
		func(s config.Service) any { return s.System }},
	{"no-start", "start",
		func(s *config.Service, f *serviceFlags) { start := !*f.noStart; s.Start = &start },
		func(f *serviceFlags) any { return !*f.noStart },
		func(s config.Service) any { return s.Starts() }},
	{"path", "path", // saved as typed (~ unexpanded), compared resolved
		func(s *config.Service, f *serviceFlags) { s.SetPath(*f.path) },
		func(f *serviceFlags) any { return *f.path },
		func(s config.Service) any { return s.Path }},
	{"run-as", "user",
		func(s *config.Service, f *serviceFlags) { s.User = *f.runAs },
		func(f *serviceFlags) any { return *f.runAs },
		func(s config.Service) any { return s.User }},
	{"restart", "restart",
		func(s *config.Service, f *serviceFlags) { s.Restart = config.Restart(*f.restart) },
		func(f *serviceFlags) any { return *f.restart },
		func(s config.Service) any { return string(s.Restart) }},
}

// given is the table rows whose flag is on the command line.
func (f *serviceFlags) given() []serviceKeyFlag {
	var rows []serviceKeyFlag
	for _, r := range serviceKeyFlags {
		if flagGiven(f.fs, r.flag) {
			rows = append(rows, r)
		}
	}
	return rows
}

// apply returns s with every given flag laid over it, validated the way
// the file is.
func (f *serviceFlags) apply(s config.Service) (config.Service, error) {
	for _, r := range f.given() {
		r.set(&s, f)
	}
	return s.Normalize()
}

// flagGiven reports whether the named flag was on the command line.
func flagGiven(fs *flag.FlagSet, name string) bool {
	given := false
	fs.Visit(func(f *flag.Flag) { given = given || f.Name == name })
	return given
}

// serviceSpec is the service waker would install for the config at
// cfgPath with settings s: the one place install, its drift check (check,
// status, serve) and the sudo hint agree on.
func serviceSpec(s config.Service, cfgPath string, h service.Host) (service.Spec, error) {
	abs, err := filepath.Abs(cfgPath)
	if err != nil {
		return service.Spec{}, err
	}
	bin, err := os.Executable()
	if err != nil {
		return service.Spec{}, fmt.Errorf("find the waker binary: %w", err)
	}
	sp := service.Spec{
		Binary: bin, Config: abs, WorkDir: filepath.Dir(abs),
		PATH: s.Path, System: s.System, RunAs: s.User, NoStart: !s.Starts(),
		Restart: s.Restart, Env: s.Env,
	}
	if sp.System && sp.RunAs == "" {
		sp.RunAs = os.Getenv("SUDO_USER")
		if sp.RunAs == "" {
			sp.RunAs = h.User
		}
	}
	return sp, nil
}

// reportDrift says, in one line, when the installed service no longer
// matches waker.yaml (check, status). Errors are not the caller's problem:
// a host without a service manager has nothing to drift.
func reportDrift(cfg *config.Config, cfgPath string, w io.Writer) {
	if msg := driftMessage(cfg, cfgPath); msg != "" {
		fmt.Fprintf(w, "warning: %s\n", msg)
	}
}

// driftMessage is the drift line, or "" when the service is current, not
// installed or not waker's.
func driftMessage(cfg *config.Config, cfgPath string) string {
	h, err := currentHost()
	if err != nil {
		return ""
	}
	sp, err := serviceSpec(cfg.Service, cfgPath, h)
	if err != nil {
		return ""
	}
	d, unit, err := service.CheckDrift(h, sp)
	if err != nil || d != service.DriftStale {
		return ""
	}
	return fmt.Sprintf("%s differs from the service waker.yaml describes (its service: block, or this waker version); apply it with: %s",
		unit, restartLine(sp))
}
