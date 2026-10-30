package main

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/version"
)

func runCheck(args []string, stdout, stderr io.Writer) error {
	fs, cfgFlag := newFlagSet("check", stderr)
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
	fmt.Fprintf(stdout, "%s: ok — %d app(s), headroom %s…%s\n",
		path, len(cfg.Apps), cfg.MinHeadroom, cfg.MaxHeadroom)
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "APP\tLISTEN\tUPSTREAM\tMODE\tIDLE\tPRIORITY\tMEMORY")
	for _, a := range cfg.Apps {
		mem := "-"
		if a.Memory > 0 {
			mem = a.Memory.String()
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			a.Name, a.Listen, a.Upstream, a.Mode, a.Idle, a.Priority, mem)
	}
	return tw.Flush()
}

func runVersion(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: version takes no arguments", errUsage)
	}
	_, err := fmt.Fprintf(stdout, "waker %s\n", version.String())
	return err
}
