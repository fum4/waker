package service

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fum4/waker/internal/config"
)

func TestCheckDrift(t *testing.T) {
	for _, h := range []Host{linuxHost(t), darwinHost(t)} {
		t.Run(h.OS, func(t *testing.T) {
			sp := spec()
			d, unit, err := CheckDrift(h, sp)
			if err != nil || d != DriftNotInstalled {
				t.Fatalf("nothing installed: %v, %v; want DriftNotInstalled", d, err)
			}
			p, err := PlanInstall(h, sp)
			if err != nil {
				t.Fatal(err)
			}
			if unit != p.Unit {
				t.Errorf("unit = %s, want %s", unit, p.Unit)
			}
			if err := p.Apply(&fakeRunner{}, &bytes.Buffer{}, false); err != nil {
				t.Fatal(err)
			}
			if d, _, err := CheckDrift(h, sp); err != nil || d != DriftNone {
				t.Fatalf("just installed: %v, %v; want DriftNone", d, err)
			}

			changed := sp
			changed.Restart = config.RestartNever
			if d, _, err := CheckDrift(h, changed); err != nil || d != DriftStale {
				t.Errorf("restart changed: %v, %v; want DriftStale", d, err)
			}
			changed = sp
			changed.Env = map[string]string{"A": "1"}
			if d, _, err := CheckDrift(h, changed); err != nil || d != DriftStale {
				t.Errorf("env added: %v, %v; want DriftStale", d, err)
			}

			if err := os.WriteFile(p.Unit, []byte("hand-written\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if d, _, err := CheckDrift(h, changed); err != nil || d != DriftNotOurs {
				t.Errorf("foreign unit: %v, %v; want DriftNotOurs", d, err)
			}
		})
	}
}

func TestCheckDriftLooksInTheSpecsScope(t *testing.T) {
	h := linuxHost(t)
	sp := spec()
	p, _ := PlanInstall(h, sp)
	if err := p.Apply(&fakeRunner{}, &bytes.Buffer{}, false); err != nil {
		t.Fatal(err)
	}
	sys := sp
	sys.System, sys.RunAs = true, "ana"
	d, unit, err := CheckDrift(h, sys)
	if err != nil || d != DriftNotInstalled || unit != filepath.Join(h.Root, "etc", "systemd", "system", UnitName) {
		t.Errorf("system scope with only a user unit = %v, %s, %v", d, unit, err)
	}
}

func TestCheckDriftUnsupported(t *testing.T) {
	h := linuxHost(t)
	h.OS = "freebsd"
	if _, _, err := CheckDrift(h, spec()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}
