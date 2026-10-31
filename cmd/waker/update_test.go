package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/update"
	"github.com/fum4/waker/internal/update/updatetest"
	"github.com/fum4/waker/internal/version"
)

// fakeRelease serves a release home, makes this waker report cur and
// points `waker update` at a copy of it; it returns the home and the copy.
func fakeRelease(t *testing.T, cur string) (*updatetest.Home, string) {
	t.Helper()
	h := updatetest.New(t)
	prevSrc, prevExe, prevVer := releaseSource, executable, version.Version
	releaseSource = h.Source
	dest := filepath.Join(t.TempDir(), "bin", "waker")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dest, string(updatetest.Script("v"+strings.TrimPrefix(cur, "v"))))
	executable = func() (string, error) { return dest, nil }
	version.Version = cur
	t.Cleanup(func() { releaseSource, executable, version.Version = prevSrc, prevExe, prevVer })
	return h, dest
}

func TestUpdateInstallsTheLatest(t *testing.T) {
	h, dest := fakeRelease(t, "0.3.0")
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	code, stdout, stderr := runCLI("update")
	if code != 0 {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"updated waker v0.3.0 → v0.4.0",
		"changes: https://github.com/fum4/waker/blob/v0.4.0/CHANGELOG.md",
		"restart to apply: waker restart",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if readString(t, dest) != string(updatetest.Script("v0.4.0")) {
		t.Error("binary not replaced")
	}
	if readString(t, dest+update.PrevSuffix) != string(updatetest.Script("v0.3.0")) {
		t.Error("old binary not kept")
	}
	if calls := ran.take(); len(calls) != 0 {
		t.Errorf("update ran %q; it never restarts", calls)
	}
}

func TestUpdateCheck(t *testing.T) {
	h, dest := fakeRelease(t, "0.3.0")
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	code, stdout, _ := runCLI("update", "--check")
	if code != 0 || !strings.Contains(stdout, "v0.3.0 → v0.4.0 available") {
		t.Errorf("= %d %q", code, stdout)
	}
	if readString(t, dest) != string(updatetest.Script("v0.3.0")) {
		t.Error("--check installed")
	}
}

func TestUpdateUpToDate(t *testing.T) {
	for _, cur := range []string{"0.4.0", "0.5.0"} { // the latest, and newer than it (never downgrades)
		h, dest := fakeRelease(t, cur)
		h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
		code, stdout, _ := runCLI("update")
		if code != 0 || !strings.Contains(stdout, "is up to date (latest: v0.4.0)") {
			t.Errorf("%s: = %d %q", cur, code, stdout)
		}
		if readString(t, dest) != string(updatetest.Script("v"+cur)) {
			t.Errorf("%s: binary replaced", cur)
		}
	}
}

func TestUpdateVersionPinsAndMayGoBack(t *testing.T) {
	h, dest := fakeRelease(t, "0.4.0")
	h.Publish("v0.3.0", updatetest.Script("v0.3.0"))
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	code, stdout, stderr := runCLI("update", "--version", "v0.3.0")
	if code != 0 || !strings.Contains(stdout, "v0.4.0 → v0.3.0") {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	if readString(t, dest) != string(updatetest.Script("v0.3.0")) {
		t.Error("not pinned")
	}
}

func TestUpdateRefusesATamperedRelease(t *testing.T) {
	h, dest := fakeRelease(t, "0.3.0")
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	h.Write("v0.4.0/"+update.Asset(), []byte("#!/bin/sh\necho pwned\n"))
	code, _, stderr := runCLI("update")
	if code != 1 || !strings.Contains(stderr, "checksum mismatch") || !strings.Contains(stderr, "do not use it") {
		t.Errorf("= %d %q", code, stderr)
	}
	if readString(t, dest) != string(updatetest.Script("v0.3.0")) {
		t.Error("tampered binary installed")
	}
}

func TestUpdateUnwritableBinaryPrintsSudo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	h, dest := fakeRelease(t, "0.3.0")
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	if err := os.Chmod(filepath.Dir(dest), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(dest), 0o755) })
	code, _, stderr := runCLI("update")
	if code != 1 || !strings.Contains(stderr, "sudo "+dest+" update --version v0.4.0") {
		t.Errorf("= %d %q", code, stderr)
	}
}

// A pre-release or pseudo-version build (go install @master) is newer
// than the release before its core: latest never takes it back.
func TestUpdateNeverDowngradesAPseudoVersion(t *testing.T) {
	h, dest := fakeRelease(t, "0.4.0")
	version.Version = "v0.4.1-0.20261030120000-abcdef123456"
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	code, stdout, _ := runCLI("update")
	if code != 0 || !strings.Contains(stdout, "is up to date (latest: v0.4.0)") {
		t.Errorf("= %d %q", code, stdout)
	}
	if readString(t, dest) != string(updatetest.Script("v0.4.0")) {
		t.Error("binary replaced")
	}
	h.Publish("v0.4.1", updatetest.Script("v0.4.1"))
	if code, stdout, _ := runCLI("update"); code != 0 || !strings.Contains(stdout, "→ v0.4.1") {
		t.Errorf("the release of its core: = %d %q", code, stdout)
	}
}

func TestUpdateUsage(t *testing.T) {
	fakeRelease(t, "0.3.0")
	for _, args := range [][]string{{"update", "--check", "--version", "v1.0.0"}, {"update", "--version", "latest"}, {"update", "x"}} {
		if code, _, _ := runCLI(args...); code != 2 {
			t.Errorf("%q = %d, want 2", args, code)
		}
	}
}
