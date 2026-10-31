package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
			heal, err := PlanHeal(h, sp)
			if err != nil {
				t.Fatal(err)
			}
			if heal.Reexec != tc.reexec {
				t.Errorf("reexec = %v, want %v", heal.Reexec, tc.reexec)
			}
			if heal.Old != renderUnit(old) {
				t.Errorf("old = %q", heal.Old)
			}
			p := heal.Plan
			if len(p.Files) != 1 || p.Files[0].Content != renderUnit(sp) {
				t.Errorf("files = %+v", p.Files)
			}
			// install's plan with NoStart: reload and enable, never a (re)start.
			var post []string
			for _, c := range p.Post {
				post = append(post, c.Argv[len(c.Argv)-2]+" "+c.Argv[len(c.Argv)-1])
			}
			if len(post) != 2 || !strings.HasSuffix(post[0], "daemon-reload") || post[1] != "enable "+UnitName {
				t.Errorf("post = %q, want daemon-reload, enable", post)
			}
		})
	}
}

func TestPlanHealLaunchdUnsupported(t *testing.T) {
	h := linuxHost(t)
	h.OS = "darwin"
	if _, err := PlanHeal(h, Spec{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
}
