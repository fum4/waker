package service

import (
	"strings"
	"testing"

	"github.com/fum4/waker/internal/config"
)

func TestUnitRestart(t *testing.T) {
	tests := []struct {
		restart config.Restart
		want    string
	}{
		{"", "Restart=always\n"}, // a Spec built without a config
		{config.RestartAlways, "Restart=always\n"},
		{config.RestartOnFailure, "Restart=on-failure\n"},
		{config.RestartNever, "Restart=no\n"},
	}
	for _, tt := range tests {
		t.Run(string(tt.restart), func(t *testing.T) {
			sp := spec()
			sp.Restart = tt.restart
			p, err := PlanInstall(linuxHost(t), sp)
			if err != nil {
				t.Fatal(err)
			}
			unit := p.Files[0].Content
			if !strings.Contains(unit, tt.want+"RestartSec=2\n") || strings.Count(unit, "Restart=") != 1 {
				t.Errorf("unit lacks %q (once):\n%s", tt.want, unit)
			}
		})
	}
}

func TestUnitEnvSortedAndQuoted(t *testing.T) {
	sp := spec()
	sp.Env = map[string]string{"ZED": "last", "A_B": `two words "quoted" 50%`, "MID": "x"}
	p, err := PlanInstall(linuxHost(t), sp)
	if err != nil {
		t.Fatal(err)
	}
	want := `Environment="PATH=/home/ana/.local/bin:/usr/bin"` + "\n" +
		`Environment="A_B=two words \"quoted\" 50%%"` + "\n" +
		`Environment="MID=x"` + "\n" +
		`Environment="ZED=last"` + "\n"
	if unit := p.Files[0].Content; !strings.Contains(unit, want) {
		t.Errorf("unit lacks the sorted env lines\n%s\nin:\n%s", want, unit)
	}
}

func TestUnitEnvBeforeTheSystemSocket(t *testing.T) {
	sp := spec()
	sp.System, sp.RunAs = true, "ana"
	sp.Env = map[string]string{"A": "1"}
	p, err := PlanInstall(linuxHost(t), sp)
	if err != nil {
		t.Fatal(err)
	}
	unit := p.Files[0].Content
	a, sock := strings.Index(unit, `Environment="A=1"`), strings.Index(unit, config.EnvControlSocket+"=")
	if a < 0 || sock < 0 || a > sock {
		t.Errorf("env A at %d, control socket at %d:\n%s", a, sock, unit)
	}
}

func TestPlistRestart(t *testing.T) {
	tests := []struct {
		restart config.Restart
		want    string // "" = no KeepAlive key at all
	}{
		{"", "<key>KeepAlive</key>\n  <true/>\n"},
		{config.RestartAlways, "<key>KeepAlive</key>\n  <true/>\n"},
		{config.RestartOnFailure, "<key>KeepAlive</key>\n  <dict>\n    <key>SuccessfulExit</key>\n    <false/>\n  </dict>\n"},
		{config.RestartNever, ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.restart), func(t *testing.T) {
			sp := spec()
			sp.Restart = tt.restart
			p, err := PlanInstall(darwinHost(t), sp)
			if err != nil {
				t.Fatal(err)
			}
			plist := p.Files[0].Content
			assertWellFormed(t, plist)
			if tt.want == "" {
				if strings.Contains(plist, "KeepAlive") {
					t.Errorf("restart never keeps KeepAlive:\n%s", plist)
				}
				return
			}
			if !strings.Contains(plist, tt.want) || strings.Count(plist, "KeepAlive") != 1 {
				t.Errorf("plist lacks %q (once):\n%s", tt.want, plist)
			}
		})
	}
}

func TestPlistEnv(t *testing.T) {
	sp := spec()
	sp.Env = map[string]string{"ZED": "z", "A": "x<y&z"}
	p, err := PlanInstall(darwinHost(t), sp)
	if err != nil {
		t.Fatal(err)
	}
	plist := p.Files[0].Content
	assertWellFormed(t, plist)
	want := "  <key>EnvironmentVariables</key>\n  <dict>\n" +
		"    <key>PATH</key>\n    <string>/home/ana/.local/bin:/usr/bin</string>\n" +
		"    <key>A</key>\n    <string>x&lt;y&amp;z</string>\n" +
		"    <key>ZED</key>\n    <string>z</string>\n" +
		"  </dict>\n"
	if !strings.Contains(plist, want) {
		t.Errorf("plist lacks the env dict\n%s\nin:\n%s", want, plist)
	}
}

func TestManualStepsCarryRestartAndEnv(t *testing.T) {
	sp := spec()
	sp.Restart = config.RestartOnFailure
	sp.Env = map[string]string{"B": "2", "A": "1"}
	s := ManualSteps(sp)
	for _, want := range []string{"env:      PATH=/home/ana/.local/bin:/usr/bin A=1 B=2", "restart:  on-failure,"} {
		if !strings.Contains(s, want) {
			t.Errorf("manual steps lack %q:\n%s", want, s)
		}
	}
}
