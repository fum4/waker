package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
)

func TestRenderStatus(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	since := now.Add(-3 * time.Minute)
	idleIn := config.Duration(9 * time.Second)
	s := app.Snapshot{
		Name: "shop", Listen: "127.0.0.1:8080", Upstream: "http://127.0.0.1:3000",
		Mode: config.ModeOnDemand, Idle: config.Duration(10 * time.Second),
		DeclaredMode: config.ModeOff, DeclaredIdle: config.Duration(30 * time.Minute),
		OverrideSince: &since, State: app.Up, Priority: 5, Heartbeat: true,
		LastActivity: now.Add(-time.Second), IdleIn: &idleIn,
		Evicted: &app.Eviction{By: "top", Priority: 10, At: now.Add(-2 * time.Hour)},
	}
	var buf bytes.Buffer
	renderStatus(&buf, s, now)
	for _, want := range []string{
		"shop — 127.0.0.1:8080 → http://127.0.0.1:3000\n",
		"  mode:      on-demand, idle 10s (override, set 3m ago; declared off)\n",
		"  state:     up\n",
		"  activity:  1s ago · idle stop in 9s\n",
		"  priority:  5 · last evicted by top (p10) 2h ago\n",
		"  heartbeat: injected into its pages\n",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in:\n%s", want, buf.String())
		}
	}
	s.Heartbeat, s.State, s.Error, s.OverrideSince = false, app.Failed, "start command exited: exit status 1", nil
	buf.Reset()
	renderStatus(&buf, s, now)
	for _, want := range []string{"state:     failed — start command exited", "heartbeat: off (heartbeat: false)", "mode:      on-demand, idle 10s\n"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in:\n%s", want, buf.String())
		}
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-time.Second: "0s", 89 * time.Second: "89s", 90 * time.Second: "1m",
		89 * time.Minute: "89m", 90 * time.Minute: "1h", 47 * time.Hour: "47h", 48 * time.Hour: "2d",
	} {
		if got := ago(d); got != want {
			t.Errorf("ago(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestStatusWithoutAWaker(t *testing.T) {
	dir := t.TempDir()
	cfg := dir + "/waker.yaml"
	writeFile(t, cfg, "control_socket: "+dir+"/none.sock\napps:\n  - {name: web, listen: '127.0.0.1:8080', upstream: 'http://127.0.0.1:3000', start: [./serve]}\n")
	code, _, stderr := runCLI("status", "--config", cfg)
	if code != 1 || !strings.Contains(stderr, "isn't running") {
		t.Fatalf("= %d %q", code, stderr)
	}
	if code, _, _ := runCLI("mode", "web", "--config", cfg); code != 2 {
		t.Fatalf("mode with one arg = %d", code)
	}
}
