package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/state"
	"github.com/fum4/waker/internal/update"
	"github.com/fum4/waker/internal/update/updatetest"
)

const hintLine = "waker v0.4.0 available (running v0.3.0): waker update"

// checkConfig writes a config whose state lives in the test's temp dir,
// with extra appended at the top level.
func checkConfig(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "waker.yaml")
	writeFile(t, p, "state_dir: "+filepath.Join(dir, "state")+"\n"+extra+
		"apps:\n  - {name: web, listen: '127.0.0.1:8080', upstream: 'http://127.0.0.1:3000', start: [./serve]}\n")
	return p
}

// onTerminal makes stderr count as a terminal, without colour.
func onTerminal(t *testing.T) {
	t.Helper()
	prev := stderrTerminal
	stderrTerminal = func(io.Writer) bool { return true }
	t.Setenv("NO_COLOR", "1")
	t.Setenv(update.EnvNoCheck, "")
	t.Cleanup(func() { stderrTerminal = prev })
}

func TestHintOnATerminal(t *testing.T) {
	h, _ := fakeRelease(t, "0.3.0")
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	onTerminal(t)
	cfg := checkConfig(t, "")
	code, _, stderr := runCLI("check", "--config", cfg)
	if code != 0 || strings.TrimSpace(stderr) != hintLine {
		t.Errorf("check = %d, stderr %q; want the hint alone", code, stderr)
	}
	// --config=path is found too, and the day's check is cached.
	code, _, stderr = runCLI("check", "--config="+cfg)
	if code != 0 || strings.TrimSpace(stderr) != hintLine {
		t.Errorf("check --config= = %d, stderr %q", code, stderr)
	}
}

func TestNoHint(t *testing.T) {
	for _, tc := range []struct {
		name, extra, env string
		terminal         bool
		args             []string
	}{
		{"off a terminal", "", "", false, []string{"check"}},
		{"update.check false", "update: {check: false}\n", "", true, []string{"check"}},
		{"WAKER_NO_UPDATE_CHECK", "", "1", true, []string{"check"}},
		{"a failed command", "", "", true, []string{"mode"}},
		{"a command that shows it itself", "", "", true, []string{"status"}},
		{"root", "", "", true, []string{"check"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := fakeRelease(t, "0.3.0")
			h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
			if tc.terminal {
				onTerminal(t)
			}
			if tc.name == "root" {
				fakeHost(t, 0)
			}
			t.Setenv(update.EnvNoCheck, tc.env)
			args := append(tc.args, "--config", checkConfig(t, tc.extra))
			if _, _, stderr := runCLI(args...); strings.Contains(stderr, "available (running") {
				t.Errorf("%q printed the hint: %q", args, stderr)
			}
		})
	}
}

// Only a clean release build checks: no release replaces the others.
func TestUpdateCheckOnlyForReleaseBuilds(t *testing.T) {
	cfg, err := config.Load(checkConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	for v, want := range map[string]bool{
		"0.3.0": true, "v0.3.0": true,
		"dev": false, "v0.3.1-0.20261031120000-abcdef123456": false, "v0.3.0+dirty": false,
	} {
		fakeRelease(t, v)
		if _, ok := updateCheck(cfg); ok != want {
			t.Errorf("version %s: checks = %v, want %v", v, ok, want)
		}
	}
}

// status shows what the last check recorded, and never asks itself.
func TestKnownUpdateIsOffline(t *testing.T) {
	fakeRelease(t, "0.3.0") // nothing published: a request would find nothing
	cfg, err := config.Load(checkConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if l := knownUpdate(cfg); l != "" {
		t.Errorf("no record: %q", l)
	}
	if err := state.SaveUpdate(cfg.StateDir, state.Update{CheckedAt: time.Now(), Latest: "v0.4.0"}); err != nil {
		t.Fatal(err)
	}
	if l := knownUpdate(cfg); l != hintLine {
		t.Errorf("= %q, want %q", l, hintLine)
	}
}

func TestConfigArg(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--config", "a.yaml"}, "a.yaml"},
		{[]string{"-config=b.yaml", "--json"}, "b.yaml"},
		{[]string{"shop", "on", "--config", "c.yaml"}, "c.yaml"},
		{[]string{"--", "--config", "d.yaml"}, ""},
		{[]string{"--config"}, ""},
		{[]string{"--configure", "x"}, ""},
	} {
		if got := configArg(tc.args); got != tc.want {
			t.Errorf("configArg(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestCheckEffectiveShowsUpdate(t *testing.T) {
	code, stdout, stderr := runCLI("check", "--effective", "--config", checkConfig(t, "update: {check: false}\n"))
	if code != 0 || !strings.Contains(stdout, "update:\n    check: false") {
		t.Errorf("check --effective = %d\n%s%s", code, stdout, stderr)
	}
}
