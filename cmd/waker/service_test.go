package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/service"
)

// fakeHost points every service lookup at a temp root, so no test reads
// or writes the real machine's units.
func fakeHost(t *testing.T, euid int) service.Host {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "run", "systemd", "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home", "ana")
	h := service.Host{OS: "linux", Home: home, User: "ana", UID: 1000, EUID: euid, ConfigHome: filepath.Join(home, ".config"), Root: root}
	prev := currentHost
	currentHost = func() (service.Host, error) { return h, nil }
	t.Cleanup(func() { currentHost = prev })
	return h
}

func serviceConfig(t *testing.T, start string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "waker.yaml")
	writeFile(t, path, "apps:\n  - {name: web, listen: '127.0.0.1:20080', upstream: 'http://127.0.0.1:20081', start: ["+start+"]}\n")
	return path
}

func TestServiceUsage(t *testing.T) {
	for _, args := range [][]string{{"service"}, {"service", "frob"}} {
		if code, _, stderr := runCLI(args...); code != 2 || !strings.Contains(stderr, "install") {
			t.Errorf("%q = %d, %q", args, code, stderr)
		}
	}
}

func TestServiceInstallDryRun(t *testing.T) {
	h := fakeHost(t, 1000)
	cfg := serviceConfig(t, "sh")
	t.Setenv("PATH", "/usr/bin:/bin")
	code, stdout, stderr := runCLI("service", "install", "--dry-run", "--config", cfg)
	if code != 0 {
		t.Fatalf("dry-run = %d\n%s%s", code, stdout, stderr)
	}
	unit := filepath.Join(h.ConfigHome, "systemd", "user", "waker.service")
	for _, want := range []string{
		"--- " + unit,
		`"--config" "` + cfg + `"`,
		`Environment="PATH=/usr/bin:/bin"`,
		"$ systemctl --user enable waker.service",
		"note: linger is off",
		"sudo loginctl enable-linger ana",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("dry-run lacks %q:\n%s", want, stdout)
		}
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote the unit: %v", err)
	}
}

func TestServiceInstallWarnsAboutPATH(t *testing.T) {
	fakeHost(t, 1000)
	cfg := serviceConfig(t, "definitely-not-a-command-xyz")
	code, _, stderr := runCLI("service", "install", "--dry-run", "--path", "/nonexistent", "--config", cfg)
	if code != 0 || !strings.Contains(stderr, `warning: web.start: "definitely-not-a-command-xyz" is not an executable on the service's PATH`) {
		t.Fatalf("= %d, %q", code, stderr)
	}
}

func TestServiceInstallRefusesABadConfig(t *testing.T) {
	fakeHost(t, 1000)
	path := filepath.Join(t.TempDir(), "waker.yaml")
	writeFile(t, path, "apps: []\n")
	if code, _, stderr := runCLI("service", "install", "--dry-run", "--config", path); code != 1 || !strings.Contains(stderr, "at least one app") {
		t.Fatalf("= %d, %q", code, stderr)
	}
}

func TestServiceSystemNeedsRoot(t *testing.T) {
	fakeHost(t, 1000)
	cfg := serviceConfig(t, "sh")
	t.Setenv("SUDO_USER", "")
	code, _, stderr := runCLI("service", "install", "--system", "--config", cfg)
	if code != 1 || !strings.Contains(stderr, "needs root; run:\n  sudo ") || !strings.Contains(stderr, "--system --config "+cfg+" --run-as ana --path ") {
		t.Fatalf("= %d, %q", code, stderr)
	}
	if code, _, stderr := runCLI("service", "uninstall", "--system"); code != 1 || !strings.Contains(stderr, "service uninstall --system") {
		t.Fatalf("uninstall = %d, %q", code, stderr)
	}
}

func TestServiceUserInstallAsRootIsRefused(t *testing.T) {
	fakeHost(t, 0)
	cfg := serviceConfig(t, "sh")
	if code, _, stderr := runCLI("service", "install", "--config", cfg); code != 2 || !strings.Contains(stderr, "use --system") {
		t.Fatalf("= %d, %q", code, stderr)
	}
}

func TestServiceStatusNotInstalled(t *testing.T) {
	fakeHost(t, 1000)
	code, stdout, _ := runCLI("service", "status")
	if code != 3 || !strings.Contains(stdout, "user service (systemd): not installed") || !strings.Contains(stdout, "system service (systemd): not installed") {
		t.Fatalf("= %d, %q", code, stdout)
	}
}

func TestServiceUnsupportedPrintsManualSteps(t *testing.T) {
	h := fakeHost(t, 1000)
	h.OS = "freebsd"
	currentHost = func() (service.Host, error) { return h, nil }
	cfg := serviceConfig(t, "sh")
	code, _, stderr := runCLI("service", "install", "--dry-run", "--config", cfg)
	if code != 1 || !strings.Contains(stderr, "Run waker under your init system") || !strings.Contains(stderr, "no supported service manager on freebsd") {
		t.Fatalf("= %d, %q", code, stderr)
	}
}

func TestCheckWarnsAboutTheServicePATH(t *testing.T) {
	fakeHost(t, 1000)
	cfg := serviceConfig(t, "definitely-not-a-command-xyz")
	code, stdout, _ := runCLI("check", "--config", cfg)
	if code != 0 || !strings.Contains(stdout, "warning: web.start") || !strings.Contains(stdout, "this shell's PATH (no service installed)") {
		t.Fatalf("= %d, %q", code, stdout)
	}
	if code, stdout, _ := runCLI("check", "--config", serviceConfig(t, "sh")); code != 0 || strings.Contains(stdout, "warning") {
		t.Fatalf("sh on this shell's PATH = %d, %q", code, stdout)
	}
	// Once a service is installed, its unit's PATH is the one that counts:
	// sh is on this shell's PATH but not on the unit's.
	h, _ := currentHost()
	p, err := service.PlanInstall(h, service.Spec{Binary: "/bin/waker", Config: cfg, WorkDir: filepath.Dir(cfg), PATH: "/nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(nopRunner{}, io.Discard, false); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = runCLI("check", "--config", serviceConfig(t, "sh"))
	if code != 0 || !strings.Contains(stdout, `warning: web.start: "sh"`) || !strings.Contains(stdout, "the PATH in "+p.Unit) {
		t.Fatalf("against the unit's PATH = %d, %q", code, stdout)
	}
}

type nopRunner struct{}

func (nopRunner) Run(...string) (string, error) { return "", nil }
