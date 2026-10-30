package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/state"
)

func TestAStopLearnsTheProcessTreePeakAndPersistsIt(t *testing.T) {
	cfg, _ := newConfig(t, "")
	d, c := start(t, cfg)
	ctx := context.Background()
	a := d.all[0]
	a.Wake(ctx)
	waitFor(t, 5*time.Second, func() bool { return a.State(ctx) == app.Up })
	a.Stop("test")

	peaks, err := state.LoadPeaks(cfg.StateDir)
	if err != nil || peaks["shop"].MiB <= 0 {
		t.Fatalf("peaks.json = %+v, %v", peaks, err)
	}
	report, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := report.Apps[0].Memory
	if m == nil || m.LearnedMiB == nil || *m.LearnedMiB != peaks["shop"].MiB || m.HeadroomMiB != 2844 || m.RequiredMiB != m.NeedMiB+2844 {
		t.Fatalf("memory = %+v", m)
	}
	if p := report.Policy; p.Eviction != "lower-priority, priority-then-idle" || p.AvailableMiB == nil || p.Headroom != "linear max 5GiB, min 512MiB" {
		t.Fatalf("policy = %+v", p)
	}

	d2, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || d2.budget.Learned()["shop"] != peaks["shop"].MiB {
		t.Fatalf("a restarted waker forgot the peak: %v", err)
	}
}

func TestCorruptPeaksStartEmpty(t *testing.T) {
	cfg, _ := newConfig(t, "")
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.StateDir, state.PeaksFile), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || len(d.budget.Learned()) != 0 {
		t.Fatalf("= %v, %v", d.budget.Learned(), err)
	}
}
