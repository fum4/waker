package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/service"
	"github.com/fum4/waker/internal/version"
)

func runCheck(args []string, stdout, stderr io.Writer) error {
	fs, cfgFlag := newFlagSet("check", stderr)
	effective := fs.Bool("effective", false, "print each app's resolved config (defaults applied) as YAML")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	path := config.ResolvePath(*cfgFlag)
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if *effective {
		out, err := cfg.EffectiveYAML()
		if err != nil {
			return err
		}
		_, err = stdout.Write(out)
		return err
	}
	fmt.Fprintf(stdout, "%s: ok — %d app(s), headroom curve %s, eviction %s\n",
		path, len(cfg.Apps), cfg.Resources.Headroom.Curve, cfg.Resources.Eviction.Summary())
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "APP\tLISTEN\tUPSTREAM\tMODE\tIDLE\tPRIORITY\tMEMORY")
	for _, a := range cfg.Apps {
		mem := "-"
		if a.Memory > 0 {
			mem = a.Memory.String()
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%g\t%s\n",
			a.Name, a.Listen, a.Upstream, a.Mode, a.Idle, a.Priority, mem)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return checkServicePATH(cfg, path, stdout)
}

// currentHost is the host check reads the installed service from; tests
// swap in a fake one.
var currentHost = service.CurrentHost

// checkServicePATH warns about commands the service won't find: on the
// installed service's PATH, else (none installed) on this shell's, which
// is what `waker service install` would capture.
func checkServicePATH(cfg *config.Config, cfgPath string, w io.Writer) error {
	warnings, source, err := servicePATHWarnings(cfg, cfgPath)
	for _, msg := range warnings {
		fmt.Fprintf(w, "warning: %s\n", msg)
	}
	if len(warnings) > 0 {
		fmt.Fprintf(w, "(checked against %s)\n", source)
	}
	return err
}

// servicePATHWarnings returns pathWarnings against the PATH the service
// has (or would capture), and which PATH that was.
func servicePATHWarnings(cfg *config.Config, cfgPath string) (warnings []string, source string, err error) {
	abs, err := filepath.Abs(cfgPath)
	if err != nil {
		return nil, "", err
	}
	path, source := os.Getenv("PATH"), "this shell's PATH (no service installed)"
	if h, err := currentHost(); err == nil {
		if p, unit, ok := service.ServicePATH(h); ok {
			path, source = p, "the PATH in "+unit
		}
	}
	return pathWarnings(cfg, path, filepath.Dir(abs)), source, nil
}

func runVersion(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: version takes no arguments", errUsage)
	}
	_, err := fmt.Fprintf(stdout, "waker %s\n", version.String())
	return err
}
