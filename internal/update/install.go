package update

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// ErrUnchanged is Install's answer when dest is already that build.
var ErrUnchanged = errors.New("already this build")

// Install replaces dest with rel's binary the way install.sh does, and
// more carefully: the new binary is written beside dest (dest.new), must
// run and report rel's version, then the current dest is kept as
// dest.prev and dest.new renamed over dest (atomic: a reader sees the old
// or the new binary, never a torn one). It records the installed tag
// where install.sh does, for its downgrade guard.
func Install(ctx context.Context, rel Release, dest string) error {
	if sameBytes(dest, rel.Binary) {
		return ErrUnchanged
	}
	next := dest + ".new"
	if err := writeFile(next, rel.Binary, 0o755); err != nil {
		return err
	}
	defer os.Remove(next) // no-op after the rename
	if err := DryRun(ctx, next, rel.Tag); err != nil {
		return err
	}
	if err := keepPrev(dest); err != nil {
		return fmt.Errorf("keep the current binary as %s%s: %w", dest, PrevSuffix, err)
	}
	if err := os.Rename(next, dest); err != nil {
		return err
	}
	return recordVersion(dest, rel.Tag)
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
// binary at dest: <dir without /bin>/share/nomadware-releases/waker.version.
func VersionRecord(dest string) string {
	dir := strings.TrimSuffix(filepath.Dir(dest), "/bin")
	return filepath.Join(dir, "share", "nomadware-releases", Product+".version")
}

func recordVersion(dest string, tag Tag) error {
	rec := VersionRecord(dest)
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
		src, err := os.Open(dest)
		if err != nil {
			return err
		}
		defer src.Close()
		b, err := io.ReadAll(src)
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
