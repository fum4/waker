package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestartRefusesNoStart(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	installed(t, path)
	if code, _, stderr := runCLI("restart", "--config", path, "--no-start"); code != 2 || !strings.Contains(stderr, "restart always starts waker") {
		t.Fatalf("= %d %q", code, stderr)
	}
	if calls := ran.take(); len(calls) != 0 {
		t.Errorf("ran %q", calls)
	}
	if got := readString(t, path); got != commented {
		t.Errorf("--no-start was saved:\n%s", got)
	}
}

func TestRestartRefusesFlagsForAHandWrittenUnit(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	h, _ := currentHost()
	unit := filepath.Join(h.ConfigHome, "systemd", "user", "waker.service")
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, unit, "mine\n")
	code, _, stderr := runCLI("restart", "--config", path, "--restart", "never")
	if code != 2 || !strings.Contains(stderr, "service flags can't apply") || !strings.Contains(stderr, "--force") {
		t.Fatalf("= %d %q", code, stderr)
	}
	if calls := ran.take(); len(calls) != 0 {
		t.Errorf("ran %q", calls)
	}
	if got := readString(t, path); got != commented {
		t.Errorf("a flag for a hand-written unit was saved:\n%s", got)
	}
}

func TestRestartNamesTheOtherScope(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	h, _ := currentHost()
	unit := filepath.Join(h.Root, "etc", "systemd", "system", "waker.service")
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, unit, "x\n")
	code, _, stderr := runCLI("restart", "--config", path)
	if code != 1 || !strings.Contains(stderr, "one is in the system scope ("+unit+")") || !strings.Contains(stderr, "service.system: true") {
		t.Fatalf("= %d %q", code, stderr)
	}
}
