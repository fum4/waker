package main

import (
	"fmt"
	"io"

	"github.com/fum4/waker/internal/service"
)

// runRestart restarts the installed service, so it runs what waker.yaml
// says now: the config is loaded and validated first (a broken file never
// takes waker down), and a waker-written unit that the service: block (or
// this waker version) would write differently is rewritten before the
// restart, the way `service install` writes it.
func runRestart(args []string, stdout, stderr io.Writer) error {
	fs, cfgFlag := newFlagSet("restart", stderr)
	dryRun := fs.Bool("dry-run", false, "print what it would rewrite and run; change nothing")
	sf := addServiceFlags(fs)
	r, err := newServiceRun([]string{"restart"}, fs, cfgFlag, sf, args)
	if err != nil {
		return err
	}
	if flagGiven(fs, "no-start") {
		return fmt.Errorf("%w: --no-start: restart always starts waker; set start: false for `waker service install`", errUsage)
	}
	plan, drift, err := service.PlanRestart(r.h, r.sp)
	if err != nil {
		return err
	}
	if drift == service.DriftNotOurs {
		if len(sf.given()) > 0 {
			return fmt.Errorf("%w: %s wasn't written by waker, so service flags can't apply to it: drop them, or replace it with `waker service install --force`", errUsage, plan.Unit)
		}
		fmt.Fprintf(stderr, "warning: %s wasn't written by waker, so restart doesn't rewrite it: it restarts it as it is\n", plan.Unit)
	} else {
		r.warnPATH(stderr)
	}
	if *dryRun {
		r.printDryRun(plan, stdout, stderr)
		return nil
	}
	if err := r.ready(); err != nil {
		return err
	}
	done := "restarted; check it with: waker service status"
	if drift == service.DriftStale {
		done = "rewrote " + plan.Unit + " from waker.yaml and restarted; check it with: waker service status"
	}
	return r.apply(plan, false, done, stdout)
}
