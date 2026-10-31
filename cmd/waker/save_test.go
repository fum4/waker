package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/config"
)

// commented is a config whose service block a person wrote, with comments
// and alignment a save must keep.
const commented = `# my waker
apps:
  - {name: web, listen: '127.0.0.1:20080', upstream: 'http://127.0.0.1:20081', start: [sh]}

service:
  # keep it up
  restart: on-failure   # crashes only
  env:     {A: "1"}
`

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "waker.yaml")
	writeFile(t, path, body)
	return path
}

func TestServiceInstallSavesFlags(t *testing.T) {
	h := fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	code, stdout, stderr := runCLI("service", "install", "--config", path, "--restart", "never", "--path", "/opt/bin:/usr/bin")
	if code != 0 {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	ran.take()
	want := strings.Replace(commented, "restart: on-failure ", "restart: never ", 1)
	want = strings.Replace(want, "service:\n", "service:\n  path: /opt/bin:/usr/bin\n", 1)
	if got := readString(t, path); got != want {
		t.Errorf("file:\n%s\nwant (only the two keys changed):\n%s", got, want)
	}
	if !strings.Contains(stdout, "saved service.path: /opt/bin:/usr/bin, service.restart: never to "+path) {
		t.Errorf("stdout lacks the saved line:\n%s", stdout)
	}
	unit := readString(t, filepath.Join(h.ConfigHome, "systemd", "user", "waker.service"))
	if !strings.Contains(unit, "Restart=no\n") {
		t.Errorf("unit:\n%s", unit)
	}
	// The file now describes the installed unit: no drift.
	if _, out, errOut := runCLI("check", "--config", path); strings.Contains(out+errOut, "differs") {
		t.Errorf("drift after a saved install:\n%s%s", out, errOut)
	}
}

func TestServiceInstallSkipsFlagsThatChangeNothing(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	// on-failure is the file's; --no-start=false is the default.
	code, stdout, stderr := runCLI("service", "install", "--config", path, "--restart", "on-failure", "--no-start=false")
	if code != 0 {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	ran.take()
	if got := readString(t, path); got != commented || strings.Contains(stdout, "saved") {
		t.Errorf("a no-op flag was saved:\n%s\n%s", got, stdout)
	}
}

func TestServiceInstallNoSave(t *testing.T) {
	h := fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	code, stdout, stderr := runCLI("service", "install", "--config", path, "--restart", "never", "--no-save")
	if code != 0 {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	ran.take()
	if got := readString(t, path); got != commented {
		t.Errorf("--no-save wrote the file:\n%s", got)
	}
	unit := readString(t, filepath.Join(h.ConfigHome, "systemd", "user", "waker.service"))
	if !strings.Contains(unit, "Restart=no\n") {
		t.Errorf("--no-save dropped the flag from the unit:\n%s", unit)
	}
}

func TestServiceInstallDryRunSavesNothing(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	code, stdout, stderr := runCLI("service", "install", "--dry-run", "--config", path, "--restart", "never")
	if code != 0 || !strings.Contains(stdout, "note: would save service.restart: never to "+path) {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	if got := readString(t, path); got != commented {
		t.Errorf("--dry-run wrote the file:\n%s", got)
	}
}

func TestServiceInstallRefusesAManagedFile(t *testing.T) {
	fakeHost(t, 1000)
	body := "# managed: chezmoi (edit the template)\n" + commented
	path := writeConfigFile(t, body)
	code, _, stderr := runCLI("service", "install", "--config", path, "--restart", "never")
	if code != 1 || !strings.Contains(stderr, "is managed by chezmoi") || !strings.Contains(stderr, "--no-save") {
		t.Fatalf("= %d %q", code, stderr)
	}
	if calls := ran.take(); len(calls) != 0 {
		t.Errorf("a refused install ran %q", calls)
	}
	if got := readString(t, path); got != body {
		t.Errorf("a managed file was written:\n%s", got)
	}
	// --no-save, or no flags at all, installs from the file.
	for _, extra := range [][]string{{"--restart", "never", "--no-save"}, nil} {
		args := append([]string{"service", "install", "--config", path}, extra...)
		if code, stdout, stderr := runCLI(args...); code != 0 {
			t.Errorf("%q = %d\n%s%s", extra, code, stdout, stderr)
		}
		ran.take()
	}
	// --dry-run shows the plan and says the real run would refuse.
	code, stdout, stderr := runCLI("service", "install", "--dry-run", "--config", path, "--restart", "never")
	if code != 0 || !strings.Contains(stdout, "[Service]") || !strings.Contains(stderr, "the real run refuses") {
		t.Errorf("dry-run = %d\n%s%s", code, stdout, stderr)
	}
}

func TestServiceInstallSudoHintSavesNothing(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	if code, _, stderr := runCLI("service", "install", "--config", path, "--system"); code != 1 || !strings.Contains(stderr, "sudo ") {
		t.Fatalf("= %d %q", code, stderr)
	}
	if got := readString(t, path); got != commented {
		t.Errorf("the sudo hint wrote the file (root's run saves):\n%s", got)
	}
}

// The sudo hint's --run-as pins the user: root's run saves it, so a later
// install from a root shell (no $SUDO_USER) still runs as that user.
func TestServiceInstallAsRootSavesTheHintsRunAs(t *testing.T) {
	fakeHost(t, 0)
	t.Setenv("SUDO_USER", "ana")
	path := writeConfigFile(t, commented)
	code, stdout, stderr := runCLI("service", "install", "--config", path, "--system", "--run-as", "ana")
	if code != 0 {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	ran.take()
	if got := readString(t, path); !strings.Contains(got, "service:\n  user: ana\n  system: true\n") {
		t.Errorf("file:\n%s", got)
	}
}

func TestInitRefusesBadServiceFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "waker.yaml")
	code, _, stderr := runCLI("init", "--yes", "--config", path, "--start", "sh", "--restart", "sometimes")
	if code != 2 || !strings.Contains(stderr, `service.restart: "sometimes"`) {
		t.Fatalf("= %d %q", code, stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("init wrote a file with a bad service flag")
	}
}

func TestInitSystemShowsTheSystemDefaultPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "waker.yaml")
	if code, stdout, stderr := runCLI("init", "--yes", "--config", path, "--start", "sh", "--system"); code != 0 {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	got := readString(t, path)
	if strings.Contains(got, "~/.local/bin") || !strings.Contains(got, "service:\n  system: true\n") {
		t.Errorf("file:\n%s", got)
	}
}

func TestInitWritesServiceFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "waker.yaml")
	code, stdout, stderr := runCLI("init", "--yes", "--config", path, "--start", "sh", "--restart", "on-failure", "--no-start")
	if code != 0 {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Service.Restart != config.RestartOnFailure || cfg.Service.Starts() {
		t.Errorf("service = %+v", cfg.Service)
	}
	if !strings.Contains(readString(t, path), "# service:\n") {
		t.Error("the commented defaults are gone")
	}
	if code, _, stderr := runCLI("init", "--yes", "--force", "--config", path, "--start", "sh", "--no-save"); code != 2 || !strings.Contains(stderr, "--no-save") {
		t.Errorf("init --no-save = %d %q", code, stderr)
	}
}

func TestSetupSavesFlagsToAnExistingConfig(t *testing.T) {
	fakeHost(t, 1000)
	path := writeConfigFile(t, commented)
	code, stdout, stderr := runCLI("setup", "--yes", "--config", path, "--restart", "always")
	if code != 0 || !strings.Contains(stdout, "saved service.restart: always") {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
	ran.take()
	if got, want := readString(t, path), strings.Replace(commented, "restart: on-failure ", "restart: always ", 1); got != want {
		t.Errorf("file:\n%s", got)
	}
}
