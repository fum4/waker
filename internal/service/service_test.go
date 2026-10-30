package service

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeRunner records every command and answers from out; argv joined by
// spaces listed in fail return an error.
type fakeRunner struct {
	calls []string
	out   map[string]string
	fail  map[string]bool
}

func (f *fakeRunner) Run(argv ...string) (string, error) {
	k := strings.Join(argv, " ")
	f.calls = append(f.calls, k)
	if f.fail[k] {
		return "boom", errors.New("exit status 1")
	}
	return f.out[k], nil
}

func linuxHost(t *testing.T) Host {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home", "ana")
	mkdir(t, filepath.Join(root, "run", "systemd", "system"))
	return Host{OS: "linux", Home: home, User: "ana", UID: 1000, EUID: 1000, ConfigHome: filepath.Join(home, ".config"), Root: root}
}

func spec() Spec {
	return Spec{Binary: "/opt/waker/bin/waker", Config: "/srv/my apps/waker.yaml", WorkDir: "/srv/my apps", PATH: "/home/ana/.local/bin:/usr/bin"}
}

func mkdir(t *testing.T, d string) {
	t.Helper()
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestSystemdUserInstall(t *testing.T) {
	h := linuxHost(t)
	p, err := PlanInstall(h, spec())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(h.ConfigHome, "systemd", "user", "waker.service"); p.Unit != want || p.Files[0].Path != want {
		t.Fatalf("unit = %s, want %s", p.Unit, want)
	}
	unit := p.Files[0].Content
	for _, want := range []string{
		"# " + Marker,
		`ExecStart="/opt/waker/bin/waker" "serve" "--config" "/srv/my apps/waker.yaml"`,
		`WorkingDirectory="/srv/my apps"`,
		`Environment="PATH=/home/ana/.local/bin:/usr/bin"`,
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "User=") {
		t.Error("a user unit must not set User=")
	}
	var cmds []string
	for _, c := range p.Post {
		cmds = append(cmds, c.String())
	}
	want := []string{"systemctl --user daemon-reload", "systemctl --user enable waker.service", "systemctl --user restart waker.service"}
	if !slices.Equal(cmds, want) {
		t.Errorf("post = %q, want %q", cmds, want)
	}
	if len(p.Notes) != 1 || !strings.Contains(p.Notes[0], "sudo loginctl enable-linger ana") {
		t.Errorf("notes = %q, want the linger line", p.Notes)
	}

	mkdir(t, filepath.Join(h.Root, "var", "lib", "systemd", "linger"))
	if err := os.WriteFile(filepath.Join(h.Root, "var", "lib", "systemd", "linger", "ana"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if p, _ := PlanInstall(h, spec()); len(p.Notes) != 0 {
		t.Errorf("lingering: notes = %q, want none", p.Notes)
	}
}

func TestSystemdSystemInstall(t *testing.T) {
	h := linuxHost(t)
	sp := spec()
	sp.System, sp.RunAs = true, "ana"
	p, err := PlanInstall(h, sp)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(h.Root, "etc", "systemd", "system", "waker.service"); p.Unit != want {
		t.Fatalf("unit = %s, want %s", p.Unit, want)
	}
	for _, want := range []string{"User=ana", "WantedBy=multi-user.target"} {
		if !strings.Contains(p.Files[0].Content, want) {
			t.Errorf("unit lacks %q", want)
		}
	}
	if p.Post[0].String() != "systemctl daemon-reload" || len(p.Notes) != 0 {
		t.Errorf("post[0] = %q, notes = %q", p.Post[0], p.Notes)
	}
}

func TestUnitQuoting(t *testing.T) {
	got := execLine(`/a b/w"k`, `50%`, `$HOME`, `c:\d`)
	want := `"/a b/w\"k" "50%%" "$$HOME" "c:\\d"`
	if got != want {
		t.Errorf("execLine = %s, want %s", got, want)
	}
	if got := unitQuote("PATH=$x:%h", false); got != `"PATH=$x:%%h"` {
		t.Errorf("env quote = %s", got)
	}
}

func TestUnsupported(t *testing.T) {
	h := linuxHost(t)
	h.OS = "freebsd"
	if _, err := PlanInstall(h, spec()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("freebsd: err = %v, want ErrUnsupported", err)
	}
	h = linuxHost(t)
	if err := os.RemoveAll(filepath.Join(h.Root, "run", "systemd")); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanInstall(h, spec()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("no systemd: err = %v, want ErrUnsupported", err)
	}
	if s := ManualSteps(spec()); !strings.Contains(s, "'/srv/my apps/waker.yaml'") || !strings.Contains(s, "PATH=/home/ana") {
		t.Errorf("manual steps:\n%s", s)
	}
}

func TestApplyInstallThenUninstall(t *testing.T) {
	h := linuxHost(t)
	p, _ := PlanInstall(h, spec())
	r := &fakeRunner{}
	var out bytes.Buffer
	if err := p.Apply(r, &out, false); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(p.Unit); err != nil || string(b) != p.Files[0].Content {
		t.Fatalf("unit on disk = %q, %v", b, err)
	}
	if len(r.calls) != 3 || !strings.Contains(out.String(), "$ systemctl --user restart waker.service") {
		t.Errorf("calls = %q\n%s", r.calls, out.String())
	}
	// A reinstall overwrites its own unit.
	if err := p.Apply(&fakeRunner{}, &out, false); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	u, _ := PlanUninstall(h, false)
	r = &fakeRunner{fail: map[string]bool{"systemctl --user disable --now waker.service": true}}
	if err := u.Apply(r, &out, false); err != nil {
		t.Fatalf("uninstall (a failing stop is ignored): %v", err)
	}
	if _, err := os.Stat(p.Unit); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unit still there: %v", err)
	}
	if !slices.Equal(r.calls, []string{"systemctl --user disable --now waker.service", "systemctl --user daemon-reload"}) {
		t.Errorf("calls = %q", r.calls)
	}
}

func TestApplyNeverClobbersAForeignUnit(t *testing.T) {
	h := linuxHost(t)
	p, _ := PlanInstall(h, spec())
	mkdir(t, filepath.Dir(p.Unit))
	const mine = "[Service]\nExecStart=/usr/bin/waker serve\n"
	if err := os.WriteFile(p.Unit, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	if err := p.Apply(r, &bytes.Buffer{}, false); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("install over a foreign unit: err = %v, want ErrNotOurs", err)
	}
	u, _ := PlanUninstall(h, false)
	if err := u.Apply(r, &bytes.Buffer{}, false); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("uninstall of a foreign unit: err = %v, want ErrNotOurs", err)
	}
	if b, _ := os.ReadFile(p.Unit); string(b) != mine || len(r.calls) != 0 {
		t.Fatalf("foreign unit touched: %q, calls %q", b, r.calls)
	}
	if err := p.Apply(r, &bytes.Buffer{}, true); err != nil {
		t.Fatalf("--force: %v", err)
	}
}

func TestApplyStopsOnAFailingCommand(t *testing.T) {
	h := linuxHost(t)
	p, _ := PlanInstall(h, spec())
	r := &fakeRunner{fail: map[string]bool{"systemctl --user daemon-reload": true}}
	err := p.Apply(r, &bytes.Buffer{}, false)
	if err == nil || !strings.Contains(err.Error(), "daemon-reload") || len(r.calls) != 1 {
		t.Fatalf("err = %v, calls = %q", err, r.calls)
	}
}

func TestSystemdStatus(t *testing.T) {
	h := linuxHost(t)
	r := &fakeRunner{out: map[string]string{
		"systemctl --user show waker.service --property=ActiveState,SubState,MainPID,UnitFileState": "ActiveState=active\nSubState=running\nMainPID=4242\nUnitFileState=enabled\n",
	}}
	sts, err := Statuses(h, r)
	if err != nil {
		t.Fatal(err)
	}
	if sts[0].Installed || sts[1].Installed {
		t.Fatalf("nothing installed yet: %+v", sts)
	}
	p, _ := PlanInstall(h, spec())
	if err := p.Apply(&fakeRunner{}, &bytes.Buffer{}, false); err != nil {
		t.Fatal(err)
	}
	sts, _ = Statuses(h, r)
	st := sts[0]
	if !st.Installed || !st.Running || !st.Enabled || st.PID != 4242 || st.State != "active (running)" {
		t.Fatalf("status = %+v", st)
	}
	var out bytes.Buffer
	st.Print(&out)
	for _, want := range []string{"user service (systemd)", "active (running), pid 4242", "journalctl --user -u waker.service -f", "enable-linger ana"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestServicePATHRoundTrips(t *testing.T) {
	const tricky = `/opt/a b/bin:/x%y/bin:/q"z/bin:/usr/bin`
	for _, os := range []string{"linux", "darwin"} {
		h := linuxHost(t)
		h.OS = os
		if _, _, ok := ServicePATH(h); ok {
			t.Fatalf("%s: a PATH with nothing installed", os)
		}
		sp := spec()
		sp.PATH = tricky
		p, _ := PlanInstall(h, sp)
		if err := p.Apply(&fakeRunner{}, &bytes.Buffer{}, false); err != nil {
			t.Fatal(err)
		}
		got, unit, ok := ServicePATH(h)
		if !ok || got != tricky || unit != p.Unit {
			t.Errorf("%s: ServicePATH = %q, %s, %v; want %q", os, got, unit, ok, tricky)
		}
	}
}

func TestLookPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	mkdir(t, bin)
	for _, f := range []string{"tool", "script"} {
		if err := os.WriteFile(filepath.Join(bin, f), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "data"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		file, path string
		want       bool
	}{
		{"tool", bin, true},
		{"tool", "/nonexistent", false},
		{"data", bin, false}, // not executable
		{"./bin/script", "", true},
		{"bin/script", "", true},
		{"./bin/missing", bin, false},
		{filepath.Join(bin, "tool"), "", true},
		{"tool", "bin", true}, // a relative PATH entry resolves against dir
	}
	for _, tt := range tests {
		if _, ok := LookPath(tt.file, tt.path, dir); ok != tt.want {
			t.Errorf("LookPath(%q, %q) = %v, want %v", tt.file, tt.path, ok, tt.want)
		}
	}
}

func TestShellJoin(t *testing.T) {
	got := ShellJoin([]string{"sudo", "loginctl", "enable-linger", "ana", "it's", "a b", ""})
	if want := `sudo loginctl enable-linger ana 'it'\''s' 'a b' ''`; got != want {
		t.Errorf("ShellJoin = %s, want %s", got, want)
	}
}
