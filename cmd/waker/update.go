package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	return update.Source{Base: update.Base(), Key: update.ReleaseKey()} // the caller's context bounds every request
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
	cur := update.ParseVersion(version.String())

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
	case *check && cur.OlderThan(tag):
		fmt.Fprintf(stdout, "waker %s → %s available: run `waker update`\nchanges: %s\n", cur, tag, update.ChangelogURL(tag))
		return nil
	case *check, *want == "" && !cur.OlderThan(tag):
		// install.sh's downgrade guard: latest never takes you back.
		fmt.Fprintf(stdout, "waker %s is up to date (latest: %s)\n", cur, tag)
		return nil
	case *want != "" && cur.Known && !cur.Pre && tag == cur.Core:
		fmt.Fprintf(stdout, "waker %s is already installed\n", cur)
		return nil
	}

	exe, err := executable()
	if err != nil {
		return fmt.Errorf("find the waker binary: %w", err)
	}
	// The real file: replacing a symlink would detach whatever manages it.
	dest, err := filepath.EvalSymlinks(exe)
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
	case errors.Is(err, update.ErrCantWrite):
		return fmt.Errorf("%v; run:\n  %s", err, sudoUpdateLine(dest, tag))
	case errors.Is(err, update.ErrNotRecorded):
		fmt.Fprintf(stderr, "warning: %v\n", err)
	case err != nil:
		return err
	}
	fmt.Fprintf(stdout, "updated waker %s → %s (%s; the old one is %s%s)\n", cur, tag, dest, dest, update.PrevSuffix)
	fmt.Fprintf(stdout, "changes: %s\n", update.ChangelogURL(tag))
	fmt.Fprintln(stdout, "restart to apply: waker restart")
	return nil
}

// sudoUpdateLine installs the release just verified as root: pinned to its
// tag (latest may move), from the same release home (sudo drops the
// environment).
func sudoUpdateLine(dest string, tag update.Tag) string {
	argv := []string{"sudo"}
	if b := os.Getenv(update.EnvBase); b != "" {
		argv = append(argv, "env", update.EnvBase+"="+b)
	}
	return service.ShellJoin(append(argv, dest, "update", "--version", tag.String()))
}
