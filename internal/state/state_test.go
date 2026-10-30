package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
)

func TestOverridesRoundTripAtomically(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	in := map[string]Override{
		"shop": {Mode: config.ModeOff, Since: since},
		"blog": {Mode: config.ModeOnDemand, Since: since, Idle: config.Duration(10 * time.Second)},
	}
	if err := SaveOverrides(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadOverrides(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out["blog"].Idle.D() != 10*time.Second || out["shop"].Mode != config.ModeOff {
		t.Fatalf("round trip = %+v", out)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != ModesFile {
		t.Fatalf("temp files left behind: %v", entries)
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("state dir mode = %v, want 0700", st.Mode().Perm())
	}
}

func TestMissingFilesAreEmpty(t *testing.T) {
	o, err := LoadOverrides(t.TempDir())
	if err != nil || len(o) != 0 {
		t.Fatalf("= %v, %v", o, err)
	}
	p, err := LoadPeaks(t.TempDir())
	if err != nil || len(p) != 0 {
		t.Fatalf("= %v, %v", p, err)
	}
}

func TestCorruptFilesAreErrors(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{ModesFile, PeaksFile} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadOverrides(dir); err == nil {
		t.Fatal("corrupt modes.json accepted")
	}
	if _, err := LoadPeaks(dir); err == nil {
		t.Fatal("corrupt peaks.json accepted")
	}
}

func TestPeaksRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SavePeaks(dir, map[string]Peak{"shop": {MiB: 700}}); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPeaks(dir)
	if err != nil || p["shop"].MiB != 700 {
		t.Fatalf("= %v, %v", p, err)
	}
}

func TestOverrideProblem(t *testing.T) {
	tests := []struct {
		o    Override
		want string
	}{
		{Override{Mode: config.ModeOn}, ""},
		{Override{Mode: config.ModeOnDemand, Idle: config.Duration(time.Second)}, ""},
		{Override{Mode: "sometimes"}, "mode must be one of on, off, on-demand"},
		{Override{Mode: config.ModeOn, Idle: config.Duration(time.Second)}, "idle applies to on-demand only"},
		{Override{Mode: config.ModeOnDemand, Idle: -1}, "idle must be positive"},
	}
	for _, tt := range tests {
		if got := tt.o.Problem(); got != tt.want {
			t.Errorf("%+v.Problem() = %q, want %q", tt.o, got, tt.want)
		}
	}
}
