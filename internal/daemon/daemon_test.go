package daemon

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/control"
	"github.com/fum4/waker/internal/state"
)

// markerApp is up while ./up exists; SIGTERM removes it.
const markerApp = `start: [sh, -c, "touch up; trap 'rm -f up; exit 0' TERM; while :; do sleep 0.05; done"]
    health: {command: [test, -f, up], interval: 50ms}`

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func newConfig(t *testing.T, extra string) (*config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Parse([]byte("apps:\n  - name: shop\n    listen: '" + freePort(t) + "'\n    upstream: 'http://127.0.0.1:1'\n    cwd: " + dir + "\n    " + markerApp + "\n" + extra))
	if err != nil {
		t.Fatal(err)
	}
	cfg.StateDir = filepath.Join(dir, "state")
	cfg.ControlSocket = filepath.Join(dir, "run", "control.sock")
	return cfg, dir
}

func start(t *testing.T, cfg *config.Config) (*Daemon, *control.Client) {
	t.Helper()
	d, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	c := control.NewClient(cfg.ControlSocket)
	waitFor(t, 5*time.Second, func() bool { _, err := c.Health(ctx); return err == nil })
	return d, c
}

func waitFor(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met in %s", limit)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAHeldPortIsSkippedNotFatal(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	cfg, _ := newConfig(t, "  - {name: held, listen: '"+held.Addr().String()+"', upstream: 'http://127.0.0.1:1', start: [x]}\n")
	_, c := start(t, cfg)
	h, err := c.Health(context.Background())
	if err != nil || len(h.Listeners) != 1 || h.Listeners[0] != "shop" || !strings.Contains(h.Skipped["held"], "address already in use") {
		t.Fatalf("health = %+v, %v", h, err)
	}
	_, err = c.SetMode(context.Background(), control.ModeRequest{App: "held", Mode: config.ModeOn})
	if err == nil || !strings.Contains(err.Error(), "no listening app 'held' (") || !strings.Contains(err.Error(), "apps: shop") {
		t.Fatalf("mode held = %v", err)
	}
}

func TestOverridesPersistAcrossRestartsAndReset(t *testing.T) {
	cfg, _ := newConfig(t, "")
	_, c := start(t, cfg)
	ctx := context.Background()
	snap, err := c.SetMode(ctx, control.ModeRequest{App: "shop", Mode: config.ModeOnDemand, Idle: config.Duration(10 * time.Second)})
	if err != nil || snap.Idle.D() != 10*time.Second || snap.OverrideSince == nil {
		t.Fatalf("set = %+v, %v", snap, err)
	}
	saved, _ := state.LoadOverrides(cfg.StateDir)
	if saved["shop"].Idle.D() != 10*time.Second {
		t.Fatalf("modes.json = %+v", saved)
	}
	d2, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || d2.all[0].Idle() != 10*time.Second {
		t.Fatalf("a restarted waker lost the override: %v", err)
	}
	if _, err := c.SetMode(ctx, control.ModeRequest{App: "shop", Reset: true}); err != nil {
		t.Fatal(err)
	}
	if saved, _ = state.LoadOverrides(cfg.StateDir); len(saved) != 0 {
		t.Fatalf("after reset modes.json = %+v", saved)
	}
}

func TestCorruptOrStaleOverridesStartEmpty(t *testing.T) {
	cfg, _ := newConfig(t, "")
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	modes := filepath.Join(cfg.StateDir, state.ModesFile)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, body := range []string{"{not json", `{"ghost": {"mode": "on"}, "shop": {"mode": "sometimes"}}`} {
		if err := os.WriteFile(modes, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		d, err := New(cfg, log)
		if err != nil || len(d.overrides) != 0 || d.all[0].Override() != nil {
			t.Fatalf("%s: overrides = %+v, %v", body, d.overrides, err)
		}
	}
}

func TestIdleOverrideStopsOnTimeNotMidRequest(t *testing.T) {
	cfg, dir := newConfig(t, "")
	d, c := start(t, cfg)
	ctx := context.Background()
	if _, err := c.SetMode(ctx, control.ModeRequest{App: "shop", Mode: config.ModeOnDemand, Idle: config.Duration(time.Second)}); err != nil {
		t.Fatal(err)
	}
	a := d.all[0]
	if st := a.Wake(ctx); st != app.Starting {
		t.Fatalf("wake = %s", st)
	}
	waitFor(t, 5*time.Second, func() bool { return a.State(ctx) == app.Up })

	a.Activity().Begin() // a request in flight for 2s: no stop meanwhile
	time.Sleep(2 * time.Second)
	if _, err := os.Stat(filepath.Join(dir, "up")); err != nil {
		t.Fatal("stopped mid-request")
	}
	a.Activity().End()
	ended := time.Now()
	waitFor(t, 3*time.Second, func() bool { _, err := os.Stat(filepath.Join(dir, "up")); return err != nil })
	if took := time.Since(ended); took < time.Second || took > 1500*time.Millisecond {
		t.Fatalf("idle stop %s after the request ended, want 1s–1.5s", took)
	}
}

func TestShutdownStopsWhatItStarted(t *testing.T) {
	cfg, dir := newConfig(t, "")
	cfg.Apps[0].Mode = config.ModeOn
	d, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return d.all[0].State(ctx) == app.Up })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "up")); err == nil {
		t.Fatal("the app outlived the waker")
	}
}
