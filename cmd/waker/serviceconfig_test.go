package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/service"
)

// TestServiceInstallPrecedence: a flag beats the file's service block,
// which beats the default.
func TestServiceInstallPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	file := "service: {restart: on-failure, path: '/opt/x/bin:/usr/bin'}\n"
	tests := []struct {
		name      string
		block     string
		flags     []string
		want      []string
		wantNever []string
	}{
		{"default", "", nil,
			[]string{"Restart=always\n", `Environment="PATH=` + defaultUserPATH(home) + `"`}, nil},
		{"file", file, nil,
			[]string{"Restart=on-failure\n", `Environment="PATH=/opt/x/bin:/usr/bin"`}, nil},
		{"flags over file", file, []string{"--restart", "never", "--path", "/flag/bin"},
			[]string{"Restart=no\n", `Environment="PATH=/flag/bin"`}, []string{"/opt/x/bin"}},
		{"one flag keeps the rest of the file", file, []string{"--restart", "always"},
			[]string{"Restart=always\n", `Environment="PATH=/opt/x/bin:/usr/bin"`}, nil},
		{"file env", "service: {env: {NODE_ENV: production, A: '1'}}\n", nil,
			[]string{`Environment="A=1"` + "\n" + `Environment="NODE_ENV=production"`}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeHost(t, 1000)
			cfg := serviceConfigWith(t, "sh", tt.block)
			args := append([]string{"service", "install", "--dry-run", "--config", cfg}, tt.flags...)
			code, stdout, stderr := runCLI(args...)
			if code != 0 {
				t.Fatalf("= %d\n%s%s", code, stdout, stderr)
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("unit lacks %q:\n%s", want, stdout)
				}
			}
			for _, never := range tt.wantNever {
				if strings.Contains(stdout, never) {
					t.Errorf("unit has %q:\n%s", never, stdout)
				}
			}
		})
	}
}

func TestServiceInstallRejectsBadFlags(t *testing.T) {
	fakeHost(t, 1000)
	cfg := serviceConfig(t, "sh")
	tests := []struct {
		flags []string
		want  string
	}{
		{[]string{"--restart", "sometimes"}, `service.restart: "sometimes" must be always, on-failure or never`},
		{[]string{"--path", "bin:/usr/bin"}, `service.path: "bin" is not an absolute directory`},
		{[]string{"--run-as", "www"}, "service.user: only applies with system: true"},
		{[]string{"--system", "--path", "~/bin"}, `service.path: "~/bin": a system service has no ~`},
	}
	for _, tt := range tests {
		args := append([]string{"service", "install", "--dry-run", "--config", cfg}, tt.flags...)
		if code, _, stderr := runCLI(args...); code != 2 || !strings.Contains(stderr, tt.want) {
			t.Errorf("%q = %d, %q; want 2 and %q", tt.flags, code, stderr, tt.want)
		}
	}
}

func TestServiceInstallStartFromTheFile(t *testing.T) {
	fakeHost(t, 1000)
	const restart = "$ systemctl --user restart waker.service"
	noStart := serviceConfigWith(t, "sh", "service: {start: false}\n")
	tests := []struct {
		name      string
		cfg       string
		flags     []string
		wantStart bool
	}{
		{"default", serviceConfig(t, "sh"), nil, true},
		{"start: false", noStart, nil, false},
		{"--no-start=false over start: false", noStart, []string{"--no-start=false"}, true},
		{"--no-start", serviceConfig(t, "sh"), []string{"--no-start"}, false},
	}
	for _, tt := range tests {
		args := append([]string{"service", "install", "--dry-run", "--config", tt.cfg}, tt.flags...)
		code, stdout, stderr := runCLI(args...)
		if code != 0 {
			t.Fatalf("%s: = %d\n%s%s", tt.name, code, stdout, stderr)
		}
		if got := strings.Contains(stdout, restart); got != tt.wantStart {
			t.Errorf("%s: restarts = %v, want %v:\n%s", tt.name, got, tt.wantStart, stdout)
		}
		if !strings.Contains(stdout, "$ systemctl --user enable waker.service") {
			t.Errorf("%s: not enabled:\n%s", tt.name, stdout)
		}
	}
}

func TestServiceUninstallScopeFromTheFile(t *testing.T) {
	h := fakeHost(t, 1000)
	userUnit := filepath.Join(h.ConfigHome, "systemd", "user", "waker.service")
	sysUnit := filepath.Join(h.Root, "etc", "systemd", "system", "waker.service")
	sys := serviceConfigWith(t, "sh", "service: {system: true}\n")
	broken := filepath.Join(t.TempDir(), "waker.yaml")
	writeFile(t, broken, "apps: []\n")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"file system: true", []string{"--config", sys}, sysUnit},
		{"--system=false over the file", []string{"--config", sys, "--system=false"}, userUnit},
		{"no service block", []string{"--config", serviceConfig(t, "sh")}, userUnit},
		{"a broken config never blocks it", []string{"--config", broken}, userUnit},
		{"--system without a config", []string{"--config", filepath.Join(t.TempDir(), "none.yaml"), "--system"}, sysUnit},
	}
	for _, tt := range tests {
		args := append([]string{"service", "uninstall", "--dry-run"}, tt.args...)
		code, stdout, stderr := runCLI(args...)
		if code != 0 || !strings.Contains(stdout, "$ rm -rf "+tt.want+"\n") {
			t.Errorf("%s: = %d, %q%q; want the removal of %s", tt.name, code, stdout, stderr, tt.want)
		}
	}
}

func TestCheckWarnsAboutAStaleService(t *testing.T) {
	h := fakeHost(t, 1000)
	cfgPath := serviceConfigWith(t, "sh", "service: {restart: always}\n")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	const stale = "differs from the service waker.yaml describes"
	if code, stdout, _ := runCLI("check", "--config", cfgPath); code != 0 || strings.Contains(stdout, stale) {
		t.Fatalf("nothing installed = %d, %q", code, stdout)
	}

	// Install exactly what waker service install would write for the file.
	sp, err := serviceSpec(cfg.Service, cfgPath, h)
	if err != nil {
		t.Fatal(err)
	}
	p, err := service.PlanInstall(h, sp)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(nopRunner{}, io.Discard, false); err != nil {
		t.Fatal(err)
	}
	if code, stdout, _ := runCLI("check", "--config", cfgPath); code != 0 || strings.Contains(stdout, stale) {
		t.Fatalf("current unit = %d, %q", code, stdout)
	}

	// The file changes; the unit doesn't until the next install.
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfgPath, strings.Replace(string(b), "restart: always", "restart: never", 1))
	want := "warning: " + p.Unit + " " + stale + " (its service: block, or this waker version); apply it with: waker service install --config " + cfgPath + "\n"
	if code, stdout, _ := runCLI("check", "--config", cfgPath); code != 0 || !strings.Contains(stdout, want) {
		t.Fatalf("stale unit = %d, %q; want %q", code, stdout, want)
	}
}
