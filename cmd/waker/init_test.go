package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
)

// typing makes the next init/setup interactive, reading answers from in.
func typing(t *testing.T, in string) {
	t.Helper()
	prevIn, prevTTY := stdin, interactive
	stdin, interactive = strings.NewReader(in), func() bool { return true }
	t.Setenv("NO_COLOR", "1")
	t.Cleanup(func() { stdin, interactive = prevIn, prevTTY })
}

func TestInitYes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shop")
	path := filepath.Join(dir, "waker.yaml")
	code, stdout, stderr := runCLI("init", "--yes", "--config", path, "--start", "npm run start", "--port", "20080", "--upstream-port", "20081", "--idle", "10m")
	if code != 0 {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Apps[0]
	if a.Name != "shop" || a.Listen != "127.0.0.1:20080" || !slices.Equal(a.Start, []string{"npm", "run", "start"}) || a.Idle.String() != "10m0s" {
		t.Errorf("app = %+v", a)
	}
	for _, want := range []string{"wrote " + path, "ok — 1 app(s)", "next: waker service install --config " + path} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if calls := ran.take(); len(calls) != 0 {
		t.Errorf("init --yes ran %q; it never installs", calls)
	}
}

func TestInitNeverOverwritesWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "waker.yaml")
	writeFile(t, path, "mine\n")
	code, _, stderr := runCLI("init", "--yes", "--config", path, "--start", "x")
	if code != 1 || !strings.Contains(stderr, "already exists; pass --force") {
		t.Fatalf("= %d %q", code, stderr)
	}
	if b, _ := os.ReadFile(path); string(b) != "mine\n" {
		t.Fatalf("overwritten: %q", b)
	}
	if code, _, stderr := runCLI("init", "--yes", "--force", "--config", path, "--start", "sh"); code != 0 {
		t.Fatalf("--force = %d %q", code, stderr)
	}
}

func TestInitRefusals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "waker.yaml")
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"init", "--config", path}, "not a terminal"}, // no TTY in tests
		{[]string{"init", "--yes", "--config", path}, "start: the command that starts the app is required"},
		{[]string{"init", "--yes", "--config", path, "--start", "x", "--port", "3000"}, "can't both listen on 3000"},
		{[]string{"init", "--yes", "--config", path, "--start", "echo 'oops"}, "unclosed"},
		{[]string{"init", "--yes", "--config", path, "--start", "x", "--name", "No Spaces"}, "name:"},
	}
	for _, tt := range tests {
		if code, _, stderr := runCLI(tt.args...); code != 2 || !strings.Contains(stderr, tt.want) {
			t.Errorf("%q = %d %q; want 2 and %q", tt.args, code, stderr, tt.want)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a refused init wrote %s", path)
	}
}

func TestInitInteractive(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "notes")
	path := filepath.Join(dir, "waker.yaml")
	// name: default · public port: bad, then 20090 · app port: taken, then
	// 20091 · start · health: default · idle: 5m · install: no.
	typing(t, "\nabc\n20090\n20090\n20091\npython3 -m http.server 20091\n\n5m\nn\n")
	code, stdout, stderr := runCLI("init", "--config", path)
	if code != 0 {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"◆ waker init", "❯ App name (notes)", `✖ "abc" is not a port`, "✖ waker already answers on that port",
		"✔ wrote " + path, "✔ waker check: ok", "notes  127.0.0.1:20090 → http://127.0.0.1:20091 · on-demand · sleeps after 5m",
		"❯ Install as a service now? [Y/n]", "later: waker service install --config ",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if a := cfg.Apps[0]; a.Name != "notes" || a.Listen != "127.0.0.1:20090" || a.Upstream != "http://127.0.0.1:20091" || a.Idle.String() != "5m0s" {
		t.Errorf("app = %+v", a)
	}
	if calls := ran.take(); len(calls) != 0 {
		t.Errorf("answered no, yet ran %q", calls)
	}
}

func TestInitInteractiveInstalls(t *testing.T) {
	h, _ := currentHost()
	path := filepath.Join(t.TempDir(), "waker.yaml")
	typing(t, "\n\n\nsh\n\n\n\n") // every default, start sh, install: yes
	code, stdout, stderr := runCLI("init", "--config", path)
	if code != 0 {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	unit := filepath.Join(h.ConfigHome, "systemd", "user", "waker.service")
	t.Cleanup(func() { os.Remove(unit) })
	b, err := os.ReadFile(unit)
	if err != nil || !strings.Contains(string(b), `"--config" "`+path+`"`) {
		t.Fatalf("unit = %q, %v", b, err)
	}
	if calls := ran.take(); !slices.Contains(calls, "systemctl --user restart waker.service") {
		t.Errorf("calls = %q", calls)
	}
}

func TestInitCtrlDExits130(t *testing.T) {
	typing(t, "blog\n") // then EOF at the port question
	path := filepath.Join(t.TempDir(), "waker.yaml")
	if code, _, _ := runCLI("init", "--config", path); code != 130 {
		t.Fatalf("= %d, want 130", code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("an aborted init wrote the file")
	}
}

func TestSetupYes(t *testing.T) {
	h, _ := currentHost()
	unit := filepath.Join(h.ConfigHome, "systemd", "user", "waker.service")
	t.Cleanup(func() { os.Remove(unit) })
	path := filepath.Join(t.TempDir(), "waker.yaml")
	// No config yet: setup writes one, then installs.
	code, stdout, stderr := runCLI("setup", "--yes", "--config", path, "--start", "sh")
	if code != 0 || !strings.Contains(stdout, "wrote "+path) || !strings.Contains(stdout, "installed") {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	ran.take()
	// With a config: setup keeps it (no --start needed) and reinstalls.
	code, stdout, stderr = runCLI("setup", "--yes", "--config", path)
	if code != 0 || !strings.Contains(stdout, "using "+path) || strings.Contains(stdout, "wrote "+path) {
		t.Fatalf("second run = %d\n%s%s", code, stdout, stderr)
	}
	if calls := ran.take(); !slices.Contains(calls, "systemctl --user enable waker.service") {
		t.Errorf("calls = %q", calls)
	}
}

func TestSetupInteractiveWithAConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "waker.yaml")
	if code, _, stderr := runCLI("init", "--yes", "--config", path, "--start", "sh"); code != 0 {
		t.Fatal(stderr)
	}
	typing(t, "n\n")
	code, stdout, _ := runCLI("setup", "--config", path)
	if code != 0 || !strings.Contains(stdout, "using "+path) || !strings.Contains(stdout, "Install as a service now?") {
		t.Fatalf("= %d\n%s", code, stdout)
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[string]string{"30m": "30m", "1h": "1h", "1h30m": "1h30m", "45s": "45s", "1m30s": "1m30s"} {
		v, _ := time.ParseDuration(d)
		if got := shortDuration(config.Duration(v)); got != want {
			t.Errorf("shortDuration(%s) = %s, want %s", d, got, want)
		}
	}
}
