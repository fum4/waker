package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const twoApps = `
apps:
  - name: web
    listen: 127.0.0.1:8080
    upstream: http://127.0.0.1:3000
    start: ["./serve"]
%s
  - name: api
    listen: 127.0.0.1:8081
    upstream: http://127.0.0.1:3001
    start: ["./api"]
`

// withDefaults builds a two-app config: defs is the defaults block's body,
// web's extra keys (indented four spaces) go into the first app.
func withDefaults(defs, web string) string {
	return "defaults:\n" + defs + strings.Replace(twoApps, "%s", web, 1)
}

func TestDefaultsPrecedence(t *testing.T) {
	tests := []struct {
		name      string
		defs, web string
		check     func(web, api App) bool
	}{
		{"scalar inherited", "  idle: 5m\n", "",
			func(w, a App) bool { return w.Idle.D() == 5*time.Minute && a.Idle.D() == 5*time.Minute }},
		{"scalar: app wins", "  idle: 5m\n", "    idle: 1m\n",
			func(w, a App) bool { return w.Idle.D() == time.Minute && a.Idle.D() == 5*time.Minute }},
		{"built-in when neither", "  priority: 7\n", "",
			func(w, a App) bool { return w.Idle.D() == DefaultIdle && w.Priority == 7 }},
		{"pointer scalar", "  heartbeat: false\n", "    heartbeat: true\n",
			func(w, a App) bool { return w.HeartbeatEnabled() && !a.HeartbeatEnabled() }},
		{"health merges per key", "  health: {path: /up, interval: 3s}\n", "    health: {timeout: 9s}\n",
			func(w, a App) bool {
				return w.Health.Path == "/up" && w.Health.Interval.D() == 3*time.Second && w.Health.Timeout.D() == 9*time.Second
			}},
		{"health: app's command drops default path", "  health: {path: /up, interval: 3s}\n", "    health: {command: [check]}\n",
			func(w, a App) bool {
				return w.Health.Path == "" && slices.Equal(w.Health.Command, []string{"check"}) &&
					w.Health.Interval.D() == 3*time.Second && a.Health.Path == "/up"
			}},
		{"env merges per key, app wins", "  env: {A: d, B: d}\n", "    env: {B: w, C: w}\n",
			func(w, a App) bool {
				return w.Env["A"] == "d" && w.Env["B"] == "w" && w.Env["C"] == "w" && len(a.Env) == 2 && a.Env["B"] == "d"
			}},
		{"branding merges per key", "  branding: {accent: '#111', name: Acme}\n", "    branding: {accent: '#222'}\n",
			func(w, a App) bool {
				return w.Branding.Accent == "#222" && w.Branding.Name == "Acme" && a.Branding.Accent == "#111"
			}},
		{"list: app replaces", "  idle_ignore: [/a, /b]\n", "    idle_ignore: [/c]\n",
			func(w, a App) bool {
				return slices.Equal(w.IdleIgnore, []string{"/c"}) && slices.Equal(a.IdleIgnore, []string{"/a", "/b"})
			}},
		{"list: empty app list replaces too", "  idle_ignore: [/a]\n", "    idle_ignore: []\n",
			func(w, a App) bool { return len(w.IdleIgnore) == 0 && len(a.IdleIgnore) == 1 }},
		{"anchors resolve", "  env: &e {A: d}\n", "    env: {B: w}\n",
			func(w, a App) bool { return w.Env["A"] == "d" && w.Env["B"] == "w" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(withDefaults(tt.defs, tt.web)))
			if err != nil {
				t.Fatal(err)
			}
			if !tt.check(cfg.Apps[0], cfg.Apps[1]) {
				t.Fatalf("web = %+v\napi = %+v", cfg.Apps[0], cfg.Apps[1])
			}
		})
	}
}

func TestDefaultsParseErrors(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"per-app key", withDefaults("  listen: 127.0.0.1:1\n", ""), "defaults.listen is per-app only"},
		{"name", withDefaults("  name: x\n", ""), "defaults.name is per-app only"},
		{"unknown key", withDefaults("  colour: red\n", ""), "field colour not found"},
		{"bad value has its line", withDefaults("  idle: soon\n", ""), `line 2: invalid duration "soon"`},
		{"not a mapping", "defaults: [idle]\n" + strings.Replace(twoApps, "%s", "", 1), "cannot unmarshal"},
		{"top-level branding", "branding: {accent: '#fff'}\n" + strings.Replace(twoApps, "%s", "", 1), "branding moved: put it under defaults"},
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

func TestDefaultsValidationNamesTheRealPath(t *testing.T) {
	tests := []struct {
		name      string
		defs, web string
		want      []string
		not       []string
	}{
		{"inherited value: defaults path, once", "  priority: 42\n", "",
			[]string{"defaults.priority: 42 must be"}, []string{"apps[0] (web).priority", "apps[1] (api).priority"}},
		{"app's own value: app path", "  priority: 3\n", "    priority: 42\n",
			[]string{"apps[0] (web).priority: 42"}, []string{"defaults.priority"}},
		{"map sub-key from defaults", "  branding: {accent: lime}\n", "    branding: {name: Acme}\n",
			[]string{`defaults.branding.accent: "lime"`}, []string{"(web).branding.accent", "(api).branding.accent"}},
		{"map sub-key from the app", "  branding: {name: Acme}\n", "    branding: {accent: lime}\n",
			[]string{`apps[0] (web).branding.accent: "lime"`}, []string{"defaults.branding"}},
		{"list element", "  idle_ignore: [nope]\n", "",
			[]string{`defaults.idle_ignore[0]: "nope"`}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(withDefaults(tt.defs, tt.web)))
			if err != nil {
				t.Fatal(err)
			}
			verr := cfg.Validate()
			if verr == nil {
				t.Fatal("Validate passed")
			}
			msg := verr.Error()
			for _, w := range tt.want {
				if strings.Count(msg, w) != 1 {
					t.Errorf("want %q exactly once in:\n%s", w, msg)
				}
			}
			for _, n := range tt.not {
				if strings.Contains(msg, n) {
					t.Errorf("unexpected %q in:\n%s", n, msg)
				}
			}
		})
	}
}

func TestDefaultsLogoResolvesPerApp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logo.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "waker.yaml")
	if err := os.WriteFile(path, []byte(withDefaults("  branding: {logo: logo.svg}\n", "")), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range cfg.Apps {
		if a.Branding.Logo != filepath.Join(dir, "logo.svg") {
			t.Errorf("%s logo = %q", a.Name, a.Branding.Logo)
		}
	}
}

func TestEffectiveYAML(t *testing.T) {
	cfg, err := Parse([]byte(withDefaults("  idle: 5m\n  env: {A: d}\n", "    memory: 512MiB\n")))
	if err != nil {
		t.Fatal(err)
	}
	out, err := cfg.EffectiveYAML()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"idle: 5m0s", "memory: 512MiB", "priority: 5", "evictable: true", "path: /", "A: d"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("effective YAML lacks %q:\n%s", want, out)
		}
	}
	// The effective config is itself a valid config.
	if _, err := Parse(out); err != nil {
		t.Fatalf("effective YAML does not parse: %v\n%s", err, out)
	}
}
