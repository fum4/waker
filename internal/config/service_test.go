package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// fakeHome pins $HOME, so a user service's ~ expands to a known directory.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func loadService(t *testing.T, block string) Service {
	t.Helper()
	cfg, err := Load(writeConfig(t, minimal+block))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Service
}

// Parse alone (no Load) already expands ~, and a bare ~ is the home dir.
func TestServiceHomeExpansion(t *testing.T) {
	home := fakeHome(t)
	cfg, err := Parse([]byte(minimal + "service:\n  path: \"~:~/bin:/usr/bin\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := home + ":" + filepath.Join(home, "bin") + ":/usr/bin"; cfg.Service.Path != want {
		t.Errorf("path = %q, want %q", cfg.Service.Path, want)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Parse + Validate: %v", err)
	}
}

func TestServiceDefaults(t *testing.T) {
	home := fakeHome(t)
	sys := strings.Join(host.ServicePATH, ":")

	s := loadService(t, "")
	if s.Restart != RestartAlways || !s.Starts() || s.System || s.User != "" || len(s.Env) != 0 {
		t.Errorf("user defaults = %+v", s)
	}
	if want := filepath.Join(home, ".local", "bin") + ":" + sys; s.Path != want {
		t.Errorf("user path = %q, want %q", s.Path, want)
	}

	s = loadService(t, "service: {system: true}\n")
	if s.Path != sys || strings.Contains(s.Path, home) || strings.Contains(s.Path, "~") {
		t.Errorf("system path = %q, want %q", s.Path, sys)
	}
}

func TestServiceDefaultPathPerPlatform(t *testing.T) {
	onHost(t, "darwin")
	if got := DefaultServicePath(true); !strings.HasPrefix(got, "/opt/homebrew/bin:") || !strings.HasSuffix(got, ":/usr/bin:/sbin:/bin") {
		t.Errorf("darwin system path = %q", got)
	}
	if got := DefaultServicePath(false); !strings.HasPrefix(got, UserBinDir+":/opt/homebrew/bin:") {
		t.Errorf("darwin user path = %q", got)
	}
}

func TestServiceExplicitValues(t *testing.T) {
	home := fakeHome(t)
	s := loadService(t, `
service:
  restart: on-failure
  path: ~/x:/usr/bin
  start: false
  env: {NODE_ENV: production}
`)
	if want := filepath.Join(home, "x") + ":/usr/bin"; s.Path != want {
		t.Errorf("path = %q, want %q", s.Path, want)
	}
	if s.Restart != RestartOnFailure || s.Starts() || s.Env["NODE_ENV"] != "production" {
		t.Errorf("explicit values not kept: %+v", s)
	}
	s = loadService(t, "service: {system: true, user: www, path: /opt/bin:/usr/bin}\n")
	if !s.System || s.User != "www" || s.Path != "/opt/bin:/usr/bin" {
		t.Errorf("system values not kept: %+v", s)
	}
}

func TestServiceValidation(t *testing.T) {
	fakeHome(t)
	tests := []struct {
		name, block, want string
	}{
		{"bad restart", "service: {restart: sometimes}\n", `service.restart: "sometimes" must be always, on-failure or never`},
		{"empty entry", "service: {path: '/usr/bin::/bin'}\n", "service.path: has an empty entry"},
		{"trailing colon", "service: {path: '/usr/bin:'}\n", "service.path: has an empty entry"},
		{"relative entry", "service: {path: 'bin:/usr/bin'}\n", `service.path: "bin" is not an absolute directory`},
		{"~ in system path", "service: {system: true, path: '~/bin:/usr/bin'}\n", `service.path: "~/bin": a system service has no ~`},
		{"bad env name", "service: {env: {1BAD: x}}\n", "service.env.1BAD: is not a valid environment variable name"},
		{"env with a dash", "service: {env: {A-B: x}}\n", "service.env.A-B: is not a valid environment variable name"},
		{"env PATH", "service: {env: {PATH: /x}}\n", "service.env.PATH: set service.path instead"},
		{"user without system", "service: {user: www}\n", "service.user: only applies with system: true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, minimal+tt.block))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestServiceEffectiveYAML(t *testing.T) {
	home := fakeHome(t)
	cfg, err := Load(writeConfig(t, minimal+"service: {env: {A: b}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := cfg.EffectiveYAML()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"service:\n",
		"    restart: always\n",
		"    path: " + filepath.Join(home, ".local", "bin") + ":",
		"    system: false\n",
		"    start: true\n", // resolved, not left unset
		"    env:\n        A: b\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("effective YAML lacks %q:\n%s", want, out)
		}
	}
	if cfg.Service.Start != nil {
		t.Error("EffectiveYAML resolved start on the config itself")
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("effective YAML does not parse: %v\n%s", err, out)
	}
}

func TestServiceNormalize(t *testing.T) {
	home := fakeHome(t)
	user := filepath.Join(home, ".local", "bin") + ":" + strings.Join(host.ServicePATH, ":")
	sys := DefaultServicePath(true)

	// The default path follows system both ways.
	s := loadService(t, "")
	s.System = true
	n, err := s.Normalize()
	if err != nil || n.Path != sys {
		t.Fatalf("→ system: path = %q, %v; want %q", n.Path, err, sys)
	}
	n.System = false
	if n, err = n.Normalize(); err != nil || n.Path != user {
		t.Fatalf("→ user: path = %q, %v; want %q", n.Path, err, user)
	}

	// A path written in the file stays.
	s = loadService(t, "service: {path: /opt/bin:/usr/bin}\n")
	s.System = true
	if n, err := s.Normalize(); err != nil || n.Path != "/opt/bin:/usr/bin" {
		t.Errorf("file path: %q, %v", n.Path, err)
	}

	// So does one set by a flag, ~ expanded for a user service.
	s = loadService(t, "")
	s.SetPath("~/y:/usr/bin")
	if n, err := s.Normalize(); err != nil || n.Path != filepath.Join(home, "y")+":/usr/bin" {
		t.Errorf("SetPath: %q, %v", n.Path, err)
	}
	s.System = true
	if _, err := s.Normalize(); err == nil || !strings.Contains(err.Error(), `service.path: "~/y": a system service has no ~`) {
		t.Errorf("SetPath ~ on a system service: err = %v", err)
	}

	// Normalize validates the way Load does, naming the field.
	s = loadService(t, "")
	s.Restart = "sometimes"
	if _, err := s.Normalize(); err == nil || !strings.HasPrefix(err.Error(), `service.restart: "sometimes" must be`) {
		t.Errorf("bad restart: err = %v", err)
	}
	s = loadService(t, "")
	s.User = "www"
	if _, err := s.Normalize(); err == nil || !strings.Contains(err.Error(), "service.user:") {
		t.Errorf("user without system: err = %v", err)
	}
	s.System = true
	if n, err := s.Normalize(); err != nil || n.User != "www" {
		t.Errorf("user with system: %+v, %v", n, err)
	}
}
