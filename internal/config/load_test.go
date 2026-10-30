package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const minimal = `
apps:
  - name: web
    listen: 127.0.0.1:8080
    upstream: http://127.0.0.1:3000
    start: ["./serve"]
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "waker.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	cfg, err := Load(writeConfig(t, minimal))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxHeadroom != 5120*MiB || cfg.MinHeadroom != 512*MiB {
		t.Errorf("headroom = %s/%s, want 5GiB/512MiB", cfg.MaxHeadroom, cfg.MinHeadroom)
	}
	home, _ := os.UserHomeDir()
	if want := filepath.Join(home, ".local/state/waker"); cfg.StateDir != want {
		t.Errorf("state_dir = %q, want %q", cfg.StateDir, want)
	}
	if want := filepath.Join(cfg.StateDir, "control.sock"); cfg.ControlSocket != want {
		t.Errorf("control_socket = %q, want %q", cfg.ControlSocket, want)
	}
	a := cfg.Apps[0]
	checks := []struct {
		field string
		ok    bool
	}{
		{"mode", a.Mode == ModeOnDemand},
		{"idle", a.Idle.D() == 30*time.Minute},
		{"priority", a.Priority == 5},
		{"heartbeat", a.HeartbeatEnabled()},
		{"boot_timeout", a.BootTimeout.D() == 600*time.Second},
		{"health.path", a.Health.Path == "/"},
		{"peak.source", a.Peak.Source == PeakProcTree},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("default for %s not applied: %+v", c.field, a)
		}
	}
}

func TestLoadXDGDefaults(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/var/lib/x")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/7")
	cfg, err := Load(writeConfig(t, minimal))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != "/var/lib/x/waker" || cfg.ControlSocket != "/run/user/7/waker/control.sock" {
		t.Fatalf("got %q, %q", cfg.StateDir, cfg.ControlSocket)
	}
}

func TestLoadResolvesRelativePaths(t *testing.T) {
	path := writeConfig(t, `
state_dir: state
apps:
  - name: web
    listen: 127.0.0.1:8080
    upstream: http://127.0.0.1:3000
    start: ["./serve"]
    cwd: app
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if cfg.StateDir != filepath.Join(dir, "state") || cfg.Apps[0].Cwd != filepath.Join(dir, "app") {
		t.Fatalf("paths not anchored at %s: %q %q", dir, cfg.StateDir, cfg.Apps[0].Cwd)
	}
}

func TestLoadExplicitValues(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
max_headroom: 2GiB
min_headroom: 256MiB
apps:
  - name: web
    listen: 127.0.0.1:8080
    upstream: http://127.0.0.1:3000
    start: ["./serve", "--port", "3000"]
    mode: "on"
    idle: 10s
    memory: 768MiB
    priority: 2
    heartbeat: false
    health: {command: ["true"]}
    peak: {source: systemd, unit: web.service}
`))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Apps[0]
	if cfg.MaxHeadroom != 2*GiB || a.Idle.D() != 10*time.Second || a.Memory != 768*MiB ||
		a.Priority != 2 || a.HeartbeatEnabled() || a.Mode != ModeOn || a.Health.Path != "" {
		t.Fatalf("explicit values not kept: %+v", a)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"empty", "", "config is empty"},
		{"unknown key", minimal + "    colour: red\n", "field colour not found"},
		{"bad duration", minimal + "    idle: soon\n", `invalid duration "soon"`},
		{"bad size", minimal + "    memory: lots\n", `invalid size "lots"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	t.Setenv(EnvConfig, "")
	if got := ResolvePath(""); got != DefaultPath {
		t.Errorf("default = %q", got)
	}
	t.Setenv(EnvConfig, "/etc/waker.yaml")
	if got := ResolvePath(""); got != "/etc/waker.yaml" {
		t.Errorf("env = %q", got)
	}
	if got := ResolvePath("x.yaml"); got != "x.yaml" {
		t.Errorf("flag = %q", got)
	}
}
