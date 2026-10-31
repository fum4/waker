package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/fum4/waker/internal/service"
	"github.com/fum4/waker/internal/update"
	"github.com/fum4/waker/internal/version"
)

// updateTimeout bounds one `waker update`: the check, the download and
// the new binary's dry run.
const updateTimeout = 3 * time.Minute

// executable is the binary `waker update` replaces (tests point it at a
// copy, never at the test binary).
var executable = os.Executable

// releaseSource is where releases come from and the key that signs them
// (tests serve a fixture release signed with their own key).
var releaseSource = func() update.Source {
	return update.Source{Base: update.Base(), Key: update.ReleaseKey(), Client: &http.Client{Timeout: updateTimeout}}
}

// runUpdate installs the latest release (or --version) over this binary,
// verified the way install.sh verifies it. It never restarts waker: the
// running service keeps the old binary until its next start.
func runUpdate(args []string, stdout, stderr io.Writer) error {
	flags := newBareFlagSet("update", stderr)
	want := flags.String("version", "", "install this release (vN.N.N; may be older) instead of the latest")
	check := flags.Bool("check", false, "only say whether a newer release exists; install nothing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, flags.Arg(0))
	}
	if *check && *want != "" {
		return fmt.Errorf("%w: --check and --version don't go together", errUsage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	src := releaseSource()
	cur, curErr := update.ParseTag(version.String()) // a dev build has no tag
	curName := version.String()
	if curErr == nil {
		curName = cur.String()
	}

	var tag update.Tag
	var err error
	if *want != "" {
		if tag, err = update.ParseTag(*want); err != nil {
			return fmt.Errorf("%w: --version %v", errUsage, err)
		}
	} else if tag, err = src.Latest(ctx); err != nil {
		return err
	}

	switch {
	case *check && (curErr != nil || tag.Compare(cur) > 0):
		fmt.Fprintf(stdout, "waker %s → %s available: run `waker update`\nchanges: %s\n", curName, tag, update.ChangelogURL(tag))
		return nil
	case *check:
		fmt.Fprintf(stdout, "waker %s is up to date (latest: %s)\n", curName, tag)
		return nil
	case *want == "" && curErr == nil && tag.Compare(cur) <= 0:
		// install.sh's downgrade guard: latest never takes you back.
		fmt.Fprintf(stdout, "waker %s is up to date (latest: %s)\n", curName, tag)
		return nil
	case *want != "" && curErr == nil && tag == cur:
		fmt.Fprintf(stdout, "waker %s is already installed\n", curName)
		return nil
	}

	dest, err := executable()
	if err != nil {
		return fmt.Errorf("find the waker binary: %w", err)
	}
	rel, err := src.Fetch(ctx, tag, update.Asset())
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "verified waker %s (%s)\n", tag, update.Asset())
	switch err := update.Install(ctx, rel, dest); {
	case errors.Is(err, update.ErrUnchanged):
		fmt.Fprintf(stdout, "%s is already this build\n", dest)
		return nil
	case errors.Is(err, fs.ErrPermission):
		argv := append([]string{"sudo", dest, "update"}, args...)
		return fmt.Errorf("can't replace %s (%v); run:\n  %s", dest, err, service.ShellJoin(argv))
	case err != nil:
		return err
	}
	fmt.Fprintf(stdout, "updated waker %s → %s (%s; the old one is %s%s)\n", curName, tag, dest, dest, update.PrevSuffix)
	fmt.Fprintf(stdout, "changes: %s\n", update.ChangelogURL(tag))
	fmt.Fprintln(stdout, "restart to apply: waker restart")
	return nil
}
