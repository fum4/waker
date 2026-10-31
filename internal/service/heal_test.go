package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fum4/waker/internal/config"
)

func TestPlanHealReexec(t *testing.T) {
	base := Spec{Binary: "/bin/waker", Config: "/c/waker.yaml", WorkDir: "/c", PATH: "/usr/bin", Restart: config.RestartAlways, Env: map[string]string{"A": "1"}}
	tests := []struct {
		name   string
		change func(*Spec)
		reexec bool
	}{
		{"restart policy only", func(s *Spec) { s.Restart = config.RestartNever }, false},
		{"path", func(s *Spec) { s.PATH = "/opt/bin:/usr/bin" }, true},
		{"env value", func(s *Spec) { s.Env = map[string]string{"A": "2"} }, true},
		{"env removed", func(s *Spec) { s.Env = nil }, true},
		{"system user", func(s *Spec) { s.System, s.RunAs = true, "bob" }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := linuxHost(t)
			old := base
			old.System = tc.name == "system user"
			old.RunAs = "ana"
			unit := systemd{h}.unitPath(old.System)
			if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(unit, []byte(renderUnit(old)), 0o644); err != nil {
				t.Fatal(err)
			}
			sp := old
			tc.change(&sp)
			p, reexec, err := PlanHeal(h, sp)
			if err != nil {
				t.Fatal(err)
			}
			if reexec != tc.reexec {
				t.Errorf("reexec = %v, want %v", reexec, tc.reexec)
			}
			if len(p.Files) != 1 || p.Files[0].Content != renderUnit(sp) {
				t.Errorf("files = %+v", p.Files)
			}
			if len(p.Post) != 1 || p.Post[0].Argv[len(p.Post[0].Argv)-1] != "daemon-reload" {
				t.Errorf("post = %v, want only daemon-reload", p.Post)
			}
		})
	}
}

func TestPlanHealLaunchdUnsupported(t *testing.T) {
	h := linuxHost(t)
	h.OS = "darwin"
	if _, _, err := PlanHeal(h, Spec{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestInUnit(t *testing.T) {
	tests := []struct {
		cgroup       string
		user, system bool
	}{
		{"0::/user.slice/user-1000.slice/user@1000.service/app.slice/waker.service\n", true, false},
		{"0::/system.slice/waker.service\n", false, true},
		{"0::/user.slice/user-1000.slice/user@1000.service/agents.slice/claude@x.service\n", false, false},
		{"0::/user.slice/user-1000.slice/session-3.scope\n", false, false},
		{"12:pids:/system.slice/waker.service\n0::/system.slice/waker.service\n", false, true},
		{"", false, false},
	}
	for _, tc := range tests {
		if got := InUnit(tc.cgroup, false); got != tc.user {
			t.Errorf("InUnit(%q, user) = %v", tc.cgroup, got)
		}
		if got := InUnit(tc.cgroup, true); got != tc.system {
			t.Errorf("InUnit(%q, system) = %v", tc.cgroup, got)
		}
	}
}
