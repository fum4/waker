package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/config"
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

// serviceConfigWith is serviceConfig plus top-level YAML (a service: block).
func serviceConfigWith(t *testing.T, start, extra string) string {
	t.Helper()
	path := serviceConfig(t, start)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(b)+extra)
	return path
}

// defaultUserPATH is a user service's default service.path with $HOME at
// home: ~/.local/bin, then the platform's system PATH.
func defaultUserPATH(home string) string {
	sys := strings.TrimPrefix(config.DefaultServicePath(false), config.UserBinDir+":")
	return filepath.Join(home, ".local", "bin") + ":" + sys
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin") // the shell's PATH is never the service's
	code, stdout, stderr := runCLI("service", "install", "--dry-run", "--config", cfg)
	if code != 0 {
		t.Fatalf("dry-run = %d\n%s%s", code, stdout, stderr)
	}
	unit := filepath.Join(h.ConfigHome, "systemd", "user", "waker.service")
	for _, want := range []string{
		"--- " + unit,
		`"--config" "` + cfg + `"`,
		`Environment="PATH=` + defaultUserPATH(home) + `"`,
		"Restart=always\n",
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
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUDO_USER", "")
	// The sudo line repeats the given flags, pins the user sudo would hide
	// and names the config by its absolute path.
	code, _, stderr := runCLI("service", "install", "--system", "--config", cfg)
	if want := "needs root; run:\n  sudo " + exe + " service install --system --config " + cfg + " --run-as ana --config " + cfg + "\n"; code != 1 || !strings.HasSuffix(stderr, want) {
		t.Fatalf("= %d, %q; want suffix %q", code, stderr, want)
	}
	if code, _, stderr := runCLI("service", "install", "--system", "--no-start", "--config", cfg); code != 1 || !strings.Contains(stderr, " --no-start") {
		t.Fatalf("--no-start: the sudo line drops it: %d, %q", code, stderr)
	}
	// system: true in the file needs root too; the file carries it, so the
	// line needs no --system.
	sys := serviceConfigWith(t, "sh", "service: {system: true}\n")
	code, _, stderr = runCLI("service", "install", "--config", sys)
	if want := "sudo " + exe + " service install --config " + sys + " --run-as ana --config " + sys + "\n"; code != 1 || !strings.HasSuffix(stderr, want) {
		t.Fatalf("file system: true = %d, %q; want suffix %q", code, stderr, want)
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := serviceConfig(t, "definitely-not-a-command-xyz")
	code, stdout, _ := runCLI("check", "--config", cfg)
	if code != 0 || !strings.Contains(stdout, `warning: web.start: "definitely-not-a-command-xyz" is not an executable on the service's PATH`) ||
		!strings.Contains(stdout, "(checked against service.path "+defaultUserPATH(home)+")") {
		t.Fatalf("= %d, %q", code, stdout)
	}
	// service.path is what counts, never this shell's PATH: sh is on the
	// default service.path whatever the shell has...
	t.Setenv("PATH", "/nonexistent")
	if code, stdout, _ := runCLI("check", "--config", serviceConfig(t, "sh")); code != 0 || strings.Contains(stdout, "warning") {
		t.Fatalf("sh on the default service.path = %d, %q", code, stdout)
	}
	// ...and missing from a service.path without it.
	code, stdout, _ = runCLI("check", "--config", serviceConfigWith(t, "sh", "service: {path: /nonexistent}\n"))
	if code != 0 || !strings.Contains(stdout, `warning: web.start: "sh"`) || !strings.Contains(stdout, "(checked against service.path /nonexistent)") {
		t.Fatalf("against service.path = %d, %q", code, stdout)
	}
}

type nopRunner struct{}

func (nopRunner) Run(...string) (string, error) { return "", nil }
