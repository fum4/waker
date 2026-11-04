package daemon

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/control"
	"github.com/fum4/waker/internal/state"
)

func workerConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	cfg, dir := newConfig(t, "")
	ac := &cfg.Apps[0]
	ac.Name, ac.Listen, ac.Upstream, ac.Mode = "jobs", "", "", config.ModeOn
	ac.Pages = config.Pages{Files: map[string]string{"offline": "/missing-worker-page.html"}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg, dir
}

func TestWorkerOnlyDaemonLifecycleAndMemory(t *testing.T) {
	cfg, dir := workerConfig(t)
	d, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	d.OnReady(func() { close(ready) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "up")); !os.IsNotExist(err) {
			t.Errorf("worker outlived daemon: %v", err)
		}
	})
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("worker-only daemon never became ready")
	}
	worker := d.all[0]
	worker.Wait()
	client := control.NewClient(cfg.ControlSocket)
	health, err := client.Health(ctx)
	if err != nil || len(health.Listeners) != 0 || len(health.Skipped) != 0 {
		t.Fatalf("health = %+v, %v", health, err)
	}
	if len(d.pages) != 0 || len(d.listening) != 0 || len(d.managed) != 1 || !slices.Contains(d.ticked(), worker) {
		t.Fatal("worker missing from managed loop or has HTTP resources")
	}
	report, err := client.Status(ctx)
	if err != nil || len(report.Apps) != 1 {
		t.Fatalf("status = %+v, %v", report, err)
	}
	status := report.Apps[0]
	if status.Name != "jobs" || status.State != app.Up || status.Listen != "" || status.Upstream != "" || status.Memory == nil || status.Heartbeat {
		t.Fatalf("worker status = %+v", status)
	}
	if _, err := client.SetMode(ctx, control.ModeRequest{App: "jobs", Mode: config.ModeOnDemand}); err == nil || !strings.Contains(err.Error(), "workers support only on or off") {
		t.Fatalf("on-demand = %v", err)
	}
	if _, err := client.SetMode(ctx, control.ModeRequest{App: "jobs", Mode: config.ModeOff}); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	if worker.Started() || worker.State(ctx) != app.Offline {
		t.Fatal("off did not stop the worker")
	}
	if cfg.Apps[0].MemorySource == config.MemoryProcTree {
		peaks, err := state.LoadPeaks(cfg.StateDir)
		if err != nil || peaks["jobs"].MiB <= 0 {
			t.Fatalf("worker peak not learned: %+v, %v", peaks, err)
		}
		report := d.Status(ctx)
		learned := report.Apps[0].Memory.LearnedMiB
		if learned == nil || *learned != peaks["jobs"].MiB {
			t.Fatalf("worker memory status lost learned peak: %+v", report.Apps[0].Memory)
		}
		restarted, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil || restarted.budget.Learned()["jobs"] != peaks["jobs"].MiB {
			t.Fatalf("worker peak did not survive restart: %v", err)
		}
	}
	if _, err := client.SetMode(ctx, control.ModeRequest{App: "jobs", Reset: true}); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	if worker.State(ctx) != app.Up || worker.Override() != nil {
		t.Fatal("reset did not start the worker")
	}
}

func TestWorkerAlongsideHTTPAndSkippedListener(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	cfg, _ := newConfig(t, "  - {name: held, listen: '"+held.Addr().String()+"', upstream: 'http://127.0.0.1:1', start: [x]}\n")
	worker := cfg.Apps[0]
	worker.Name, worker.Listen, worker.Upstream, worker.Mode = "jobs", "", "", config.ModeOn
	cfg.Apps = append(cfg.Apps, worker)
	d, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	servers := d.listen()
	defer func() {
		for _, server := range servers {
			server.Close()
		}
	}()
	if !slices.Equal(names(d.managed), []string{"shop", "jobs"}) || !slices.Equal(names(d.ticked()), []string{"shop", "jobs"}) {
		t.Fatalf("managed = %v, ticked = %v", names(d.managed), names(d.ticked()))
	}
	health := d.Health()
	if !slices.Equal(health.Listeners, []string{"shop"}) || health.Skipped["held"] == "" {
		t.Fatalf("health = %+v", health)
	}
	if !slices.Equal(names(d.newGate().Apps()), []string{"shop", "jobs"}) {
		t.Fatal("memory gate lost managed workers")
	}
}

func TestWorkerStoredOverrides(t *testing.T) {
	for _, mode := range []config.Mode{config.ModeOnDemand, config.ModeOff, config.ModeOn} {
		t.Run(string(mode), func(t *testing.T) {
			cfg, _ := workerConfig(t)
			if err := state.SaveOverrides(cfg.StateDir, map[string]state.Override{"jobs": {Mode: mode}}); err != nil {
				t.Fatal(err)
			}
			d, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			if mode == config.ModeOnDemand {
				if len(d.overrides) != 0 || d.all[0].Override() != nil || d.all[0].Mode() != config.ModeOn {
					t.Fatal("stored on-demand override applied to worker")
				}
			} else if d.all[0].Mode() != mode || d.all[0].Override() == nil {
				t.Fatalf("stored %s override lost", mode)
			}
		})
	}
}
