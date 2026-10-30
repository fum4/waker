package config

import (
	"fmt"
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
	if h := cfg.Resources.Headroom; h.Curve != CurveLinear || h.Max != 5120*MiB || h.Min != 512*MiB || !cfg.Resources.Eviction.Enabled || cfg.Resources.Eviction.Policy != EvictLower {
		t.Errorf("resources defaults = %+v", cfg.Resources)
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
		{"memory_source", a.MemorySource == MemoryProcTree && a.IsEvictable()},
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
resources: {headroom: {max: 2GiB, min: 256MiB}}
apps:
  - name: web
    listen: 127.0.0.1:8080
    upstream: http://127.0.0.1:3000
    start: ["./serve", "--port", "3000"]
    mode: "on"
    idle: 10s
    memory: 768MiB
    priority: 2.5
    heartbeat: false
    health: {command: ["true"]}
    memory_source: systemd
    systemd_unit: web.service
`))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Apps[0]
	if cfg.Resources.Headroom.Max != 2*GiB || a.Idle.D() != 10*time.Second || a.Memory != 768*MiB ||
		a.Priority != 2.5 || a.HeartbeatEnabled() || a.Mode != ModeOn || a.Health.Path != "" {
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

func TestControlSocketDefaults(t *testing.T) {
	const app = "apps:\n  - {name: web, listen: 127.0.0.1:20080, upstream: http://127.0.0.1:20081, start: [x]}\n"
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv(EnvControlSocket, "")
	cfg, err := Parse([]byte(app))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ControlSocket != "/run/user/1000/waker/control.sock" || cfg.ControlSocketSet() {
		t.Errorf("xdg: %s, set %v", cfg.ControlSocket, cfg.ControlSocketSet())
	}
	t.Setenv(EnvControlSocket, SystemControlSocket)
	if cfg, _ := Parse([]byte(app)); cfg.ControlSocket != SystemControlSocket || cfg.ControlSocketSet() {
		t.Errorf("env: %s", cfg.ControlSocket)
	}
	cfg, _ = Parse([]byte("control_socket: /srv/w.sock\n" + app))
	if cfg.ControlSocket != "/srv/w.sock" || !cfg.ControlSocketSet() {
		t.Errorf("file wins: %s, set %v", cfg.ControlSocket, cfg.ControlSocketSet())
	}
}

// waker reads YAML 1.2: bare on and off are the modes, not booleans. A
// YAML 1.1 rewrite (true/yes, false/no) fails with a hint saying so.
func TestBareOnOffAndTheBooleanHint(t *testing.T) {
	const app = "apps:\n  - {name: web, listen: 127.0.0.1:20080, upstream: http://127.0.0.1:20081, start: [x], mode: %s}\n"
	for _, m := range []string{"on", "off", "on-demand"} {
		cfg, err := Parse([]byte(fmt.Sprintf(app, m)))
		if err != nil || cfg.Validate() != nil || string(cfg.Apps[0].Mode) != m {
			t.Errorf("bare %s: err %v, mode %q", m, err, cfg.Apps[0].Mode)
		}
	}
	for v, want := range map[string]string{
		"true":      "(write on; a YAML 1.1 tool may have rewritten on as true)",
		"Yes":       "(write on;",
		"false":     "(write off; a YAML 1.1 tool may have rewritten off as false)",
		"no":        "(write off;",
		"sometimes": "must be on, off or on-demand\n",
	} {
		cfg, err := Parse([]byte(fmt.Sprintf(app, v)))
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error()+"\n", want) {
			t.Errorf("mode %s: %v; want %q", v, err, want)
		}
	}
}
