package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// installed installs the file's service on the fake host and returns the
// unit's path; the recorded commands are dropped.
func installed(t *testing.T, path string) string {
	t.Helper()
	if code, stdout, stderr := runCLI("service", "install", "--config", path); code != 0 {
		t.Fatalf("install = %d\n%s%s", code, stdout, stderr)
	}
	ran.take()
	h, _ := currentHost()
	return filepath.Join(h.ConfigHome, "systemd", "user", "waker.service")
}

const restartCmd = "systemctl --user restart waker.service"

func TestRestartNotInstalled(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	code, _, stderr := runCLI("restart", "--config", path)
	if code != 1 || !strings.Contains(stderr, "no waker service is installed") || !strings.Contains(stderr, "waker service install") {
		t.Fatalf("= %d %q", code, stderr)
	}
	if calls := ran.take(); len(calls) != 0 {
		t.Errorf("ran %q", calls)
	}
}

func TestRestartCurrentUnitOnlyRestarts(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	unit := installed(t, path)
	before := readString(t, unit)
	code, stdout, stderr := runCLI("restart", "--config", path)
	if code != 0 || !strings.Contains(stdout, "restarted;") || strings.Contains(stdout, "rewrote") {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	if calls := ran.take(); !slices.Equal(calls, []string{restartCmd}) {
		t.Errorf("calls = %q, want only the restart", calls)
	}
	if readString(t, unit) != before {
		t.Error("a current unit was rewritten")
	}
}

func TestRestartRewritesAStaleUnit(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	unit := installed(t, path)
	// The block changes, and says start: false, which is install's alone.
	writeFile(t, path, strings.Replace(commented, "restart: on-failure", "restart: never\n  start: false", 1))
	code, stdout, stderr := runCLI("restart", "--config", path)
	if code != 0 || !strings.Contains(stdout, "rewrote "+unit+" from waker.yaml and restarted") {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	if got := readString(t, unit); !strings.Contains(got, "Restart=no\n") {
		t.Errorf("unit not rewritten:\n%s", got)
	}
	if calls := ran.take(); !slices.Contains(calls, "systemctl --user daemon-reload") || calls[len(calls)-1] != restartCmd {
		t.Errorf("calls = %q", calls)
	}
	if _, out, errOut := runCLI("check", "--config", path); strings.Contains(out+errOut, "differs") {
		t.Errorf("still drifting:\n%s%s", out, errOut)
	}
}

func TestRestartSavesFlags(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	unit := installed(t, path)
	code, stdout, stderr := runCLI("restart", "--config", path, "--restart", "always")
	if code != 0 || !strings.Contains(stdout, "saved service.restart: always") {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	ran.take()
	if !strings.Contains(readString(t, unit), "Restart=always\n") || !strings.Contains(readString(t, path), "restart: always ") {
		t.Error("the flag reached neither the unit nor the file")
	}
}

func TestRestartRefusesABrokenConfig(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	unit := installed(t, path)
	before := readString(t, unit)
	writeFile(t, path, commented+"bogus: 1\n")
	if code, _, stderr := runCLI("restart", "--config", path); code != 1 || !strings.Contains(stderr, "bogus") {
		t.Fatalf("= %d %q", code, stderr)
	}
	if calls := ran.take(); len(calls) != 0 {
		t.Errorf("a broken config ran %q", calls)
	}
	if readString(t, unit) != before {
		t.Error("a broken config rewrote the unit")
	}
}

func TestRestartLeavesAHandWrittenUnit(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	h, _ := currentHost()
	unit := filepath.Join(h.ConfigHome, "systemd", "user", "waker.service")
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, unit, "[Service]\nExecStart=/mine\n")
	code, stdout, stderr := runCLI("restart", "--config", path)
	if code != 0 || !strings.Contains(stderr, "wasn't written by waker") {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	if calls := ran.take(); !slices.Equal(calls, []string{restartCmd}) {
		t.Errorf("calls = %q", calls)
	}
	if readString(t, unit) != "[Service]\nExecStart=/mine\n" {
		t.Error("a hand-written unit was rewritten")
	}
}

func TestRestartSystemNeedsRoot(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented+"  system: true\n")
	h, _ := currentHost()
	unit := filepath.Join(h.Root, "etc", "systemd", "system", "waker.service")
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, unit, "x\n") // installed (not ours: the sudo line comes first anyway)
	code, _, stderr := runCLI("restart", "--config", path)
	if code != 1 || !strings.Contains(stderr, "sudo ") || !strings.Contains(stderr, " restart --config ") || !strings.HasSuffix(stderr, "--run-as ana --config "+path+"\n") {
		t.Fatalf("= %d %q", code, stderr)
	}
	if calls := ran.take(); len(calls) != 0 {
		t.Errorf("ran %q", calls)
	}
}

func TestRestartDryRun(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	installed(t, path)
	code, stdout, stderr := runCLI("restart", "--dry-run", "--config", path)
	if code != 0 || !strings.Contains(stdout, "$ "+restartCmd) {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	if calls := ran.take(); len(calls) != 0 {
		t.Errorf("--dry-run ran %q", calls)
	}
}
