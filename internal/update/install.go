package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PrevSuffix names the copy of the replaced binary kept beside it
// (waker.prev), for rolling back.
const PrevSuffix = ".prev"

// dryRunTimeout bounds the new binary's `version` run.
const dryRunTimeout = 10 * time.Second

var (
	// ErrUnchanged is Install's answer when dest is already that build.
	ErrUnchanged = errors.New("already this build")
	// ErrCantWrite wraps a failure to write beside dest or replace it:
	// root (sudo) may fix it. A dry run that fails is never this.
	ErrCantWrite = errors.New("can't write")
	// ErrNotRecorded wraps a failure to record the installed tag after
	// the binary was replaced: the update itself succeeded.
	ErrNotRecorded = errors.New("installed, but the version record wasn't written")
)

// Install replaces dest with rel's binary the way install.sh does, and
// more carefully: the new binary is written beside dest (dest.new), must
// run and report rel's version, then the current dest is kept as
// dest.prev and dest.new renamed over dest (atomic: a reader sees the old
// or the new binary, never a torn one). It records the installed tag
// where install.sh does, for its downgrade guard, also when dest already
// was that build.
func Install(ctx context.Context, rel Release, dest string) error {
	if cur, err := os.ReadFile(dest); err == nil && bytes.Equal(cur, rel.Binary) {
		_ = recordVersion(dest, rel.Tag) // repair a missing record; best effort
		return ErrUnchanged
	}
	next := dest + ".new"
	if err := writeFile(next, rel.Binary, 0o755); err != nil {
		return fmt.Errorf("%w %s: %w", ErrCantWrite, next, err)
	}
	defer os.Remove(next) // no-op after the rename
	if err := DryRun(ctx, next, rel.Tag); err != nil {
		return err
	}
	if err := keepPrev(dest); err != nil {
		return fmt.Errorf("%w %s%s: %w", ErrCantWrite, dest, PrevSuffix, err)
	}
	if err := os.Rename(next, dest); err != nil {
		return fmt.Errorf("%w %s: %w", ErrCantWrite, dest, err)
	}
	if err := recordVersion(dest, rel.Tag); err != nil {
		return fmt.Errorf("%w (%s): %w", ErrNotRecorded, VersionRecord(dest), err)
	}
	return nil
}

// DryRun runs `<bin> version` and checks it reports tag: a binary that
// can't even say its version never replaces a working one.
func DryRun(ctx context.Context, bin string, tag Tag) error {
	ctx, cancel := context.WithTimeout(ctx, dryRunTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").Output()
	if err != nil {
		return fmt.Errorf("the new binary doesn't run (%s version): %w", bin, err)
	}
	f := strings.Fields(string(out))
	if len(f) != 2 || f[0] != Product {
		return fmt.Errorf("the new binary's version is %q, want %q", strings.TrimSpace(string(out)), Product+" "+tag.String())
	}
	if got, err := ParseTag(f[1]); err != nil || got != tag {
		return fmt.Errorf("the new binary says %q, want %s", strings.TrimSpace(string(out)), tag)
	}
	return nil
}

// VersionRecord is install.sh's record of the installed tag for the
// binary at dest: <prefix>/share/nomadware-releases/waker.version for a
// binary in <prefix>/bin, the only places install.sh installs to; ""
// for any other directory (nothing is recorded).
func VersionRecord(dest string) string {
	dir := filepath.Dir(dest)
	prefix := filepath.Dir(dir)
	if filepath.Base(dir) != "bin" || prefix == "/" || !filepath.IsAbs(prefix) {
		return ""
	}
	return filepath.Join(prefix, "share", "nomadware-releases", Product+".version")
}

func recordVersion(dest string, tag Tag) error {
	rec := VersionRecord(dest)
	if rec == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(rec), 0o755); err != nil {
		return err
	}
	return writeFile(rec, []byte(tag.String()+"\n"), 0o644)
}

// keepPrev makes dest.prev a copy of dest: a hard link when it can (no
// copy), else a copy, replaced atomically either way.
func keepPrev(dest string) error {
	prev := dest + PrevSuffix
	tmp := prev + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Link(dest, tmp); err != nil {
		b, err := os.ReadFile(dest)
		if err != nil {
			return err
		}
		if err := writeFile(tmp, b, 0o755); err != nil {
			return err
		}
	}
	return os.Rename(tmp, prev)
}

// writeFile writes and syncs path with mode, whatever umask says.
func writeFile(path string, b []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
