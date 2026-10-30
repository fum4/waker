package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func darwinHost(t *testing.T) Host {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "Users", "ana")
	return Host{OS: "darwin", Home: home, User: "ana", UID: 501, EUID: 501, ConfigHome: filepath.Join(home, ".config"), Root: root}
}

func TestLaunchAgentInstall(t *testing.T) {
	h := darwinHost(t)
	sp := spec()
	sp.PATH = "/opt/homebrew/bin:/usr/bin&more"
	p, err := PlanInstall(h, sp)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(h.Home, "Library", "LaunchAgents", Label+".plist"); p.Unit != want {
		t.Fatalf("plist = %s, want %s", p.Unit, want)
	}
	plist := p.Files[0].Content
	assertWellFormed(t, plist)
	for _, want := range []string{
		Marker,
		"<string>/srv/my apps/waker.yaml</string>",
		"<key>WorkingDirectory</key>\n  <string>/srv/my apps</string>",
		"<string>/opt/homebrew/bin:/usr/bin&amp;more</string>",
		"<key>SuccessfulExit</key>\n    <false/>",
		filepath.Join(h.Home, "Library", "Logs", "waker", "waker.log"),
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
	if strings.Contains(plist, "UserName") {
		t.Error("an agent must not set UserName")
	}
	if p.Pre[0].String() != "launchctl bootout gui/501/"+Label || !p.Pre[0].Ignore {
		t.Errorf("pre = %+v", p.Pre)
	}
	if p.Post[0].String() != "launchctl bootstrap gui/501 "+shellQuote(p.Unit) {
		t.Errorf("post = %q", p.Post[0])
	}

	r := &fakeRunner{fail: map[string]bool{"launchctl bootout gui/501/" + Label: true}}
	if err := p.Apply(r, &bytes.Buffer{}, false); err != nil {
		t.Fatalf("install (bootout of an unloaded job is ignored): %v", err)
	}
	if st, err := os.Stat(filepath.Join(h.Home, "Library", "Logs", "waker")); err != nil || !st.IsDir() {
		t.Fatalf("log dir: %v", err)
	}
	u, _ := PlanUninstall(h, false)
	if err := u.Apply(&fakeRunner{}, &bytes.Buffer{}, false); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{p.Unit, filepath.Join(h.Home, "Library", "Logs", "waker")} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived uninstall", gone)
		}
	}
}

func TestLaunchDaemonInstall(t *testing.T) {
	h := darwinHost(t)
	sp := spec()
	sp.System, sp.RunAs = true, "ana"
	p, _ := PlanInstall(h, sp)
	if want := filepath.Join(h.Root, "Library", "LaunchDaemons", Label+".plist"); p.Unit != want {
		t.Fatalf("plist = %s", p.Unit)
	}
	if !strings.Contains(p.Files[0].Content, "<key>UserName</key>\n  <string>ana</string>") {
		t.Error("a daemon runs as RunAs")
	}
	if !slices.Contains(p.Post[0].Argv, "system") {
		t.Errorf("post = %q", p.Post[0])
	}
}

func TestLaunchdStatus(t *testing.T) {
	h := darwinHost(t)
	p, _ := PlanInstall(h, spec())
	if err := p.Apply(&fakeRunner{}, &bytes.Buffer{}, false); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{out: map[string]string{
		"launchctl print gui/501/" + Label: "gui/501/io.nomadware.waker = {\n\tactive count = 1\n\tstate = running\n\tpid = 777\n}\n",
	}}
	sts, _ := Statuses(h, r)
	st := sts[0]
	if !st.Installed || !st.Running || st.PID != 777 || !strings.HasSuffix(st.Logs, "waker.log") {
		t.Fatalf("status = %+v", st)
	}
	r = &fakeRunner{fail: map[string]bool{"launchctl print gui/501/" + Label: true}}
	if sts, _ := Statuses(h, r); sts[0].Running || sts[0].State != "not loaded" {
		t.Fatalf("unloaded status = %+v", sts[0])
	}
}

func assertWellFormed(t *testing.T, doc string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(doc))
	for {
		if _, err := d.Token(); err == io.EOF {
			return
		} else if err != nil {
			t.Fatalf("plist is not well-formed XML: %v\n%s", err, doc)
		}
	}
}
